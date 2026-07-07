// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program TC counter object. Three TC programs share one
// object file; tests attach all three at distinct priorities to the
// same veth ingress, then detach in a staggered pattern. Each
// program counts only IPv4 ICMP echo requests (see
// multi_prog_net_common.bpf.h) and increments its own
// LIBBPF_PIN_NONE counter by its own per-program weight. All three
// sit in the dispatcher chain at once, so a single PingExact(N)
// burst fires every active program once per packet -- the
// staggered detach then yields distinct events-per-wave per
// program.
//
// Why TC_ACT_PIPE rather than TC_ACT_OK:
// The kernel only allows one BPF program at a single TC priority,
// so bpfman generates a dispatcher program that internally fans
// out to the user's programs in priority order. After each user
// program returns, the dispatcher checks the program's return
// code against its `proceed_on` bitmask and either continues to
// the next program in the chain or stops and returns to the
// kernel. The default proceed-on for TC is
// `pipe | dispatcher_return`, which deliberately *excludes*
// TC_ACT_OK because OK means "I've decided -- accept this
// packet"; honouring that decision is the whole point of the
// dispatcher contract. Programs that want continuation must
// return TC_ACT_PIPE ("I observed the packet, hand off to the
// next filter"). For a counter that wants every program in the
// chain to see every packet, PIPE is the canonical return.

#include <linux/pkt_cls.h>

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

NET_COUNTER_MAP(mtc_a_count);
NET_COUNTER_MAP(mtc_b_count);
NET_COUNTER_MAP(mtc_c_count);

#define TC_COUNT_PROG(prog_name, map_var, weight_var)                          \
  int prog_name(struct __sk_buff *skb) {                                       \
    void *data = (void *)(long)skb->data;                                      \
    void *data_end = (void *)(long)skb->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return TC_ACT_PIPE;                                                        \
  }

SEC("classifier/mtc_a")
TC_COUNT_PROG(mtc_a, mtc_a_count, weight_a)

SEC("classifier/mtc_b")
TC_COUNT_PROG(mtc_b, mtc_b_count, weight_b)

SEC("classifier/mtc_c")
TC_COUNT_PROG(mtc_c, mtc_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";
