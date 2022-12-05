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
	"io/ioutil"
	"log"
	"os"

	toml "github.com/pelletier/go-toml"
	bpfdiov1alpha1 "github.com/redhat-et/bpfd/api/v1alpha1"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"google.golang.org/grpc/credentials"
)

const (
	DefaultConfigPath     = " /etc/bpfd/bpfd.toml"
	DefaultRootCaPath     = "/etc/bpfd/certs/ca/ca.crt"
	DefaultClientCertPath = "/etc/bpfd/certs/bpfd-client/tls.crt"
	DefaultClientKeyPath  = "/etc/bpfd/certs/bpfd-client/tls.key"
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
	file, err := ioutil.ReadFile(DefaultConfigPath)
	if err == nil {
		err = toml.Unmarshal(file, &tlsConfig)
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

func BuildBpfdLoadRequest(ebpf_program_config *bpfdiov1alpha1.EbpfProgramConfig) (*gobpfd.LoadRequest, error) {
	loadRequest := gobpfd.LoadRequest{}

	loadRequest.SectionName = ebpf_program_config.Spec.Name

	// Parse if bytecode source is an image or local
	// TODO since we know only one field here will be non-nil we can probably
	// optimize at some point
	if ebpf_program_config.Spec.ByteCode.ImageUrl != nil {
		loadRequest.FromImage = true
		loadRequest.Path = *ebpf_program_config.Spec.ByteCode.ImageUrl
	} else {
		loadRequest.FromImage = false
		loadRequest.Path = *ebpf_program_config.Spec.ByteCode.Path
	}

	// Map program type (ultimately we should make this an ENUM in the API)
	switch ebpf_program_config.Spec.Type {
	case "XDP":
		loadRequest.ProgramType = gobpfd.ProgramType_XDP
	case "TC":
		loadRequest.ProgramType = gobpfd.ProgramType_TC
	default:
		// Add a condition and exit don't requeue, an ensuing update to ebpfProgramConfig
		// should fix this
		return nil, fmt.Errorf("invalid Program Type")
	}

	if ebpf_program_config.Spec.AttachPoint.Interface != nil {
		loadRequest.AttachType = &gobpfd.LoadRequest_NetworkMultiAttach{
			NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
				Priority: int32(ebpf_program_config.Spec.Priority),
				Iface:    *ebpf_program_config.Spec.AttachPoint.Interface,
			},
		}
	} else {
		// Add a condition and exit don't requeue, an ensuing update to ebpfProgramConfig
		// should fix this
		return nil, fmt.Errorf("invalid Attach Type")
	}

	return &loadRequest, nil
}

func BuildBpfdUnloadRequest(uuid string) (*gobpfd.UnloadRequest, error) {
	return &gobpfd.UnloadRequest{
		Id: uuid,
	}, nil
}
