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

	"github.com/bpfman/bpfman/bpfman-operator/internal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

//var log = ctrl.Log.WithName("bpfman-conn")

func CreateConnection(ctx context.Context, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	addr := fmt.Sprintf("unix://%s", internal.DefaultPath)
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("unable to establish connection to %s: %w", addr, err)
	}

	return conn, nil
}
