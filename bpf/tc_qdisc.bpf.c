/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
/* Copyright Authors of bpfd */
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <linux/types.h>

#define DEBUG 1
#ifdef  DEBUG
/* Only use this for debug output. Notice output from bpf_trace_printk()
 * end-up in /sys/kernel/debug/tracing/trace_pipe
 */
#define bpf_debug(fmt, ...)						\
		({							\
			char ____fmt[] = fmt;				\
			bpf_trace_printk(____fmt, sizeof(____fmt),	\
				     ##__VA_ARGS__);			\
		})
#else
#define bpf_debug(fmt, ...) { } while (0)
#endif

// qdisc_destroy format
// from: /sys/kernel/debug/tracing/events/qdisc/qdisc_destroy/format
// name: qdisc_destroy
// ID: 1426
// format:
//         field:unsigned short common_type;       offset:0;       size:2; signed:0;
//         field:unsigned char common_flags;       offset:2;       size:1; signed:0;
//         field:unsigned char common_preempt_count;       offset:3;       size:1; signed:0;
//         field:int common_pid;   offset:4;       size:4; signed:1;
//
//         field:__data_loc char[] dev;    offset:8;       size:4; signed:1;
//         field:__data_loc char[] kind;   offset:12;      size:4; signed:1;
//         field:u32 parent;       offset:16;      size:4; signed:0;
//         field:u32 handle;       offset:20;      size:4; signed:0;
typedef struct {
    __u64 __unused__;
    __u32 data_loc_dev;
    __u32 data_loc_kind;
    __u32 parent;
    __u32 handle;
} qdisc_destroy_args_t;

#define DEV_NAME_MAX_LEN 64
#define KIND_NAME_MAX_LEN 64

// We should use __u32 __length = args->data_loc##field > 16
// instead of sizeof(dst), but a verifier message is emitted:
// 2023/09/21 22:14:08 loading objects: field TpClsactQdiscDestroy: program tp_clsact_qdisc_destroy: load program:
// permission denied: invalid indirect access to stack R1 off=-128 size=65535 (36 line(s) omitted)
#define TP_DATA_LOC_READ(dst, field)                                        \
        do {                                                                \
            __u32 __offset = args->data_loc_##field & 0xFFFF;      \
            bpf_probe_read((void *)dst, sizeof(dst), (char *)args + __offset); \
        } while (0);

struct qdisc_event{
    char dev[DEV_NAME_MAX_LEN];
    char kind[KIND_NAME_MAX_LEN];
} ;

// Force emitting struct qdisc_event into the ELF.
const struct qdisc_event *unused __attribute__((unused));

// create perf event map to send qdisc_destroy events to user space.
struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(int));
    __uint(value_size, sizeof(int));
} perf_event_qdisc SEC(".maps");


SEC("tracepoint/clsact_qdisc_destroy")
int tp_clsact_qdisc_destroy(void *ctx) {
    qdisc_destroy_args_t *args = (qdisc_destroy_args_t *)ctx;
    struct qdisc_event event = {};

    TP_DATA_LOC_READ(event.dev, dev);
    TP_DATA_LOC_READ(event.kind, kind);

    // Sending event to user space
    long ret = bpf_perf_event_output(ctx, &perf_event_qdisc, BPF_F_CURRENT_CPU, &event, sizeof(struct qdisc_event));
    if (ret != 0) {
        bpf_debug("bpf_perf_event_output failed: %ld", ret);
    }

    return 0;
}

char _license[] SEC("license") = "GPL";
