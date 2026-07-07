// SPDX-License-Identifier: GPL-2.0
/*
 * bpfman_e2e_targets: dedicated kmod-backed attach targets for the
 * bpfman e2e test suite. Exports a fixed pool of noinline functions that
 * tests attach BPF programs to, plus per-slot debugfs trigger
 * files that invoke the corresponding function on write(2).
 *
 * The pool gives each fentry/fexit and kprobe/kretprobe test a
 * leased kernel-function slot with its own BPF trampoline, eliminating
 * the rebuild contention that sharing a common function (e.g. do_unlinkat)
 * introduces when several parallel tests attach and detach concurrently.
 * The lease is a test-harness convention, not kernel access control.
 */

#include <linux/atomic.h>
#include <linux/debugfs.h>
#include <linux/fs.h>
#include <linux/module.h>
#include <linux/uaccess.h>

#define CREATE_TRACE_POINTS
#include "bpfman_e2e_trace.h"

#define BPFMAN_E2E_NUM_SLOTS 128

// clang-format off

/*
 * X-macro that expands to a list of slot indices. Used to keep the
 * function definitions, the function-pointer table, and any future
 * per-slot state in sync without hand-maintaining duplicated blocks.
 */
#define BPFMAN_E2E_FOR_EACH_SLOT(X)                                  \
	X(0)  X(1)  X(2)  X(3)  X(4)  X(5)  X(6)  X(7)               \
	X(8)  X(9)  X(10) X(11) X(12) X(13) X(14) X(15)              \
	X(16) X(17) X(18) X(19) X(20) X(21) X(22) X(23)              \
	X(24) X(25) X(26) X(27) X(28) X(29) X(30) X(31)              \
	X(32) X(33) X(34) X(35) X(36) X(37) X(38) X(39)              \
	X(40) X(41) X(42) X(43) X(44) X(45) X(46) X(47)              \
	X(48) X(49) X(50) X(51) X(52) X(53) X(54) X(55)              \
	X(56) X(57) X(58) X(59) X(60) X(61) X(62) X(63)              \
	X(64) X(65) X(66) X(67) X(68) X(69) X(70) X(71)              \
	X(72) X(73) X(74) X(75) X(76) X(77) X(78) X(79)              \
	X(80) X(81) X(82) X(83) X(84) X(85) X(86) X(87)              \
	X(88) X(89) X(90) X(91) X(92) X(93) X(94) X(95)              \
	X(96) X(97) X(98) X(99) X(100) X(101) X(102) X(103)          \
	X(104) X(105) X(106) X(107) X(108) X(109) X(110) X(111)      \
	X(112) X(113) X(114) X(115) X(116) X(117) X(118) X(119)      \
	X(120) X(121) X(122) X(123) X(124) X(125) X(126) X(127)

// clang-format on

/*
 * Each target is noinline so a real symbol exists in kallsyms and
 * BTF, and the asm volatile barrier prevents the compiler from
 * folding the body away. notrace is deliberately NOT set: BPF
 * fentry/fexit attach goes through the same ftrace machinery that
 * notrace excludes a function from, so marking these notrace would
 * make them unattachable.
 */
#define BPFMAN_E2E_DECLARE_TARGET(n)                                           \
  noinline long bpfman_e2e_target_##n(unsigned long arg);
BPFMAN_E2E_FOR_EACH_SLOT(BPFMAN_E2E_DECLARE_TARGET)
#undef BPFMAN_E2E_DECLARE_TARGET

#define BPFMAN_E2E_DEFINE_TARGET(n)                                            \
  noinline long bpfman_e2e_target_##n(unsigned long arg) {                     \
    asm volatile("" : : "r"(arg));                                             \
    return (long)arg;                                                          \
  }
BPFMAN_E2E_FOR_EACH_SLOT(BPFMAN_E2E_DEFINE_TARGET)
#undef BPFMAN_E2E_DEFINE_TARGET

typedef long (*bpfman_e2e_target_fn)(unsigned long);

struct bpfman_e2e_slot {
  unsigned int slot;
  bpfman_e2e_target_fn fn;
  atomic64_t trigger_count;
};

#define BPFMAN_E2E_SLOT_ENTRY(n)                                               \
  {.slot = n, .fn = bpfman_e2e_target_##n, .trigger_count = ATOMIC64_INIT(0)},
static struct bpfman_e2e_slot bpfman_e2e_slots[BPFMAN_E2E_NUM_SLOTS] = {
    BPFMAN_E2E_FOR_EACH_SLOT(BPFMAN_E2E_SLOT_ENTRY)};
#undef BPFMAN_E2E_SLOT_ENTRY

static struct dentry *bpfman_e2e_root;

/*
 * Any write(2) to a trigger file invokes the corresponding target
 * function exactly once and returns the byte count to satisfy
 * standard write semantics. Buffer contents are ignored: the test
 * harness drives event count by issuing N write calls, not by
 * encoding N in the buffer. This keeps the kernel side trivial
 * (no parsing, no copy_from_user) and the per-call overhead at
 * one syscall plus one indirect call.
 */
static ssize_t bpfman_e2e_trigger_write(struct file *file,
                                        const char __user *buf, size_t count,
                                        loff_t *ppos) {
  struct bpfman_e2e_slot *slot = file->private_data;
  long ret;

  if (!slot || !slot->fn)
    return -EINVAL;
  ret = slot->fn(slot->slot);
  trace_bpfman_e2e_ping(slot->slot, ret);
  atomic64_inc(&slot->trigger_count);
  return count;
}

static ssize_t bpfman_e2e_count_read(struct file *file, char __user *buf,
                                     size_t count, loff_t *ppos) {
  struct bpfman_e2e_slot *slot = file->private_data;
  char tmp[32];
  int len;

  if (!slot)
    return -EINVAL;

  len = scnprintf(tmp, sizeof(tmp), "%lld\n",
                  (long long)atomic64_read(&slot->trigger_count));
  return simple_read_from_buffer(buf, count, ppos, tmp, len);
}

static const struct file_operations bpfman_e2e_trigger_fops = {
    .owner = THIS_MODULE,
    .open = simple_open,
    .write = bpfman_e2e_trigger_write,
    .llseek = noop_llseek,
};

static const struct file_operations bpfman_e2e_count_fops = {
    .owner = THIS_MODULE,
    .open = simple_open,
    .read = bpfman_e2e_count_read,
    .llseek = noop_llseek,
};

static int __init bpfman_e2e_init(void) {
  int i;
  char name[32];

  bpfman_e2e_root = debugfs_create_dir("bpfman_e2e", NULL);
  if (IS_ERR(bpfman_e2e_root))
    return PTR_ERR(bpfman_e2e_root);

  for (i = 0; i < BPFMAN_E2E_NUM_SLOTS; i++) {
    snprintf(name, sizeof(name), "trigger_%03d", i);
    debugfs_create_file(name, 0600, bpfman_e2e_root, &bpfman_e2e_slots[i],
                        &bpfman_e2e_trigger_fops);

    snprintf(name, sizeof(name), "count_%03d", i);
    debugfs_create_file(name, 0400, bpfman_e2e_root, &bpfman_e2e_slots[i],
                        &bpfman_e2e_count_fops);
  }

  pr_info("bpfman_e2e_targets: %d slots ready under "
          "/sys/kernel/debug/bpfman_e2e/\n",
          BPFMAN_E2E_NUM_SLOTS);
  return 0;
}

static void __exit bpfman_e2e_exit(void) {
  debugfs_remove_recursive(bpfman_e2e_root);
  pr_info("bpfman_e2e_targets: unloaded\n");
}

module_init(bpfman_e2e_init);
module_exit(bpfman_e2e_exit);

MODULE_LICENSE("GPL");
MODULE_AUTHOR("bpfman authors");
MODULE_DESCRIPTION("Dedicated kmod-backed attach targets for bpfman e2e tests");
MODULE_VERSION("0.1");
