// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program TCX counter object. Three TCX programs share one
// object file; tests attach all three to the same veth ingress at
// distinct priorities. TCX uses native kernel multi-prog support
// (kernel 6.6+) rather than a bpfman dispatcher.
//
// TCX chaining verdict differs from TC. TCX shares the TC numbering
// for terminal codes (TC_ACT_OK accepts and stops, TC_ACT_SHOT
// drops, TC_ACT_REDIRECT redirects), but the "continue to the next
// program" verdict is TC_ACT_UNSPEC (-1), aliased in the kernel as
// TCX_NEXT. This is *not* TC_ACT_PIPE -- TC_ACT_PIPE is what the
// bpfman TC dispatcher uses because its default proceed-on
// bitmask permits PIPE; TCX has no dispatcher and follows the
// kernel's native TCX_NEXT contract instead. A TCX program
// returning TC_ACT_OK or TC_ACT_PIPE terminates the chain at that
// point, so every program here returns TC_ACT_UNSPEC to allow
// every active program in the chain to see the packet.

#include <linux/pkt_cls.h>

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

NET_COUNTER_MAP(mtcx_a_count);
NET_COUNTER_MAP(mtcx_b_count);
NET_COUNTER_MAP(mtcx_c_count);

#define TCX_COUNT_PROG(prog_name, map_var, weight_var)                         \
  int prog_name(struct __sk_buff *skb) {                                       \
    void *data = (void *)(long)skb->data;                                      \
    void *data_end = (void *)(long)skb->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return TC_ACT_UNSPEC;                                                      \
  }

SEC("classifier/mtcx_a")
TCX_COUNT_PROG(mtcx_a, mtcx_a_count, weight_a)

SEC("classifier/mtcx_b")
TCX_COUNT_PROG(mtcx_b, mtcx_b_count, weight_b)

SEC("classifier/mtcx_c")
TCX_COUNT_PROG(mtcx_c, mtcx_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";
