// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program TCX counter object where the middle program in the
// chain returns TC_ACT_OK to terminate the kernel TCX chain. TCX
// shares the TC verdict numbering for terminal codes, so OK
// behaves the same way: the kernel honours the verdict and stops
// dispatching to subsequent programs in the chain. The other two
// programs return TC_ACT_UNSPEC (TCX_NEXT) so the chain continues
// up to the OK. Counter assertions at positions 0/1/2:
// events*weight, events*weight, 0.
//
// Companion to multi_prog_tcx_counter.bpf.c (the all-proceed
// case where every program returns TC_ACT_UNSPEC and the chain
// runs to completion).

#include <linux/pkt_cls.h>

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

NET_COUNTER_MAP(mxca_count);
NET_COUNTER_MAP(mxcb_count);
NET_COUNTER_MAP(mxcc_count);

#define TCX_COUNT_PROG(prog_name, map_var, weight_var, verdict)                \
  int prog_name(struct __sk_buff *skb) {                                       \
    void *data = (void *)(long)skb->data;                                      \
    void *data_end = (void *)(long)skb->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return verdict;                                                            \
  }

SEC("classifier/mtcx_chain_a")
TCX_COUNT_PROG(mtcx_chain_a, mxca_count, weight_a, TC_ACT_UNSPEC)

SEC("classifier/mtcx_chain_b")
TCX_COUNT_PROG(mtcx_chain_b, mxcb_count, weight_b, TC_ACT_OK)

SEC("classifier/mtcx_chain_c")
TCX_COUNT_PROG(mtcx_chain_c, mxcc_count, weight_c, TC_ACT_UNSPEC)

char _license[] SEC("license") = "Dual BSD/GPL";
