// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Some kprobe test code

// clang-format off
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
// clang-format on

volatile const __u32 sampling = 0;
volatile const __u8 trace_messages = 0;
volatile const __u8 enable_rtt = 0;
volatile const __u8 enable_pca = 0;
volatile const __u8 enable_dns_tracking = 0;
volatile const __u8 enable_flows_filtering = 0;
volatile const __u16 dns_port = 0;
volatile const __u8 enable_network_events_monitoring = 0;
volatile const __u8 network_events_monitoring_groupid = 0;

void print_globals() {
  bpf_printk("sampling: 0x%08X, trace_messages: 0x%02X, enable_rtt: 0x%02X, "
             "enable_pca: 0x%02X, enable_dns_tracking: 0x%02X, "
             "enable_flows_filtering: 0x%02X, dns_port: 0x%02X, "
             "enable_network_events_monitoring: 0x%02X, "
             "network_events_monitoring_groupid: 0x%02X\n",
             sampling, trace_messages, enable_rtt, enable_pca,
             enable_dns_tracking, enable_flows_filtering, dns_port,
             enable_network_events_monitoring,
             network_events_monitoring_groupid);
}

SEC("kprobe/kprobe_globals")
int kprobe_globals(struct pt_regs *ctx) {
  print_globals();
  return 0;
}

SEC("kretprobe/kprobe_globals")
int kretprobe_globals(struct pt_regs *ctx) {
  print_globals();
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";
