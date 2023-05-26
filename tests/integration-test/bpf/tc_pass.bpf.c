#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>

volatile const __u32 GLOBAL_1 = 0;
volatile const __u32 GLOBAL_2 = 0;

SEC("classifier/pass")
int pass(struct __sk_buff *skb)
{
	bpf_printk(" TC: GLOBAL_1: 0x%08X, GLOBAL_2: 0x%08X", GLOBAL_1, GLOBAL_2);
	return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";
