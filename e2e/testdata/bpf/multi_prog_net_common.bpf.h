// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman
//
// Shared building blocks for the network multi-program counter
// tests (tc, tcx, xdp). Pulls together the IPv4 ICMP echo
// classifier and a small counter-bump helper so each multi-program
// .bpf.c stays focused on its per-program glue (SEC strings,
// per-program maps, weight globals, and the verdict return).
//
// Why an ICMP echo filter rather than counting every packet:
// veth.PingExact(N) drives N ICMP echo requests; ARP, IPv6 RA/ND
// and other incidental traffic arrive too. Filtering by
// "IPv4 + IPPROTO_ICMP + ICMP_ECHO" inside BPF means
// `events * weight == count` exactly, regardless of test
// environment noise.

#ifndef E2E_TESTDATA_MULTI_PROG_NET_COMMON_BPF_H
#define E2E_TESTDATA_MULTI_PROG_NET_COMMON_BPF_H

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>

#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>

#define ICMP_ECHO 8
#define IPPROTO_ICMP 1

/* linux/icmp.h pulls in userland headers; declare what we need. */
struct icmp_min_hdr {
  __u8 type;
  __u8 code;
  __u16 checksum;
};

#define NET_COUNTER_MAP(name)                                                  \
  struct {                                                                     \
    __uint(type, BPF_MAP_TYPE_ARRAY);                                          \
    __type(key, __u32);                                                        \
    __type(value, __u64);                                                      \
    __uint(max_entries, 1);                                                    \
    __uint(pinning, LIBBPF_PIN_NONE);                                          \
  } name SEC(".maps")

/* is_ipv4_icmp_echo returns 1 if the packet starting at `data`
 * (ending at `data_end`) is a complete IPv4 ICMP echo request,
 * else 0. Pure packet parsing, no map I/O. */
static __always_inline int is_ipv4_icmp_echo(void *data, void *data_end) {
  struct ethhdr *eth = data;
  if ((void *)(eth + 1) > data_end)
    return 0;
  if (eth->h_proto != bpf_htons(ETH_P_IP))
    return 0;

  struct iphdr *ip = (void *)(eth + 1);
  if ((void *)(ip + 1) > data_end)
    return 0;
  if (ip->protocol != IPPROTO_ICMP)
    return 0;

  struct icmp_min_hdr *icmp = (void *)ip + (ip->ihl * 4);
  if ((void *)(icmp + 1) > data_end)
    return 0;
  if (icmp->type != ICMP_ECHO)
    return 0;

  return 1;
}

/* bump_counter adds `weight` to the 1-entry __u64 array at `map`.
 * Used after a positive is_ipv4_icmp_echo match. */
static __always_inline void bump_counter(void *map, __u64 weight) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(map, &key);
  if (val)
    __sync_fetch_and_add(val, weight);
}

#endif /* E2E_TESTDATA_MULTI_PROG_NET_COMMON_BPF_H */
