/* SPDX-License-Identifier: GPL-2.0 */

#undef TRACE_SYSTEM
#define TRACE_SYSTEM bpfman_e2e

#if !defined(_BPFMAN_E2E_TRACE_H) || defined(TRACE_HEADER_MULTI_READ)
#define _BPFMAN_E2E_TRACE_H

#include <linux/tracepoint.h>

TRACE_EVENT(bpfman_e2e_ping, TP_PROTO(unsigned int slot, unsigned long value),
            TP_ARGS(slot, value),
            TP_STRUCT__entry(__field(unsigned int, slot)
                                 __field(unsigned long, value)),
            TP_fast_assign(__entry->slot = slot; __entry->value = value;),
            TP_printk("slot=%u value=%lu", __entry->slot, __entry->value));

#endif /* _BPFMAN_E2E_TRACE_H */

#undef TRACE_INCLUDE_PATH
#define TRACE_INCLUDE_PATH .
#undef TRACE_INCLUDE_FILE
#define TRACE_INCLUDE_FILE bpfman_e2e_trace

#include <trace/define_trace.h>
