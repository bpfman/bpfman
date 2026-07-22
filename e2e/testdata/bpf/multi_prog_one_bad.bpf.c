// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Two XDP programs in one object, used to prove that loading a single
// named program does not drag the rest of the object through the
// verifier. "good" is a trivial XDP_PASS that verifies cleanly.
// "bad" reads a packet byte with no data_end bounds check, so the
// verifier rejects it ("invalid access to packet") at load time --
// the object still compiles, the rejection only happens in the
// kernel. A load of only "good" must succeed even though "bad" shares
// the object; loading "bad" must fail.
//
// Both functions are in the "xdp" section and load as separate
// programs, keyed by function name.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

SEC("xdp")
int good(struct xdp_md *ctx) { return XDP_PASS; }

SEC("xdp")
int bad(struct xdp_md *ctx) {
  char *p = (char *)(unsigned long)ctx->data;
  return p[100]; // no bounds check against data_end -> verifier reject
}

char _license[] SEC("license") = "Dual BSD/GPL";
