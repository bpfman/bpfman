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

package conn

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/bpfman/bpfman/bpfman-operator/internal"
	toml "github.com/pelletier/go-toml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("bpfman-conn")

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
	Grpc Grpc `toml:"grpc"`
}

func LoadConfig() ConfigFileData {
	config := ConfigFileData{}

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

func CreateConnection(endpoints []Endpoint, ctx context.Context, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	var (
		addr string
	)

	// TODO(astoycos) this currently connects to the first valid endpoint
	// rather then spawning multiple connections. This should be cleaned up
	// and made explicitly configurable.
	for _, e := range endpoints {
		if !e.Enabled {
			continue
		}

		if e.Type == "tcp" {
			addr = fmt.Sprintf("localhost:%d", e.Port)
		} else if e.Type == "unix" {
			addr = fmt.Sprintf("unix://%s", e.Path)
		}

		conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds), grpc.WithBlock())
		if err != nil {
			return nil, fmt.Errorf("unable to establish connection to %s: %w", addr, err)
		}

		return conn, nil
	}

	return nil, fmt.Errorf("unable to establish connection, no valid endpoints")
}
