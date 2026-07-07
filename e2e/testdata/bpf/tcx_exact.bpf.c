// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Single-program TCX counter for exact-equality e2e assertions.
// Same packet-content filter as tc_exact.bpf.c (IPv4 ICMP echo
// requests only) but attached at the TCX hook. See that file for
// the rationale.

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/pkt_cls.h>

#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>

#define ICMP_ECHO 8
#define IPPROTO_ICMP 1
struct icmp_min_hdr {
  __u8 type;
  __u8 code;
  __u16 checksum;
};

volatile const __u64 weight = 0;

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} tcx_count SEC(".maps");

SEC("classifier/tcx_stats")
int tcx_stats(struct __sk_buff *skb) {
  void *data = (void *)(long)skb->data;
  void *data_end = (void *)(long)skb->data_end;

  struct ethhdr *eth = data;
  if ((void *)(eth + 1) > data_end)
    return TC_ACT_OK;
  if (eth->h_proto != bpf_htons(ETH_P_IP))
    return TC_ACT_OK;

  struct iphdr *ip = (void *)(eth + 1);
  if ((void *)(ip + 1) > data_end)
    return TC_ACT_OK;
  if (ip->protocol != IPPROTO_ICMP)
    return TC_ACT_OK;

  struct icmp_min_hdr *icmp = (void *)ip + (ip->ihl * 4);
  if ((void *)(icmp + 1) > data_end)
    return TC_ACT_OK;
  if (icmp->type != ICMP_ECHO)
    return TC_ACT_OK;

  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&tcx_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight);
  return TC_ACT_OK;
}

char _license[] SEC("license") = "Dual BSD/GPL";
