// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Single-program XDP counter for exact-equality e2e assertions.
// Same packet-content filter as tc_exact.bpf.c (IPv4 ICMP echo
// requests only) but at the XDP hook. Returns XDP_PASS on every
// packet so traffic still flows; only matching packets bump the
// counter.

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>

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
} xdp_count SEC(".maps");

SEC("xdp")
int pass(struct xdp_md *ctx) {
  void *data = (void *)(long)ctx->data;
  void *data_end = (void *)(long)ctx->data_end;

  struct ethhdr *eth = data;
  if ((void *)(eth + 1) > data_end)
    return XDP_PASS;
  if (eth->h_proto != bpf_htons(ETH_P_IP))
    return XDP_PASS;

  struct iphdr *ip = (void *)(eth + 1);
  if ((void *)(ip + 1) > data_end)
    return XDP_PASS;
  if (ip->protocol != IPPROTO_ICMP)
    return XDP_PASS;

  struct icmp_min_hdr *icmp = (void *)ip + (ip->ihl * 4);
  if ((void *)(icmp + 1) > data_end)
    return XDP_PASS;
  if (icmp->type != ICMP_ECHO)
    return XDP_PASS;

  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&xdp_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight);
  return XDP_PASS;
}

char _license[] SEC("license") = "Dual BSD/GPL";
