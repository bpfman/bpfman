// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/types.h>
#include <signal.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} uretprobe_stats_map SEC(".maps");

SEC("uretprobe/uretprobe_counter")
static __u32 uretprobe_counter(struct pt_regs *ctx) {

  __u32 index = 0;
  __u64 initValue = 1, *valp;

  valp = bpf_map_lookup_elem(&uretprobe_stats_map, &index);
  if (!valp) {
    bpf_map_update_elem(&uretprobe_stats_map, &index, &initValue, BPF_ANY);
    return 0;
  }
  __sync_fetch_and_add(valp, 1);
  bpf_printk("uretprobe called");

  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";