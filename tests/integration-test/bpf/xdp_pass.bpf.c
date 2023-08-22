#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

SEC("xdp/pass")
int pass(struct xdp_md *ctx)
{
	bpf_printk("XDP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8, GLOBAL_u32);
        return XDP_PASS;
}

char _license[] SEC("license") = "GPL";
