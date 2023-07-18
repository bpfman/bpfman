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
	XdpProgramControllerFinalizer        = "bpfd.io.xdpprogramcontroller/finalizer"
	TcProgramControllerFinalizer         = "bpfd.io.tcprogramcontroller/finalizer"
	TracepointProgramControllerFinalizer = "bpfd.io.tracepointprogramcontroller/finalizer"
	XdpProgramInterface                  = "bpfd.io.xdpprogramcontroller/interface"
	TcProgramInterface                   = "bpfd.io.tcprogramcontroller/interface"
	TracepointProgramTracepoint          = "bpfd.io.tracepointprogramcontroller/tracepoint"
	BpfProgramOwnerLabel                 = "bpfd.io/ownedByProgram"
	K8sHostLabel                         = "kubernetes.io/hostname"
	BpfdNs                               = "bpfd"
	BpfdOperatorName                     = "bpfd-operator"
	BpfdDsName                           = "bpfd-daemon"
	BpfdConfigName                       = "bpfd-config"
	BpfdDaemonManifestPath               = "./config/bpfd-deployment/daemonset.yaml"
	BpfdMapFs                            = "/run/bpfd/fs/maps"
	DefaultConfigPath                    = "/etc/bpfd/bpfd.toml"
	DefaultRootCaPath                    = "/etc/bpfd/certs/ca/ca.crt"
	DefaultCertPath                      = "/etc/bpfd/certs/bpfd/tls.crt"
	DefaultKeyPath                       = "/etc/bpfd/certs/bpfd/tls.key"
	DefaultClientCertPath                = "/etc/bpfd/certs/bpfd-client/tls.crt"
	DefaultClientKeyPath                 = "/etc/bpfd/certs/bpfd-client/tls.key"
	DefaultType                          = "tcp"
	DefaultPath                          = "/run/bpfd/bpfd.sock"
	DefaultPort                          = 50051
	DefaultEnabled                       = true
)

// Must match the internal bpfd-api mappings
type SupportedProgramType int32

const (
	Tc         SupportedProgramType = 3
	Xdp        SupportedProgramType = 6
	Tracepoint SupportedProgramType = 5
)

func (p SupportedProgramType) Uint32() *uint32 {
	progTypeInt := uint32(p)
	return &progTypeInt
}

func FromString(p string) (*SupportedProgramType, error) {
	var programType SupportedProgramType
	switch p {
	case "tc":
		programType = Tc
	case "xdp":
		programType = Xdp
	case "tracepoint":
		programType = Tracepoint
	default:
		return nil, fmt.Errorf("unknown program type: %s", p)
	}

	return &programType, nil
}

func (p SupportedProgramType) String() string {
	switch p {
	case Tc:
		return "tc"
	case Xdp:
		return "xdp"
	case Tracepoint:
		return "tracepoint"
	default:
		return ""
	}
}

// -----------------------------------------------------------------------------
// Finalizers
// -----------------------------------------------------------------------------

const (
	// BpfdOperatorFinalizer is the finalizer that holds a BPF program from
	// deletion until cleanup can be performed.
	BpfdOperatorFinalizer = "bpfd.io.operator/finalizer"
)
