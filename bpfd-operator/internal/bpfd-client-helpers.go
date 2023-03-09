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

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml"
	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/apis/v1alpha1"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"google.golang.org/grpc/credentials"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	BpfdNs                 = "bpfd"
	BpfdOperatorName       = "bpfd-operator"
	BpfdDsName             = "bpfd-daemon"
	BpfdConfigName         = "bpfd-config"
	BpfdDaemonManifestPath = "./config/bpfd-deployment/daemonset.yaml"
	bpfdMapFs              = "/run/bpfd/fs/maps"
	DefaultConfigPath      = "/etc/bpfd/bpfd.toml"
	DefaultRootCaPath      = "/etc/bpfd/certs/ca/ca.crt"
	DefaultClientCertPath  = "/etc/bpfd/certs/bpfd-client/tls.crt"
	DefaultClientKeyPath   = "/etc/bpfd/certs/bpfd-client/tls.key"
)

type Tls struct {
	CaCert string `toml:"ca_cert"`
	Cert   string `toml:"cert"`
	Key    string `toml:"key"`
}

func LoadConfig() Tls {
	tlsConfig := Tls{
		CaCert: DefaultRootCaPath,
		Cert:   DefaultClientCertPath,
		Key:    DefaultClientKeyPath,
	}

	log.Printf("Reading %s ...\n", DefaultConfigPath)
	file, err := os.Open(DefaultConfigPath)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err == nil {
		err = toml.Unmarshal(b, &tlsConfig)
		if err != nil {
			log.Printf("Unmarshal failed: err %+v\n", err)
		}
	} else {
		log.Printf("Read %s failed: err %+v\n", DefaultConfigPath, err)
	}

	return tlsConfig
}

func LoadTLSCredentials(tlsFiles Tls) (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := os.ReadFile(tlsFiles.CaCert)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair(tlsFiles.Cert, tlsFiles.Key)
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	return credentials.NewTLS(config), nil
}

func BuildBpfdLoadRequest(bpf_program_config *bpfdiov1alpha1.BpfProgramConfig) (*gobpfd.LoadRequest, error) {
	loadRequest := gobpfd.LoadRequest{
		SectionName: bpf_program_config.Spec.Name,
		Location:    bpf_program_config.Spec.ByteCode,
	}

	// Map program type (ultimately we should make this an ENUM in the API)
	switch bpf_program_config.Spec.Type {
	case "XDP":
		loadRequest.ProgramType = gobpfd.ProgramType_XDP

		if bpf_program_config.Spec.AttachPoint.NetworkMultiAttach != nil {
			var proc_on []gobpfd.ProceedOn
			if len(bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.ProceedOn) > 0 {
				for _, proceedOnStr := range bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.ProceedOn {
					if action, ok := gobpfd.ProceedOn_value[string(proceedOnStr)]; !ok {
						return nil, fmt.Errorf("invalid proceedOn value %s for BpfProgramConfig %s",
							string(proceedOnStr), bpf_program_config.Name)
					} else {
						proc_on = append(proc_on, gobpfd.ProceedOn(action))
					}
				}
			}

			loadRequest.AttachType = &gobpfd.LoadRequest_NetworkMultiAttach{
				NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
					Priority:  int32(bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.Priority),
					Iface:     bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.Interface,
					ProceedOn: proc_on,
				},
			}
		} else {
			return nil, fmt.Errorf("invalid attach type for program type: XDP")
		}

	case "TC":
		loadRequest.ProgramType = gobpfd.ProgramType_TC

		if bpf_program_config.Spec.AttachPoint.NetworkMultiAttach != nil {
			var direction gobpfd.Direction
			switch bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.Direction {
			case "INGRESS":
				direction = gobpfd.Direction_INGRESS
			case "EGRESS":
				direction = gobpfd.Direction_EGRESS
			default:
				// Default to INGRESS
				bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.Direction = "INGRESS"
				direction = gobpfd.Direction_INGRESS
			}

			loadRequest.AttachType = &gobpfd.LoadRequest_NetworkMultiAttach{
				NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
					Priority:  int32(bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.Priority),
					Iface:     bpf_program_config.Spec.AttachPoint.NetworkMultiAttach.Interface,
					Direction: direction,
				},
			}
		} else {
			return nil, fmt.Errorf("invalid attach type for program type: XDP")
		}
	case "TRACEPOINT":
		loadRequest.ProgramType = gobpfd.ProgramType_TRACEPOINT

		if bpf_program_config.Spec.AttachPoint.SingleAttach != nil {
			loadRequest.AttachType = &gobpfd.LoadRequest_SingleAttach{
				SingleAttach: &gobpfd.SingleAttach{
					Name: bpf_program_config.Spec.AttachPoint.SingleAttach.Name,
				},
			}
		} else {
			return nil, fmt.Errorf("invalid attach type for program type: TRACEPOINT")
		}
	default:
		// Add a condition and exit don't requeue, an ensuing update to BpfProgramConfig
		// should fix this
		return nil, fmt.Errorf("invalid Program Type: %v", bpf_program_config.Spec.Type)
	}

	return &loadRequest, nil
}

func BuildBpfdUnloadRequest(uuid string) *gobpfd.UnloadRequest {
	return &gobpfd.UnloadRequest{
		Id: uuid,
	}
}

// GetMapsForUUID returns any maps for the specified bpf program
// which bpfd is managing
func GetMapsForUUID(uuid string) (map[string]string, error) {
	maps := map[string]string{}
	programMapPath := fmt.Sprintf("%s/%s", bpfdMapFs, uuid)

	// The directory may not be created instantaneously by bpfd so wait 10 seconds
	if err := filepath.Walk(programMapPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Name() != uuid {
			maps[info.Name()] = path
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return maps, nil
}

// ExistingRequests rebuilds the LoadRequests needed to actually get the node
// to the desired state
type ExistingReq struct {
	Uuid string
	Req  *bpfdiov1alpha1.BpfProgramConfigSpec
}

type ProgramKey struct {
	Name     string
	ProgType string
}

// CreateExistingState takes bpfd state via the list API and
// transforms it to k8s bpfd API state.
func CreateExistingState(nodeState []*gobpfd.ListResponse_ListResult) (map[ProgramKey]ExistingReq, error) {
	existingRequests := map[ProgramKey]ExistingReq{}

	for _, bpfdProg := range nodeState {
		var existingConfigSpec *bpfdiov1alpha1.BpfProgramConfigSpec

		switch bpfdProg.ProgramType.String() {
		case "XDP":
			existingConfigSpec = &bpfdiov1alpha1.BpfProgramConfigSpec{
				Name:         bpfdProg.Name,
				Type:         bpfdProg.ProgramType.String(),
				ByteCode:     bpfdProg.Location,
				AttachPoint:  *AttachConversion(bpfdProg),
				NodeSelector: metav1.LabelSelector{},
			}
		case "TC":
			existingConfigSpec = &bpfdiov1alpha1.BpfProgramConfigSpec{
				Name:         bpfdProg.Name,
				Type:         bpfdProg.ProgramType.String(),
				ByteCode:     bpfdProg.Location,
				AttachPoint:  *AttachConversion(bpfdProg),
				NodeSelector: metav1.LabelSelector{},
			}
		case "TRACEPOINT":
			existingConfigSpec = &bpfdiov1alpha1.BpfProgramConfigSpec{
				Name:         bpfdProg.Name,
				Type:         bpfdProg.ProgramType.String(),
				ByteCode:     bpfdProg.Location,
				AttachPoint:  *AttachConversion(bpfdProg),
				NodeSelector: metav1.LabelSelector{},
			}
		default:
			return nil, fmt.Errorf("invalid existing program type: %s", bpfdProg.ProgramType.String())
		}

		key := ProgramKey{
			Name:     bpfdProg.Name,
			ProgType: bpfdProg.ProgramType.String(),
		}

		// Don't overwrite existing entries
		if _, ok := existingRequests[key]; ok {
			return nil, fmt.Errorf("cannot have two programs loaded with the same type and section name")
		}

		existingRequests[key] = ExistingReq{
			Uuid: bpfdProg.Id,
			Req:  existingConfigSpec,
		}
	}

	return existingRequests, nil
}

type BpfdAttachType interface {
	GetNetworkMultiAttach() *gobpfd.NetworkMultiAttach
	GetSingleAttach() *gobpfd.SingleAttach
}

// AttachConversion changes a bpfd core API attachType (represented by the
// bpfdAttachType interface) to a bpfd k8s API Attachment type.
func AttachConversion(attachment BpfdAttachType) *bpfdiov1alpha1.BpfProgramAttachPoint {
	if attachment.GetNetworkMultiAttach() != nil {
		proceedOn := []bpfdiov1alpha1.ProceedOnValue{}
		for _, entry := range attachment.GetNetworkMultiAttach().ProceedOn {
			proceedOn = append(proceedOn, bpfdiov1alpha1.ProceedOnValue(entry.String()))
		}

		return &bpfdiov1alpha1.BpfProgramAttachPoint{
			NetworkMultiAttach: &bpfdiov1alpha1.BpfNetworkMultiAttach{
				Interface: attachment.GetNetworkMultiAttach().Iface,
				Priority:  attachment.GetNetworkMultiAttach().Priority,
				Direction: attachment.GetNetworkMultiAttach().Direction.String(),
				ProceedOn: proceedOn,
			},
		}
	}

	if attachment.GetSingleAttach() != nil {
		return &bpfdiov1alpha1.BpfProgramAttachPoint{
			SingleAttach: &bpfdiov1alpha1.BpfSingleAttach{
				Name: attachment.GetSingleAttach().Name,
			},
		}
	}

	panic("Attachment Type is unknown")
}

func LoadAndConfigureBpfdDs(config *corev1.ConfigMap) *appsv1.DaemonSet {
	// Load static bpfd deployment from disk
	file, err := os.Open(BpfdDaemonManifestPath)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, nil)

	staticBpfdDeployment := obj.(*appsv1.DaemonSet)

	// Runtime Configurable fields
	bpfdNamespace := config.Data["bpfd.namespace"]
	bpfdImage := config.Data["bpfd.image"]
	bpfdAgentImage := config.Data["bpfd.agent.image"]
	bpfdLogLevel := config.Data["bpfd.log.level"]

	// Annotate the log level on the ds so we get automatic restarts on changes.
	if staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations == nil {
		staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}
	staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations["bpfd.io.bpfd.loglevel"] = bpfdLogLevel
	staticBpfdDeployment.Name = "bpfd-daemon"
	staticBpfdDeployment.Namespace = bpfdNamespace
	staticBpfdDeployment.Spec.Template.Spec.Containers[0].Image = bpfdImage
	staticBpfdDeployment.Spec.Template.Spec.Containers[1].Image = bpfdAgentImage
	controllerutil.AddFinalizer(staticBpfdDeployment, "bpfd.io.operator/finalizer")

	return staticBpfdDeployment
}
