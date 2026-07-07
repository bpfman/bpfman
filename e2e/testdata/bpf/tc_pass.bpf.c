// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Minimal TC programs for e2e testing. Each function has a distinct
// name so tests can verify priority tie-breaking by program name.

#include <linux/bpf.h>
#include <linux/pkt_cls.h>

#include <bpf/bpf_helpers.h>

SEC("classifier/alpha")
int alpha(struct __sk_buff *skb) { return TC_ACT_OK; }

SEC("classifier/beta")
int beta(struct __sk_buff *skb) { return TC_ACT_OK; }

char _license[] SEC("license") = "Dual BSD/GPL";
