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

package configMgmt

import (
	"context"
	"fmt"
	"log"
	"os"

	toml "github.com/pelletier/go-toml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

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

const (
	DefaultType    = "unix"
	DefaultPath    = "/run/bpfd/bpfd.sock"
	DefaultEnabled = true
)

func LoadConfig(configFilePath string) ConfigFileData {
	config := ConfigFileData{
		Grpc: Grpc{
			Endpoints: []Endpoint{
				{
					Type:    DefaultType,
					Path:    DefaultPath,
					Enabled: DefaultEnabled,
				},
			},
		},
	}

	file, err := os.ReadFile(configFilePath)
	if err == nil {
		err = toml.Unmarshal(file, &config)
		if err != nil {
			log.Printf("Unable to parse %s, using default configuration values.\n", configFilePath)
		} else {
			log.Printf("Using configuration values from %s\n", configFilePath)
		}
	} else {
		log.Printf("Unable to read %s, using default configuration values.\n", configFilePath)
	}

	return config
}

func CreateConnection(endpoints []Endpoint, ctx context.Context) (*grpc.ClientConn, error) {
	var (
		addr        string
		local_creds credentials.TransportCredentials
	)

	for _, e := range endpoints {
		if !e.Enabled {
			continue
		}

		if e.Type == "unix" {
			addr = fmt.Sprintf("unix://%s", e.Path)
			local_creds = insecure.NewCredentials()
		}

		conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(local_creds))
		if err == nil {
			return conn, nil
		}
		log.Printf("did not connect: %v", err)
	}

	return nil, fmt.Errorf("unable to stablish connection")
}
