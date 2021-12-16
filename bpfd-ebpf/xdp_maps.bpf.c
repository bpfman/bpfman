/* SPDX-License-Identifier: GPL-2.0 */
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

/* This is the data record stored in the map */
struct datarec {
	__u64 rx_packets;
	/* Assignment#1: Add byte counters */
};

#define XDP_ACTION_MAX (XDP_REDIRECT + 1)

/* Lesson#1: See how a map is defined.
 * - Here an array with XDP_ACTION_MAX (max_)entries are created.
 * - The idea is to keep stats per (enum) xdp_action
 */
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__type(key, __u32);
	__type(value, struct datarec);
	__uint(max_entries, XDP_ACTION_MAX);
} xdp_stats_map1 SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__type(key, __u32);
	__type(value, struct datarec);
	__uint(max_entries, XDP_ACTION_MAX);
} xdp_stats_map2 SEC(".maps");

/* LLVM maps __sync_fetch_and_add() as a built-in function to the BPF atomic add
 * instruction (that is BPF_STX | BPF_XADD | BPF_W for word sizes)
 */
#ifndef lock_xadd
#define lock_xadd(ptr, val)	((void) __sync_fetch_and_add(ptr, val))
#endif

SEC("xdp/stats")
int  xdp_stats1_func(struct xdp_md *ctx)
{
	// void *data_end = (void *)(long)ctx->data_end;
	// void *data     = (void *)(long)ctx->data;
	struct datarec *rec;
	__u32 key = XDP_PASS; /* XDP_PASS = 2 */

	/* Lookup in kernel BPF-side return pointer to actual data record */
	rec = bpf_map_lookup_elem(&xdp_stats_map1, &key);
	/* BPF kernel-side verifier will reject program if the NULL pointer
	 * check isn't performed here. Even-though this is a static array where
	 * we know key lookup XDP_PASS always will succeed.
	 */
	if (!rec)
		return XDP_ABORTED;

	/* Multiple CPUs can access data record. Thus, the accounting needs to
	 * use an atomic operation.
	 */
	lock_xadd(&rec->rx_packets, 1);
        /* Assignment#1: Add byte counters
         * - Hint look at struct xdp_md *ctx (copied below)
         *
         * Assignment#3: Avoid the atomic operation
         * - Hint there is a map type named BPF_MAP_TYPE_PERCPU_ARRAY
         */

	return XDP_PASS;
}

char _license[] SEC("license") = "GPL";
