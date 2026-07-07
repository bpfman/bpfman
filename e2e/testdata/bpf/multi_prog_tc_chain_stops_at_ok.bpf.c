// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program TC counter object where the middle program in the
// chain returns TC_ACT_OK to terminate the dispatcher chain. Tests
// attach all three at ascending priorities (a/b/c at 50/60/70) and
// fire a single PingExact burst:
//
//   position 0 (mtc_chain_a, priority 50): runs, counts, returns
//     TC_ACT_PIPE  -> chain continues.
//   position 1 (mtc_chain_b, priority 60): runs, counts, returns
//     TC_ACT_OK    -> chain stops at the dispatcher (OK is *not*
//     in the default proceed-on bitmask `pipe | dispatcher_return`).
//   position 2 (mtc_chain_c, priority 70): never runs, counter
//     stays at zero.
//
// Counter values prove the contract directly: a and b are non-zero
// and equal to events * weight, c is exactly zero. See
// multi_prog_tc_counter.bpf.c for the long-form proceed-on
// rationale.

#include <linux/pkt_cls.h>

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

NET_COUNTER_MAP(mca_count);
NET_COUNTER_MAP(mcb_count);
NET_COUNTER_MAP(mcc_count);

#define TC_COUNT_PROG(prog_name, map_var, weight_var, verdict)                 \
  int prog_name(struct __sk_buff *skb) {                                       \
    void *data = (void *)(long)skb->data;                                      \
    void *data_end = (void *)(long)skb->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return verdict;                                                            \
  }

SEC("classifier/mtc_chain_a")
TC_COUNT_PROG(mtc_chain_a, mca_count, weight_a, TC_ACT_PIPE)

SEC("classifier/mtc_chain_b")
TC_COUNT_PROG(mtc_chain_b, mcb_count, weight_b, TC_ACT_OK)

SEC("classifier/mtc_chain_c")
TC_COUNT_PROG(mtc_chain_c, mcc_count, weight_c, TC_ACT_PIPE)

char _license[] SEC("license") = "Dual BSD/GPL";
