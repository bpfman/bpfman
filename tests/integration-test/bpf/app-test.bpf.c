// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// clang-format off
#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
// clang-format on

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

void print_globals(char *prefix) {
  bpf_printk("%s: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", prefix, GLOBAL_u8,
             GLOBAL_u32);
}

SEC("fentry/do_unlinkat")
int BPF_PROG(fentry_test) {
  print_globals("FEN");
  return 0;
}

SEC("fexit/do_unlinkat")
int BPF_PROG(fexit_test) {
  print_globals("FEX");
  return 0;
}

SEC("kprobe/kprobe_test")
int kprobe_test(struct pt_regs *ctx) {
  print_globals(" KP");
  return 0;
}

SEC("kretprobe/kretprobe_test")
int kretprobe_test(struct pt_regs *ctx) {
  print_globals("KRP");
  return 0;
}

SEC("classifier/tc_pass_test")
int tc_pass_test(struct __sk_buff *skb) {
  print_globals(" TC");
  return TC_ACT_OK;
}

SEC("classifier/tcx_pass_test")
int tcx_pass_test(struct __sk_buff *skb) {
  print_globals("TCX");
  return TCX_PASS;
}

SEC("classifier/tcx_next_test")
int tcx_next_test(struct __sk_buff *skb) {
  print_globals("TCX");
  return TCX_NEXT;
}

SEC("classifier/tcx_drop_test")
int tcx_drop_test(struct __sk_buff *skb) {
  print_globals("TCX");
  return TCX_DROP;
}

SEC("classifier/tcx_redirect_test")
int tcx_redirect_test(struct __sk_buff *skb) {
  print_globals("TCX");
  return TCX_REDIRECT;
}

SEC("tracepoint/tracepoint_test")
int tracepoint_test(void *ctx) {
  print_globals(" TP");
  return 0;
}

SEC("uprobe/uprobe_test")
int uprobe_test(struct pt_regs *ctx) {
  print_globals(" UP");
  return 0;
}

SEC("uretprobe/my_uretprobe")
int uretprobe_test(struct pt_regs *ctx) {
  print_globals("URP");
  return 0;
}

SEC("xdp")
int xdp_pass_test(struct xdp_md *ctx) {
  print_globals("XDP");
  return XDP_PASS;
}

char _license[] SEC("license") = "Dual BSD/GPL";