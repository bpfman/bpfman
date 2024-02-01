/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package internal

import "fmt"

const (
	XdpProgramInterface         = "bpfman.io.xdpprogramcontroller/interface"
	TcProgramInterface          = "bpfman.io.tcprogramcontroller/interface"
	TracepointProgramTracepoint = "bpfman.io.tracepointprogramcontroller/tracepoint"
	KprobeProgramFunction       = "bpfman.io.kprobeprogramcontroller/function"
	UprobeProgramTarget         = "bpfman.io.uprobeprogramcontroller/target"
	UprobeContainerPid          = "bpfman.io.uprobeprogramcontroller/containerpid"
	UprobeNoContainersOnNode    = "bpfman.io.uprobeprogramcontroller/nocontainersonnode"
	BpfProgramOwnerLabel        = "bpfman.io/ownedByProgram"
	K8sHostLabel                = "kubernetes.io/hostname"
	DiscoveredLabel             = "bpfman.io/discoveredProgram"
	IdAnnotation                = "bpfman.io/ProgramId"
	UuidMetadataKey             = "bpfman.io/uuid"
	ProgramNameKey              = "bpfman.io/ProgramName"
	BpfmanNs                    = "bpfman"
	BpfmanOperatorName          = "bpfman-operator"
	BpfmanDsName                = "bpfman-daemon"
	BpfmanConfigName            = "bpfman-config"
	BpfmanCsiDriverName         = "csi.bpfman.io"
	BpfmanDaemonManifestPath    = "./config/bpfman-deployment/daemonset.yaml"
	BpfmanCsiDriverPath         = "./config/bpfman-deployment/csidriverinfo.yaml"
	BpfmanMapFs                 = "/run/bpfman/fs/maps"
	DefaultType                 = "tcp"
	DefaultPath                 = "/run/bpfman-sock/bpfman.sock"
	DefaultPort                 = 50051
	DefaultEnabled              = true
)

// -----------------------------------------------------------------------------
// Finalizers
// -----------------------------------------------------------------------------

const (
	// BpfmanOperatorFinalizer is the finalizer that holds a *Program from
	// deletion until cleanup can be performed.
	BpfmanOperatorFinalizer = "bpfman.io.operator/finalizer"
	// XdpProgramControllerFinalizer is the finalizer that holds an Xdp BpfProgram
	// object from deletion until cleanup can be performed.
	XdpProgramControllerFinalizer = "bpfman.io.xdpprogramcontroller/finalizer"
	// TcProgramControllerFinalizer is the finalizer that holds an Tc BpfProgram
	// object from deletion until cleanup can be performed.
	TcProgramControllerFinalizer = "bpfman.io.tcprogramcontroller/finalizer"
	// TracepointProgramControllerFinalizer is the finalizer that holds an Tracepoint
	// BpfProgram object from deletion until cleanup can be performed.
	TracepointProgramControllerFinalizer = "bpfman.io.tracepointprogramcontroller/finalizer"
	// KprobeProgramControllerFinalizer is the finalizer that holds a Kprobe
	// BpfProgram object from deletion until cleanup can be performed.
	KprobeProgramControllerFinalizer = "bpfman.io.kprobeprogramcontroller/finalizer"
	// KprobeProgramControllerFinalizer is the finalizer that holds a Uprobe
	// BpfProgram object from deletion until cleanup can be performed.
	UprobeProgramControllerFinalizer = "bpfman.io.uprobeprogramcontroller/finalizer"
)

// Must match the kernel's `bpf_prog_type` enum.
// https://elixir.bootlin.com/linux/v6.4.4/source/include/uapi/linux/bpf.h#L948
type ProgramType int32

const (
	Unspec ProgramType = iota
	SocketFilter
	Kprobe
	Tc
	SchedAct
	Tracepoint
	Xdp
	PerfEvent
	CgroupSkb
	CgroupSock
	LwtIn
	LwtOut
	LwtXmit
	SockOps
	SkSkb
	CgroupDevice
	SkMsg
	RawTracepoint
	CgroupSockAddr
	LwtSeg6Local
	LircMode2
	SkReuseport
	FlowDissector
	CgroupSysctl
	RawTracepointWritable
	CgroupSockopt
	Tracing
	StructOps
	Ext
	Lsm
	SkLookup
	Syscall
)

func (p ProgramType) Uint32() *uint32 {
	progTypeInt := uint32(p)
	return &progTypeInt
}

func FromString(p string) (*ProgramType, error) {
	var programType ProgramType
	switch p {
	case "tc":
		programType = Tc
	case "xdp":
		programType = Xdp
	case "tracepoint":
		programType = Tracepoint
	case "kprobe":
		programType = Kprobe
	case "uprobe":
		programType = Kprobe
	default:
		return nil, fmt.Errorf("unknown program type: %s", p)
	}

	return &programType, nil
}

func (p ProgramType) String() string {
	switch p {
	case Unspec:
		return "unspec"
	case SocketFilter:
		return "socket_filter"
	case Kprobe:
		return "kprobe"
	case Tc:
		return "tc"
	case SchedAct:
		return "sched_act"
	case Tracepoint:
		return "tracepoint"
	case Xdp:
		return "xdp"
	case PerfEvent:
		return "perf_event"
	case CgroupSkb:
		return "cgroup_skb"
	case CgroupSock:
		return "cgroup_sock"
	case LwtIn:
		return "lwt_in"
	case LwtOut:
		return "lwt_out"
	case LwtXmit:
		return "lwt_xmit"
	case SockOps:
		return "sock_ops"
	case SkSkb:
		return "sk_skb"
	case CgroupDevice:
		return "cgroup_device"
	case SkMsg:
		return "sk_msg"
	case RawTracepoint:
		return "raw_tracepoint"
	case CgroupSockAddr:
		return "cgroup_sock_addr"
	case LwtSeg6Local:
		return "lwt_seg6local"
	case LircMode2:
		return "lirc_mode2"
	case SkReuseport:
		return "sk_reuseport"
	case FlowDissector:
		return "flow_dissector"
	case CgroupSysctl:
		return "cgroup_sysctl"
	case RawTracepointWritable:
		return "raw_tracepoint_writable"
	case CgroupSockopt:
		return "cgroup_sockopt"
	case Tracing:
		return "tracing"
	case StructOps:
		return "struct_ops"
	case Ext:
		return "ext"
	case Lsm:
		return "lsm"
	case SkLookup:
		return "sk_lookup"
	case Syscall:
		return "syscall"
	default:
		return "INVALID_PROG_TYPE"
	}
}

// Define a constant string for Uprobe.  It has the same kernel ProgramType as
// Kprobe, so we can't use the ProgramType String() method above.
const UprobeString = "uprobe"

type ReconcileResult uint8

const (
	// No changes were made to k8s objects, and rescheduling another reconcile
	// is not necessary. The calling code may continue reconciling other
	// programs in it's list.
	Unchanged ReconcileResult = 0
	// Changes were made to k8s objects that we know will trigger another
	// reconcile.  Calling code should stop reconciling additional programs and
	// return immediately to avoid multiple concurrent reconcile threads.
	Updated ReconcileResult = 1
	// A retry should be scheduled. This should only be used when "Updated"
	// doesn't apply, but we want to trigger another reconcile anyway.  For
	// example, there was a transient error. The calling code may continue
	// reconciling other programs in it's list.
	Requeue ReconcileResult = 2
)

func (r ReconcileResult) String() string {
	switch r {
	case Unchanged:
		return "Unchanged"
	case Updated:
		return "Updated"
	case Requeue:
		return "Requeue"
	default:
		return fmt.Sprintf("INVALID RECONCILE RESULT (%d)", r)
	}
}
