// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Some fentry/fexit test code

// clang-format off
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
// clang-format on

SEC("fentry/do_unlinkat")
int BPF_PROG(test_fentry) {
  bpf_printk("fentry: do_unlinkat ENTER\n");
  return 0;
}

SEC("fexit/do_unlinkat")
int BPF_PROG(test_fexit) {
  bpf_printk("fexit: do_unlinkat EXIT\n");
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
