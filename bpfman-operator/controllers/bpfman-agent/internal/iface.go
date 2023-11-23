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
	"fmt"
	"net"

	v1 "k8s.io/api/core/v1"
)

func GetPrimaryNodeInterface(ourNode *v1.Node) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to read node interfaces: %v", err)
	}

	for _, ipaddr := range ourNode.Status.Addresses {
		log.V(2).Info("Node IP  - ", "Type", ipaddr.Type, "Address", ipaddr.Address)
		if ipaddr.Type == v1.NodeInternalIP {
			for _, i := range ifaces {
				addrs, err := i.Addrs()
				if err != nil {
					log.Error(err, "failed to parse localAddresses, continuing")
					continue
				}
				for _, a := range addrs {
					switch v := a.(type) {
					case *net.IPAddr:
						log.V(2).Info("localAddresses", "name", i.Name, "index", i.Index, "addr", v, "mask", v.IP.DefaultMask())
						if ipaddr.Address == v.String() {
							log.V(1).Info("primary node interface set", "name", i.Name)
							return i.Name, nil
						}

					case *net.IPNet:
						log.V(2).Info(" localAddresses", "name", i.Name, "index", i.Index, "v", v, "addr", v.IP, "mask", v.Mask)
						if v.IP.Equal(net.ParseIP(ipaddr.Address)) {
							log.V(1).Info("primary node interface set", "name", i.Name)
							return i.Name, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("unable to find Node Interface")
}
