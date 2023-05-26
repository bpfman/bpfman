#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

volatile const __u32 GLOBAL_1 = 0;
volatile const __u32 GLOBAL_2 = 0;

SEC("xdp/pass")
int  pass(struct xdp_md *ctx)
{
	bpf_printk("XDP: GLOBAL_1: 0x%08X, GLOBAL_2: 0x%08X", GLOBAL_1, GLOBAL_2);
        return XDP_PASS;
}

char _license[] SEC("license") = "GPL";
