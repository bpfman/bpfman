// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfd

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/types.h>
#include <signal.h>

#include <bpf/bpf_helpers.h>

struct datarec {
  __u64 calls;
} datarec;

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, datarec);
  __uint(max_entries, 8);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} tracepoint_stats_map SEC(".maps");

struct kill_args {
  long long pad;
  long syscall_nr;
  long pid;
  long sig;
};

SEC("tracepoint/tracepoint_kill_recorder")
static __u32 tracepoint_kill_recorder(struct kill_args *ctx) {
  if (ctx->sig != SIGUSR1)
    return 0;

  __u32 index = 0;
  struct datarec *rec = bpf_map_lookup_elem(&tracepoint_stats_map, &index);
  if (!rec)
    return 1;

  rec->calls++;
  bpf_printk("process received SIGUSR1");

  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
