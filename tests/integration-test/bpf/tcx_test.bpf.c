// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// clang-format off
#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
// clang-format on

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

SEC("classifier/tcx_pass")
int tcx_pass(struct __sk_buff *skb) {
  bpf_printk(" TCX: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return TCX_PASS;
}

SEC("classifier/tcx_next")
int tcx_next(struct __sk_buff *skb) {
  bpf_printk(" TCX: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return TCX_NEXT;
}

SEC("classifier/tcx_drop")
int tcx_drop(struct __sk_buff *skb) {
  bpf_printk(" TCX: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return TCX_DROP;
}

SEC("classifier/tcx_redirect")
int tcx_redirect(struct __sk_buff *skb) {
  bpf_printk(" TCX: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return TCX_REDIRECT;
}

char _license[] SEC("license") = "Dual BSD/GPL";
