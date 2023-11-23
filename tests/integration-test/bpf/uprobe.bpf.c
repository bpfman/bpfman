// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Some uprobe test code

// clang-format off
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
// clang-format on

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

SEC("uprobe/my_uprobe")
int my_uprobe(struct pt_regs *ctx) {
  bpf_printk(" UP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return 0;
}

SEC("uretprobe/my_uretprobe")
int my_uretprobe(struct pt_regs *ctx) {
  bpf_printk("URP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
