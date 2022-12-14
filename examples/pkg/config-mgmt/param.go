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
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
)

const (
	SrcNone = iota
	SrcUuid
	SrcLocation
)

const (
	ProgTypeXdp = iota
	ProgTypeTc
)

const (
	FILE_PREFIX = "file:///"
)

type ParameterData struct {
	Iface string
	Priority int
	CrdFlag bool
	Uuid string
	BytecodeLocation string
	BytecodeSrc int
}

func ParseParamData(progType int, configFilePath string, defaultBytecodeFile string) (ParameterData, ConfigFileData, error) {
	var paramData ParameterData
	paramData.BytecodeSrc = SrcNone

	var cmdlineUuid, cmdlinelocation string

	flag.StringVar(&paramData.Iface, "iface", "",
		"Interface to load bytecode.")
	flag.IntVar(&paramData.Priority, "priority", -1,
		"Priority to load program in bpfd")
	flag.StringVar(&cmdlineUuid, "uuid", "",
		"UUID of bytecode that has already been loaded. uuid and location are mutually exclusive.")
	flag.StringVar(&cmdlinelocation, "location", "",
		"URL of bytecode source. uuid and location are mutually exclusive.")
	flag.BoolVar(&paramData.CrdFlag, "crd", false,
		"Flag to indicate all attributes should be pulled from the EbpfProgram CRD. Used in Kubernetes deployments and is mutually exclusive with all other parameters.")
	if progType == ProgTypeTc {
		flag.BoolVar(&paramData.CrdFlag, "direction", false,
			"Direction (ingress, egress)")
	}
	flag.Parse()

	if paramData.CrdFlag == true {
		if flag.NFlag() != 1 {
			return paramData, ConfigFileData{}, fmt.Errorf("\"crd\" is mutually exclusive with all other parameters.")
		} else {
			return paramData, ConfigFileData{}, nil
		}
	}

	configFileData := loadConfig(configFilePath)

	// "-iface" is the interface to run bpf program on. If not provided, then
	// use value loaded from gocounter.toml file. If not provided, error.
	//    ./go-xdp-counter -iface eth0
	if len(paramData.Iface) == 0 {
		if configFileData.Config.Iface != "" {
			paramData.Iface = configFileData.Config.Iface
		} else {
			return paramData, configFileData, fmt.Errorf("interface is required")
		}
	}

	// "-priority" is the priority to load bpf program at. If not provided, then
	// use value loaded from gocounter.toml file. If not provided, defaults to 50.
	//    ./go-xdp-counter -iface eth0 -priority 45
	if paramData.Priority < 0 {
		if configFileData.Config.Priority != "" {
			var err error
			paramData.Priority, err = strconv.Atoi(configFileData.Config.Priority)
			if err != nil {
				return paramData, configFileData, fmt.Errorf("invalid priority in toml: %s", configFileData.Config.Priority)
			}
		} else {
			paramData.Priority = 50
		}
	}

	// "-uuid" and "-location" are mutually exclusive and "-uuid" takes precedence.
	// Parse Commandline first.

	// "-uuid" is a UUID for the bytecode that has already loaded into bpfd. If not
	// provided, check "-location".
	//    ./go-xdp-counter -iface eth0 -uuid 53ac77fc-18a9-42e2-8dd3-152fc31ba979
	if len(cmdlineUuid) == 0 {
		// "-location" is a URL for the bytecode source. If not provided, check toml file.
		//    ./go-xdp-counter -iface eth0 -location image://quay.io/bpfd/bytecode:gocounter
		//    ./go-xdp-counter -iface eth0 -location file://var/bpfd/bytecode/bpf_bpfel.o
		if len(cmdlinelocation) != 0 {
			// "-location" was entered so it is a URL
			paramData.BytecodeLocation = cmdlinelocation
			paramData.BytecodeSrc = SrcLocation
		}
	} else {
		// "-uuid" was entered so it is a UUID
		paramData.Uuid = cmdlineUuid
		paramData.BytecodeSrc = SrcUuid
	}

	// If bytecode source not entered not entered on Commandline, check toml file.
	if paramData.BytecodeSrc == SrcNone {
		if configFileData.Config.BytecodeUuid != "" {
			paramData.Uuid = configFileData.Config.BytecodeUuid
			paramData.BytecodeSrc = SrcUuid
		} else if configFileData.Config.BytecodeLocation != "" {
			paramData.BytecodeLocation = configFileData.Config.BytecodeLocation
			paramData.BytecodeSrc = SrcLocation
		} else {
			// Else default to local bytecode file
			path, err := filepath.Abs(defaultBytecodeFile)
			if err != nil {
				return paramData, configFileData, fmt.Errorf("couldn't find bpf elf file: %v", err)
			}

			paramData.BytecodeLocation = FILE_PREFIX + path
			paramData.BytecodeSrc = SrcLocation
		}
	}


	if paramData.BytecodeSrc == SrcUuid {
		log.Printf("Using Input: Interface=%s Source=%s",
			paramData.Iface, paramData.Uuid)
	} else {
		log.Printf("Using Input: Interface=%s Priority=%d Source=%s",
			paramData.Iface, paramData.Priority, paramData.BytecodeLocation)
	}

	return paramData, configFileData, nil
}
