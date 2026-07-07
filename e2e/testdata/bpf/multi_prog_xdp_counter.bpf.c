// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program XDP counter object. Three XDP programs share one
// object file; tests attach all three at distinct dispatcher
// priorities to the same veth, then detach in a staggered pattern.
//
// XDP chaining verdict:
// XDP uses a bpfman dispatcher (BPF_PROG_TYPE_EXT targeting a
// generated XDP dispatcher) just like TC. The XDP dispatcher's
// default proceed-on includes XDP_PASS and dispatcher_return, so a
// program returning XDP_PASS continues the chain to the next program
// -- this is different from the TC dispatcher case (where TC_ACT_OK
// terminates and TC_ACT_PIPE continues). XDP_PASS is the natural
// return for an observer program that wants the kernel to deliver
// the packet up the stack, so no special "chain" verdict is needed:
// the multi-program chain composes from ordinary observer programs.

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

NET_COUNTER_MAP(mxdp_a_count);
NET_COUNTER_MAP(mxdp_b_count);
NET_COUNTER_MAP(mxdp_c_count);

#define XDP_COUNT_PROG(prog_name, map_var, weight_var)                         \
  int prog_name(struct xdp_md *ctx) {                                          \
    void *data = (void *)(long)ctx->data;                                      \
    void *data_end = (void *)(long)ctx->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return XDP_PASS;                                                           \
  }

SEC("xdp")
XDP_COUNT_PROG(mxdp_a, mxdp_a_count, weight_a)

SEC("xdp")
XDP_COUNT_PROG(mxdp_b, mxdp_b_count, weight_b)

SEC("xdp")
XDP_COUNT_PROG(mxdp_c, mxdp_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";
