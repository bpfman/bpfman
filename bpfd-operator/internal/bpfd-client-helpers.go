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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/containers/image/docker/reference"
	toml "github.com/pelletier/go-toml"
	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/apis/v1alpha1"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"google.golang.org/grpc/credentials"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	DefaultCertPath        = "/etc/bpfd/certs/bpfd/tls.crt"
	DefaultKeyPath         = "/etc/bpfd/certs/bpfd/tls.key"
	DefaultClientCertPath  = "/etc/bpfd/certs/bpfd-client/tls.crt"
	DefaultClientKeyPath   = "/etc/bpfd/certs/bpfd-client/tls.key"
	DefaultPort            = 50051
)

var log = ctrl.Log.WithName("bpfd-internal-helpers")

type Tls struct {
	CaCert     string `toml:"ca_cert"`
	Cert       string `toml:"cert"`
	Key        string `toml:"key"`
	ClientCert string `toml:"client_cert"`
	ClientKey  string `toml:"client_key"`
}

type Endpoint struct {
	Port uint16 `toml:"port"`
}

type Grpc struct {
	Endpoint Endpoint `toml:"endpoint"`
}

type ConfigFileData struct {
	Tls  Tls  `toml:"tls"`
	Grpc Grpc `toml:"grpc"`
}

func LoadConfig() ConfigFileData {
	config := ConfigFileData{
		Tls: Tls{
			CaCert:     DefaultRootCaPath,
			Cert:       DefaultCertPath,
			Key:        DefaultKeyPath,
			ClientCert: DefaultClientCertPath,
			ClientKey:  DefaultClientKeyPath,
		},
		Grpc: Grpc{
			Endpoint: Endpoint{
				Port: DefaultPort,
			},
		},
	}

	log.Info("Reading...\n", "Default config path", DefaultConfigPath)
	file, err := os.Open(DefaultConfigPath)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err == nil {
		err = toml.Unmarshal(b, &config)
		if err != nil {
			log.Info("Unmarshal failed: err %+v\n", err)
		}
	} else {
		log.Info("Read config-path failed: err\n", "config-path", DefaultConfigPath, "err", err)
	}

	return config
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
	clientCert, err := tls.LoadX509KeyPair(tlsFiles.ClientCert, tlsFiles.ClientKey)
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

// ContainerConfigJSON represents ~/.docker/config.json file info
// See https://github.com/docker/docker/pull/12009
// Structure from https://github.com/kubernetes/kubernetes/blob/master/pkg/credentialprovider/config.go#L39
type ContainerConfigJSON struct {
	Auths ContainerConfig `json:"auths"`
	// +optional
	HTTPHeaders map[string]string `json:"HttpHeaders,omitempty"`
}

// DockerConfig represents the config file used by the docker CLI.
// This config that represents the credentials that should be used
// when pulling images from specific image repositories.
type ContainerConfig map[string]ContainerConfigEntry

// ContainerConfigEntry wraps a container config as a entry
type ContainerConfigEntry struct {
	Username string
	Password string
	Email    string
}

// dockerConfigEntryWithAuth is used solely for deserializing the Auth field
// into a dockerConfigEntry during JSON deserialization.
type ContainerConfigEntryWithAuth struct {
	// +optional
	Username string `json:"username,omitempty"`
	// +optional
	Password string `json:"password,omitempty"`
	// +optional
	Email string `json:"email,omitempty"`
	// +optional
	Auth string `json:"auth,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (ident *ContainerConfigEntry) UnmarshalJSON(data []byte) error {
	var tmp ContainerConfigEntryWithAuth
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}

	if len(tmp.Auth) == 0 {
		return nil
	}

	ident.Username, ident.Password, err = decodeContainerConfigFieldAuth(tmp.Auth)
	return err
}

// decodeContainerConfigFieldAuth deserializes the "auth" field from containercfg into a
// username and a password. The format of the auth field is base64(<username>:<password>).
func decodeContainerConfigFieldAuth(field string) (username, password string, err error) {

	var decoded []byte

	// StdEncoding can only decode padded string
	// RawStdEncoding can only decode unpadded string
	if strings.HasSuffix(strings.TrimSpace(field), "=") {
		// decode padded data
		decoded, err = base64.StdEncoding.DecodeString(field)
	} else {
		// decode unpadded data
		decoded, err = base64.RawStdEncoding.DecodeString(field)
	}

	if err != nil {
		return
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("unable to parse auth field, must be formatted as base64(username:password)")
		return
	}

	username = parts[0]
	password = parts[1]

	return
}

// Mimicking exactly what Kubernetes does to pull out auths from secrets:
// https://github.com/kubernetes/kubernetes/blob/master/pkg/credentialprovider/secrets/secrets.go#L29
func ParseAuth(c client.Client, secretName, secretNamespace string) (*ContainerConfig, error) {
	var creds ContainerConfig

	// Lookup the k8s Secret in the bpfd-namespace for repository authentication
	ctx := context.TODO()
	imageSecret := &v1.Secret{}

	if err := c.Get(ctx, types.NamespacedName{Namespace: secretNamespace, Name: secretName}, imageSecret); err != nil {
		return nil, fmt.Errorf("failed image auth secret %s: %v",
			secretName, err)
	}

	if containerConfigJSONBytes, containerConfigJSONExists := imageSecret.Data[v1.DockerConfigJsonKey]; (imageSecret.Type == v1.SecretTypeDockerConfigJson) && containerConfigJSONExists && (len(containerConfigJSONBytes) > 0) {
		containerConfigJSON := ContainerConfigJSON{}
		if err := json.Unmarshal(containerConfigJSONBytes, &containerConfigJSON); err != nil {
			return nil, err
		}

		creds = containerConfigJSON.Auths
	} else if dockercfgBytes, dockercfgExists := imageSecret.Data[v1.DockerConfigKey]; (imageSecret.Type == v1.SecretTypeDockercfg) && dockercfgExists && (len(dockercfgBytes) > 0) {
		dockercfg := ContainerConfig{}
		if err := json.Unmarshal(dockercfgBytes, &dockercfg); err != nil {
			return nil, err
		}

		creds = dockercfg
	}

	return &creds, nil
}

func BuildBpfdLoadRequest(bpf_program_config_spec *bpfdiov1alpha1.BpfProgramConfigSpec, name string, namespace string, c client.Client) (*gobpfd.LoadRequest, error) {
	loadRequest := gobpfd.LoadRequest{
		SectionName: bpf_program_config_spec.Name,
	}

	if bpf_program_config_spec.ByteCode.Image != nil {
		bytecodeImage := bpf_program_config_spec.ByteCode.Image
		var pullPolicy gobpfd.ImagePullPolicy

		ref, err := reference.ParseNamed(bytecodeImage.Url)
		if err != nil {
			return nil, err
		}

		var username, password string
		if bytecodeImage.ImagePullSecret != "" {
			creds, err := ParseAuth(c, bytecodeImage.ImagePullSecret, namespace)
			if err != nil {
				return nil, err
			}

			if creds == nil {
				return nil, fmt.Errorf("no registry credentials found in secret: %s", bytecodeImage.ImagePullSecret)
			}

			domain := reference.Domain(ref)

			// All docker.io image domains resolve to https://index.docker.io/v1/ in the credentials JSON file.
			if domain == "docker.io" || domain == "" {
				domain = "https://index.docker.io/v1/"
			}

			cred := (*creds)[domain]

			username = cred.Username
			password = cred.Password
		}

		switch bytecodeImage.ImagePullPolicy {
		case "Always":
			pullPolicy = gobpfd.ImagePullPolicy_Always
		case "IfNotPresent":
			pullPolicy = gobpfd.ImagePullPolicy_IfNotPresent
		case "Never":
			pullPolicy = gobpfd.ImagePullPolicy_Never
		}

		loadRequest.Location = &gobpfd.LoadRequest_Image{
			Image: &gobpfd.BytecodeImage{
				Url:             bytecodeImage.Url,
				ImagePullPolicy: pullPolicy,
				Username:        username,
				Password:        password,
			},
		}
	} else {
		loadRequest.Location = &gobpfd.LoadRequest_File{
			File: *bpf_program_config_spec.ByteCode.Path,
		}
	}

	// Map program type (ultimately we should make this an ENUM in the API)
	switch bpf_program_config_spec.Type {
	case "XDP":
		loadRequest.ProgramType = gobpfd.ProgramType_XDP

		if bpf_program_config_spec.AttachPoint.NetworkMultiAttach != nil {
			var proc_on []gobpfd.ProceedOn
			if len(bpf_program_config_spec.AttachPoint.NetworkMultiAttach.ProceedOn) > 0 {
				for _, proceedOnStr := range bpf_program_config_spec.AttachPoint.NetworkMultiAttach.ProceedOn {
					if action, ok := gobpfd.ProceedOn_value[string(proceedOnStr)]; !ok {
						return nil, fmt.Errorf("invalid proceedOn value %s for BpfProgramConfig %s",
							string(proceedOnStr), name)
					} else {
						proc_on = append(proc_on, gobpfd.ProceedOn(action))
					}
				}
			}

			if bpf_program_config_spec.AttachPoint.NetworkMultiAttach.InterfaceSelector.Interface != nil {
				loadRequest.AttachType = &gobpfd.LoadRequest_NetworkMultiAttach{
					NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
						Priority:  int32(bpf_program_config_spec.AttachPoint.NetworkMultiAttach.Priority),
						Iface:     *bpf_program_config_spec.AttachPoint.NetworkMultiAttach.InterfaceSelector.Interface,
						ProceedOn: proc_on,
					},
				}
			} else {
				return nil, fmt.Errorf("invalid interface selector for program type: XDP")
			}
		} else {
			return nil, fmt.Errorf("invalid attach type for program type: XDP")
		}

	case "TC":
		loadRequest.ProgramType = gobpfd.ProgramType_TC

		if bpf_program_config_spec.AttachPoint.NetworkMultiAttach != nil {
			var direction gobpfd.Direction
			switch bpf_program_config_spec.AttachPoint.NetworkMultiAttach.Direction {
			case "INGRESS":
				direction = gobpfd.Direction_INGRESS
			case "EGRESS":
				direction = gobpfd.Direction_EGRESS
			default:
				// Default to INGRESS
				bpf_program_config_spec.AttachPoint.NetworkMultiAttach.Direction = "INGRESS"
				direction = gobpfd.Direction_INGRESS
			}

			if bpf_program_config_spec.AttachPoint.NetworkMultiAttach.InterfaceSelector.Interface != nil {
				loadRequest.AttachType = &gobpfd.LoadRequest_NetworkMultiAttach{
					NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
						Priority:  int32(bpf_program_config_spec.AttachPoint.NetworkMultiAttach.Priority),
						Iface:     *bpf_program_config_spec.AttachPoint.NetworkMultiAttach.InterfaceSelector.Interface,
						Direction: direction,
					},
				}
			} else {
				return nil, fmt.Errorf("invalid interface selector for program type: TC")
			}
		} else {
			return nil, fmt.Errorf("invalid attach type for program type: TC")
		}
	case "TRACEPOINT":
		loadRequest.ProgramType = gobpfd.ProgramType_TRACEPOINT

		if bpf_program_config_spec.AttachPoint.SingleAttach != nil {
			loadRequest.AttachType = &gobpfd.LoadRequest_SingleAttach{
				SingleAttach: &gobpfd.SingleAttach{
					Name: bpf_program_config_spec.AttachPoint.SingleAttach.Name,
				},
			}
		} else {
			return nil, fmt.Errorf("invalid attach type for program type: TRACEPOINT")
		}
	default:
		// Add a condition and exit don't requeue, an ensuing update to BpfProgramConfig
		// should fix this
		return nil, fmt.Errorf("invalid Program Type: %v", bpf_program_config_spec.Type)
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
	Name        string
	ProgType    string
	AttachPoint string
}

// CreateExistingState takes bpfd state via the list API and
// transforms it to k8s bpfd API state.
func CreateExistingState(nodeState []*gobpfd.ListResponse_ListResult) (map[ProgramKey]ExistingReq, error) {
	existingRequests := map[ProgramKey]ExistingReq{}

	for _, bpfdProg := range nodeState {
		var existingConfigSpec *bpfdiov1alpha1.BpfProgramConfigSpec
		attachType := AttachConversion(bpfdProg)
		byteCode := BytecodeConversion(bpfdProg)

		switch bpfdProg.ProgramType.String() {
		case "XDP":
			existingConfigSpec = &bpfdiov1alpha1.BpfProgramConfigSpec{
				Name:         bpfdProg.Name,
				Type:         bpfdProg.ProgramType.String(),
				ByteCode:     *byteCode,
				AttachPoint:  *attachType,
				NodeSelector: metav1.LabelSelector{},
			}
		case "TC":
			existingConfigSpec = &bpfdiov1alpha1.BpfProgramConfigSpec{
				Name:         bpfdProg.Name,
				Type:         bpfdProg.ProgramType.String(),
				ByteCode:     *byteCode,
				AttachPoint:  *attachType,
				NodeSelector: metav1.LabelSelector{},
			}
		case "TRACEPOINT":
			existingConfigSpec = &bpfdiov1alpha1.BpfProgramConfigSpec{
				Name:         bpfdProg.Name,
				Type:         bpfdProg.ProgramType.String(),
				ByteCode:     *byteCode,
				AttachPoint:  *attachType,
				NodeSelector: metav1.LabelSelector{},
			}
		default:
			return nil, fmt.Errorf("invalid existing program type: %s", bpfdProg.ProgramType.String())
		}

		key := ProgramKey{
			Name:        bpfdProg.Name,
			ProgType:    bpfdProg.ProgramType.String(),
			AttachPoint: StringifyAttachType(attachType),
		}

		// Don't overwrite existing entries
		if _, ok := existingRequests[key]; ok {
			return nil, fmt.Errorf("cannot have two programs loaded with the same type, section name, and attachpoint")
		}

		existingRequests[key] = ExistingReq{
			Uuid: bpfdProg.Id,
			Req:  existingConfigSpec,
		}
	}

	return existingRequests, nil
}

// This Interface is derived from the go protobuf bindings to abstract the
// Bytecode Location type without being coupled to a specific GRPC Service Type
type BpfdBytecode interface {
	GetImage() *gobpfd.BytecodeImage
	GetFile() string
}

// BytecodeConversion changes a bpfd core API bytecode Location (represented by the
// bpfdLocation interface) to a bpfd k8s API BytecodeSelector.
func BytecodeConversion(location BpfdBytecode) *bpfdiov1alpha1.BytecodeSelector {
	if location.GetImage() != nil {

		return &bpfdiov1alpha1.BytecodeSelector{
			Image: &bpfdiov1alpha1.BytecodeImage{
				Url:             location.GetImage().Url,
				ImagePullPolicy: bpfdiov1alpha1.PullPolicy(location.GetImage().GetImagePullPolicy().String()),
			},
		}
	}

	if location.GetFile() != "" {
		path := location.GetFile()
		return &bpfdiov1alpha1.BytecodeSelector{
			Path: &path,
		}
	}

	panic("Bytecode Location Type is unknown")
}

// This Interface is derived from the go protobuf bindings to abstract the
// attachType without being coupled to a specific GRPC Service Type
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
				InterfaceSelector: bpfdiov1alpha1.InterfaceSelector{
					Interface: &attachment.GetNetworkMultiAttach().Iface,
				},
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

func StringifyAttachType(attach *bpfdiov1alpha1.BpfProgramAttachPoint) string {
	if attach.NetworkMultiAttach != nil {
		if attach.NetworkMultiAttach.InterfaceSelector.Interface != nil {
			return fmt.Sprintf("%s_%s_%d",
				*attach.NetworkMultiAttach.InterfaceSelector.Interface,
				attach.NetworkMultiAttach.Direction,
				attach.NetworkMultiAttach.Priority,
			)
		} else {
			return fmt.Sprintf("%s_%s_%d",
				"unknown",
				attach.NetworkMultiAttach.Direction,
				attach.NetworkMultiAttach.Priority,
			)
		}
	}

	return attach.SingleAttach.Name
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

func PrintNodeState(state map[ProgramKey]ExistingReq) {
	for k, v := range state {
		log.V(1).Info("Node State---->", "ProgramKey", k, "Value", v)
	}
}

func ConvertToBpfProgramConfigSpecList(BpfProgramConfigSpec *bpfdiov1alpha1.BpfProgramConfigSpec, NodeIface string) (nodeState []bpfdiov1alpha1.BpfProgramConfigSpec) {
	bpfProgramConfigSpecList := []bpfdiov1alpha1.BpfProgramConfigSpec{}

	if BpfProgramConfigSpec.AttachPoint.NetworkMultiAttach != nil {
		if BpfProgramConfigSpec.AttachPoint.NetworkMultiAttach.InterfaceSelector.Interface != nil {
			bpfProgramConfigSpecList = append(bpfProgramConfigSpecList, *BpfProgramConfigSpec)
			return bpfProgramConfigSpecList
		}

		if BpfProgramConfigSpec.AttachPoint.NetworkMultiAttach.InterfaceSelector.PrimaryNodeInterface != nil {
			modBpfProgramConfigSpec := *BpfProgramConfigSpec
			modBpfProgramConfigSpec.AttachPoint.NetworkMultiAttach.InterfaceSelector.PrimaryNodeInterface = nil
			modBpfProgramConfigSpec.AttachPoint.NetworkMultiAttach.InterfaceSelector.Interface = &NodeIface
			bpfProgramConfigSpecList = append(bpfProgramConfigSpecList, modBpfProgramConfigSpec)
			return bpfProgramConfigSpecList
		}

		panic("AttachPoint is unknown")
	}

	if BpfProgramConfigSpec.AttachPoint.SingleAttach != nil {
		bpfProgramConfigSpecList = append(bpfProgramConfigSpecList, *BpfProgramConfigSpec)
		return bpfProgramConfigSpecList
	}

	panic("Attachment Type is unknown")
}
