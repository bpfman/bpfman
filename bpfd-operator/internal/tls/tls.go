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

package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"

	toml "github.com/pelletier/go-toml"
	"github.com/redhat-et/bpfd/bpfd-operator/internal"
	"google.golang.org/grpc/credentials"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("tls-internal")

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
			CaCert:     internal.DefaultRootCaPath,
			Cert:       internal.DefaultCertPath,
			Key:        internal.DefaultKeyPath,
			ClientCert: internal.DefaultClientCertPath,
			ClientKey:  internal.DefaultClientKeyPath,
		},
		Grpc: Grpc{
			Endpoint: Endpoint{
				Port: internal.DefaultPort,
			},
		},
	}

	log.Info("Reading...\n", "Default config path", internal.DefaultConfigPath)
	file, err := os.Open(internal.DefaultConfigPath)
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
		log.Info("Read config-path failed: err\n", "config-path", internal.DefaultConfigPath, "err", err)
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
