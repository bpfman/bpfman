// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program XDP counter object with per-program verdict globals.
// Tests choose what each program returns via load-time global data
// (`verdict_a`, `verdict_b`, `verdict_c`), which lets a single
// .bpf.o cover both halves of the custom-proceed-on contract:
//
//   AllProceed_CustomProceedOn: all three globals = XDP_DROP,
//     attach with WithProceedOn including DROP -- chain proceeds
//     on a verdict the default bitmask would have stopped on. Side
//     effect: packets are dropped (last program determines kernel
//     verdict), so the test uses PingExpectDrop.
//
//   ChainStopsAtPass_CustomProceedOn: outer verdicts = XDP_DROP,
//     middle = XDP_PASS, attach with WithProceedOn including DROP
//     but *excluding* PASS -- chain stops on a verdict the default
//     bitmask would have proceeded on. The middle program's PASS
//     also tells the kernel to deliver the packet, so PingExact
//     works.
//
// Both cases prove the WithProceedOn knob plumbs through to the
// dispatcher: the chain behaviour swaps relative to what the
// default would do, given the same return verdicts.

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;
volatile const __u32 verdict_a = XDP_PASS;
volatile const __u32 verdict_b = XDP_PASS;
volatile const __u32 verdict_c = XDP_PASS;

NET_COUNTER_MAP(mxdv_a_count);
NET_COUNTER_MAP(mxdv_b_count);
NET_COUNTER_MAP(mxdv_c_count);

#define XDP_PARAM_PROG(prog_name, map_var, weight_var, verdict_var)            \
  int prog_name(struct xdp_md *ctx) {                                          \
    void *data = (void *)(long)ctx->data;                                      \
    void *data_end = (void *)(long)ctx->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return verdict_var;                                                        \
  }

SEC("xdp")
XDP_PARAM_PROG(mxdv_a, mxdv_a_count, weight_a, verdict_a)

SEC("xdp")
XDP_PARAM_PROG(mxdv_b, mxdv_b_count, weight_b, verdict_b)

SEC("xdp")
XDP_PARAM_PROG(mxdv_c, mxdv_c_count, weight_c, verdict_c)

char _license[] SEC("license") = "Dual BSD/GPL";
