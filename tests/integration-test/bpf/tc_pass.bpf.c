#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>

volatile const __u8 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;

SEC("classifier/pass")
int pass(struct __sk_buff *skb)
{
	bpf_printk(" TC: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8, GLOBAL_u32);
	return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";
