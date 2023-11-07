// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfd

// clang-format off
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
// clang-format on

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

struct syscalls_enter_open_args {
  unsigned long long unused;
  long syscall_nr;
  long filename_ptr;
  long flags;
  long mode;
};

SEC("tracepoint/sys_enter_openat")
int enter_openat(struct syscalls_enter_open_args *ctx) {
  bpf_printk(" TP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
