// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfd

// clang-format off
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
// clang-format on

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

SEC("xdp")
int pass(struct xdp_md *ctx) {
  bpf_printk("XDP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return XDP_PASS;
}

char _license[] SEC("license") = "Dual BSD/GPL";
