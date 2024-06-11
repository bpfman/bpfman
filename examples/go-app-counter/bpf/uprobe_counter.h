// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/types.h>
#include <signal.h>

#include <bpf/bpf_helpers.h>

struct uprobedatarec {
  __u64 counter;
} uprobedatarec;

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, uprobedatarec);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} uprobe_stats_map SEC(".maps");

SEC("uprobe/uprobe_counter")
static __u32 uprobe_counter(struct pt_regs *ctx) {

  __u32 index = 0;
  struct uprobedatarec *rec = bpf_map_lookup_elem(&uprobe_stats_map, &index);
  if (!rec)
    return 1;

  rec->counter++;
  bpf_printk("uprobe called");

  return 0;
}
