/* SPDX-License-Identifier: GPL-2.0 */
#include <linux/bpf.h>
#include <linux/in.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define TC_METADATA_SECTION "tc_metadata"
#define TC_DISPATCHER_VERSION 1
#define TC_DISPATCHER_RETVAL 31
#define MAX_DISPATCHER_ACTIONS 10

struct tc_dispatcher_config {
	__u8 num_progs_enabled;
	__u32 chain_call_actions[MAX_DISPATCHER_ACTIONS];
	__u32 run_prios[MAX_DISPATCHER_ACTIONS];
};
static volatile const struct tc_dispatcher_config conf = {};

__attribute__ ((noinline))
int prog0(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog1(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog2(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog3(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog4(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog5(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog6(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog7(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog8(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int prog9(struct __sk_buff *ctx) {
    volatile int ret = TC_DISPATCHER_RETVAL;

    if (!ctx)
        return TC_ACT_SHOT;
    return ret;
}

__attribute__ ((noinline))
int compat_test(struct __sk_buff *ctx) {
        volatile int ret = TC_DISPATCHER_RETVAL;

        if (!ctx)
          return TC_ACT_SHOT;
        return ret;
}


SEC("classifier/dispatcher")
int tc_dispatcher(struct __sk_buff *ctx)
{
        __u8 num_progs_enabled = conf.num_progs_enabled;
        int ret;

        if (num_progs_enabled < 1)
            goto out;
        ret = prog0(ctx);
        if (!((1U << ret) & conf.chain_call_actions[0]))
            return ret;

        if (num_progs_enabled < 2)
            goto out;
        ret = prog1(ctx);
        if (!((1U << ret) & conf.chain_call_actions[1]))
            return ret;
        
        if (num_progs_enabled < 3)
            goto out;
        ret = prog2(ctx);
        if (!((1U << ret) & conf.chain_call_actions[2]))
            return ret;

        if (num_progs_enabled < 4)
            goto out;
        ret = prog3(ctx);
        if (!((1U << ret) & conf.chain_call_actions[3]))
            return ret;

        if (num_progs_enabled < 5)
            goto out;
        ret = prog4(ctx);
        if (!((1U << ret) & conf.chain_call_actions[4]))
            return ret;
        
        if (num_progs_enabled < 6)
            goto out;
        ret = prog5(ctx);
        if (!((1U << ret) & conf.chain_call_actions[5]))
            return ret;
        
        if (num_progs_enabled < 7)
            goto out;
        ret = prog6(ctx);
        if (!((1U << ret) & conf.chain_call_actions[6]))
            return ret;

        if (num_progs_enabled < 8)
            goto out;
        ret = prog7(ctx);
        if (!((1U << ret) & conf.chain_call_actions[7]))
            return ret;

        if (num_progs_enabled < 9)
            goto out;
        ret = prog8(ctx);
        if (!((1U << ret) & conf.chain_call_actions[8]))
            return ret;

        if (num_progs_enabled < 10)
            goto out;
        ret = prog9(ctx);
        if (!((1U << ret) & conf.chain_call_actions[9]))
            return ret;

        /* keep a reference to the compat_test() function so we can use it
         * as an freplace target in xdp_multiprog__check_compat() in libxdp
         */
        if (num_progs_enabled < 11)
                goto out;
        ret = compat_test(ctx);
out:
        return TC_ACT_OK;
}

SEC("classifier/pass")
int tc_pass(struct __sk_buff *ctx)
{
        return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";
__uint(dispatcher_version, TC_DISPATCHER_VERSION) SEC(TC_METADATA_SECTION);