// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program TC counter object with per-program verdict globals.
// Tests choose what each program returns via load-time global data
// (`verdict_a`, `verdict_b`, `verdict_c`), which lets a single
// .bpf.o cover both halves of the custom-proceed-on contract:
//
//   AllProceed_CustomProceedOn: all three globals = TC_ACT_OK,
//     attach with WithProceedOn including OK -- chain proceeds on
//     a verdict the *default* bitmask would have stopped on.
//
//   ChainStopsAtPipe_CustomProceedOn: outer verdicts = TC_ACT_OK,
//     middle = TC_ACT_PIPE, attach with WithProceedOn including OK
//     but *excluding* PIPE -- chain stops on a verdict the default
//     bitmask would have proceeded on.
//
// Both cases prove the WithProceedOn knob actually plumbs through
// to the dispatcher's bitmask: the chain behaviour swaps relative
// to what the default would do, given the same return verdicts.

#include <linux/pkt_cls.h>

#include "multi_prog_net_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;
volatile const __u32 verdict_a = TC_ACT_PIPE;
volatile const __u32 verdict_b = TC_ACT_PIPE;
volatile const __u32 verdict_c = TC_ACT_PIPE;

NET_COUNTER_MAP(mtcv_a_count);
NET_COUNTER_MAP(mtcv_b_count);
NET_COUNTER_MAP(mtcv_c_count);

#define TC_PARAM_PROG(prog_name, map_var, weight_var, verdict_var)             \
  int prog_name(struct __sk_buff *skb) {                                       \
    void *data = (void *)(long)skb->data;                                      \
    void *data_end = (void *)(long)skb->data_end;                              \
    if (is_ipv4_icmp_echo(data, data_end))                                     \
      bump_counter(&map_var, weight_var);                                      \
    return verdict_var;                                                        \
  }

SEC("classifier/mtcv_a")
TC_PARAM_PROG(mtcv_a, mtcv_a_count, weight_a, verdict_a)

SEC("classifier/mtcv_b")
TC_PARAM_PROG(mtcv_b, mtcv_b_count, weight_b, verdict_b)

SEC("classifier/mtcv_c")
TC_PARAM_PROG(mtcv_c, mtcv_c_count, weight_c, verdict_c)

char _license[] SEC("license") = "Dual BSD/GPL";
