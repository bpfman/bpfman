// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program XDP counter object where the middle program in the
// chain returns XDP_DROP. The XDP dispatcher's default proceed-on
// includes XDP_PASS and dispatcher_return, so XDP_DROP is *not* in
// the proceed-on bitmask and terminates the chain at that point.
// Side effect: the packet is dropped at A's ingress, so the kernel
// ICMP responder never gets it and the userspace ping reports loss.
// Tests use the PingExpectDrop helper, which sends N requests but
// tolerates 100% reply loss; the BPF counter still advances for
// every program that runs, regardless of whether the packet
// completes its traversal.
//
// Companion to multi_prog_xdp_counter.bpf.c (the
// all-proceed case where every program returns XDP_PASS and the
// chain runs to completion).

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

NET_COUNTER_MAP(mxda_count);
NET_COUNTER_MAP(mxdb_count);
NET_COUNTER_MAP(mxdc_count);

#define XDP_COUNT_PROG(prog_name, map_var, weight_var, verdict)                \
  int prog_name(struct xdp_md *ctx) {                                          \
    void *data = (void *)(long)ctx->data;                                      \
    void *data_end = (void *)(long)ctx->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return verdict;                                                            \
  }

SEC("xdp")
XDP_COUNT_PROG(mxdp_chain_a, mxda_count, weight_a, XDP_PASS)

SEC("xdp")
XDP_COUNT_PROG(mxdp_chain_b, mxdb_count, weight_b, XDP_DROP)

SEC("xdp")
XDP_COUNT_PROG(mxdp_chain_c, mxdc_count, weight_c, XDP_PASS)

char _license[] SEC("license") = "Dual BSD/GPL";
