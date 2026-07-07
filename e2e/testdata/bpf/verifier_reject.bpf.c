// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// A kprobe program the kernel verifier must reject, used to prove the
// CLI surfaces the full verifier log on a failed load. It looks up a
// hash-map element and dereferences the result without the null check
// the verifier requires, so the verifier rejects it with "invalid mem
// access 'map_value_or_null'". A hash lookup can miss, so the verifier
// treats the result as maybe-null; an array map would not, since an
// in-bounds index always resolves. The object compiles cleanly; the
// rejection only happens in the kernel at load time.
//
// A kprobe loads directly into the kernel and is verified at program
// load, unlike XDP and TC, which bpfman loads as freplace dispatcher
// extensions whose verification is deferred. The verifier log therefore
// surfaces from `program load`, not from a later attach.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} verifier_reject_map SEC(".maps");

SEC("kprobe/verifier_reject")
int verifier_reject(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&verifier_reject_map, &key);
  (*val)++; // no null check -> verifier reject: invalid mem access
            // 'map_value_or_null'
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
