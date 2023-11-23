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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

	// Lookup the k8s Secret for repository authentication
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
