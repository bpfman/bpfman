// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/types.h>
#include <signal.h>

#include <bpf/bpf_helpers.h>

struct kprobedatarec {
  __u64 counter;
} kprobedatarec;

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, kprobedatarec);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} kprobe_stats_map SEC(".maps");

SEC("kprobe/kprobe_counter")
static __u32 kprobe_counter(struct pt_regs *ctx) {

  __u32 index = 0;
  struct kprobedatarec *rec = bpf_map_lookup_elem(&kprobe_stats_map, &index);
  if (!rec)
    return 1;

  rec->counter++;
  bpf_printk("kprobe called");

  return 0;
}
