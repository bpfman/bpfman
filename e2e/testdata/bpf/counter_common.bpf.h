// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman
//
// Shared building blocks for the weighted counter programs used by
// exact-equality e2e assertions.
//
// Each translation unit must declare its own `expected_pid` global
// (and one `weight_*` global per program) before invoking
// COUNTER_PROG. The macros below provide:
//
//   COUNTER_MAP(name)
//     A 1-entry __u64 BPF_MAP_TYPE_ARRAY with LIBBPF_PIN_NONE so
//     each loaded copy of an object owns a private map. Tests open
//     it by kernel ID, never by pin path.
//
//   COUNTER_PROG(prog_name, map_name, weight)
//     A program body that filters on the load-time `expected_pid`
//     global and adds `weight` to the counter on every matching
//     event. Exact-equality tests assert `events * weight == count`
//     so misrouted events, missing globals, or shared maps surface
//     as wrong arithmetic, not as a missed signal.
//
//   TRACEPOINT_SLOT_COUNTER_PROG(prog_name, map_name, weight)
//     A program body for the e2e kmod's bpfman_e2e_ping tracepoint.
//     The tracepoint is shared, so each loaded program filters on the
//     load-time `expected_slot` global carried in the event payload.

#ifndef E2E_TESTDATA_COUNTER_COMMON_BPF_H
#define E2E_TESTDATA_COUNTER_COMMON_BPF_H

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct trace_event_raw_bpfman_e2e_ping {
  __u64 common;
  __u32 slot;
  __u32 __pad;
  unsigned long value;
};

#define COUNTER_MAP(name)                                                      \
  struct {                                                                     \
    __uint(type, BPF_MAP_TYPE_ARRAY);                                          \
    __type(key, __u32);                                                        \
    __type(value, __u64);                                                      \
    __uint(max_entries, 1);                                                    \
    __uint(pinning, LIBBPF_PIN_NONE);                                          \
  } name SEC(".maps")

#define COUNTER_PROG(prog_name, map_name, weight)                              \
  int prog_name(void *ctx) {                                                   \
    if ((bpf_get_current_pid_tgid() >> 32) != expected_pid)                    \
      return 0;                                                                \
    __u32 key = 0;                                                             \
    __u64 *val = bpf_map_lookup_elem(&map_name, &key);                         \
    if (val)                                                                   \
      __sync_fetch_and_add(val, weight);                                       \
    return 0;                                                                  \
  }

#define TRACEPOINT_SLOT_COUNTER_PROG(prog_name, map_name, weight)              \
  int prog_name(struct trace_event_raw_bpfman_e2e_ping *ctx) {                 \
    if (ctx->slot != expected_slot)                                            \
      return 0;                                                                \
    __u32 key = 0;                                                             \
    __u64 *val = bpf_map_lookup_elem(&map_name, &key);                         \
    if (val)                                                                   \
      __sync_fetch_and_add(val, weight);                                       \
    return 0;                                                                  \
  }

#endif /* E2E_TESTDATA_COUNTER_COMMON_BPF_H */
