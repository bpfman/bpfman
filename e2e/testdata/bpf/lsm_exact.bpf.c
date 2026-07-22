// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// LSM file_open counter for the e2e tests. Attaches to the file_open
// LSM hook and always allows (returns 0), counting only opens made by
// the fixture worker -- identified by its 8-byte marker comm, passed in
// as the target_comm global. file_open fires system-wide, so the comm
// filter is what makes the count deterministic: the box's own file
// activity (daemon, shell, bpftool) has a different comm and is ignored.
// The `lsm` bpfman-shell fixture leases the marker and drives the
// matching worker. Reading struct file would need vmlinux/CO-RE; the
// comm helper needs neither, so the program stays on plain UAPI headers.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

// target_comm is the fixture worker's comm, its first 8 bytes packed
// little-endian into a u64. Set per program instance via global data so
// concurrent lsm fixtures on one host stay isolated.
volatile const __u64 target_comm = 0;

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} lsm_count SEC(".maps");

SEC("lsm/file_open")
int test_lsm(void *ctx) {
  char comm[16] = {};
  bpf_get_current_comm(&comm, sizeof(comm));

  __u64 got = 0;
  __builtin_memcpy(&got, comm, sizeof(got));
  if (got != target_comm)
    return 0;

  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&lsm_count, &key);
  if (val)
    __sync_fetch_and_add(val, 1);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
