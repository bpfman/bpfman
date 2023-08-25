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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"

	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	toml "github.com/pelletier/go-toml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("tls-intern")

type Tls struct {
	CaCert     string `toml:"ca_cert"`
	Cert       string `toml:"cert"`
	Key        string `toml:"key"`
	ClientCert string `toml:"client_cert"`
	ClientKey  string `toml:"client_key"`
}

type Endpoint struct {
	Type    string `toml:"type"`
	Path    string `toml:"path"`
	Port    uint16 `toml:"port"`
	Enabled bool   `toml:"enabled"`
}

type Grpc struct {
	Endpoints []Endpoint `toml:"endpoints"`
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
			Endpoints: []Endpoint{
				{
					Type:    internal.DefaultType,
					Port:    internal.DefaultPort,
					Enabled: internal.DefaultEnabled,
				},
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

func CreateConnection(endpoints []Endpoint, ctx context.Context, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	var (
		addr        string
		local_creds credentials.TransportCredentials
	)

	for _, e := range endpoints {
		if !e.Enabled {
			continue
		}

		if e.Type == "tcp" {
			addr = fmt.Sprintf("localhost:%d", e.Port)
			local_creds = creds
		} else if e.Type == "unix" {
			addr = fmt.Sprintf("unix://%s", e.Path)
			local_creds = insecure.NewCredentials()
		}

		conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(local_creds), grpc.WithBlock())
		if err == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("unable to stablish connection")
}
