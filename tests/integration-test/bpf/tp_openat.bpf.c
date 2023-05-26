#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

volatile const __u32 GLOBAL_1 = 0;
volatile const __u32 GLOBAL_2 = 0;

struct syscalls_enter_open_args {
	unsigned long long unused;
	long syscall_nr;
	long filename_ptr;
	long flags;
	long mode;
};

SEC("tracepoint/sys_enter_openat")
int enter_openat(struct syscalls_enter_open_args *ctx)
{
	bpf_printk(" TP: GLOBAL_1: 0x%08X, GLOBAL_2: 0x%08X", GLOBAL_1, GLOBAL_2);
    return 0;
}

char _license[] SEC("license") = "GPL";
