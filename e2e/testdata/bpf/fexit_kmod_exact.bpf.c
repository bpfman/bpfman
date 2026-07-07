// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Single-program fexit counter for the kmod-targeting e2e tests.
// Attaches at the return of one of the bpfman_e2e_targets module's
// private slot functions; the test harness overrides AttachFunc at
// load time to pick a specific slot, so the SEC name here is a
// placeholder satisfying the verifier rather than a load-bearing
// target. No PID filter is needed because the slot's only writer
// is the test that owns it.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

volatile const __u64 weight = 0;

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} fx_count SEC(".maps");

SEC("fexit/bpfman_e2e_target_0")
int test_fexit(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&fx_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
