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

	bpfdHelpers "github.com/redhat-et/bpfd/bpfd-operator/pkg/helpers"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
)

const (
	SrcNone = iota
	SrcUuid
	SrcImage
	SrcFile
)

const (
	ProgTypeXdp = iota
	ProgTypeTc
	ProgTypeTracepoint
)

type ParameterData struct {
	Iface     string
	Priority  int
	Direction bpfdHelpers.TcProgramDirection
	CrdFlag   bool
	Uuid      string
	// The bytecodesource type has to be encapsulated in a complete LoadRequest because isLoadRequest_Location is not Public
	BytecodeSource *gobpfd.LoadRequestCommon
	BytecodeSrc    int
}

func ParseParamData(progType int, configFilePath string, defaultBytecodeFile string) (ParameterData, error) {
	var paramData ParameterData
	paramData.BytecodeSrc = SrcNone

	var cmdlineUuid, cmdlineImage, cmdlineFile, direction_str string

	flag.StringVar(&paramData.Iface, "iface", "",
		"Interface to load bytecode.")
	flag.IntVar(&paramData.Priority, "priority", -1,
		"Priority to load program in bpfd")
	flag.StringVar(&cmdlineUuid, "uuid", "",
		"UUID of bytecode that has already been loaded. uuid and image/file are mutually exclusive.")
	flag.StringVar(&cmdlineImage, "image", "",
		"Image repository URL of bytecode source. uuid/file and image are mutually exclusive.")
	flag.StringVar(&cmdlineFile, "file", "",
		"File path of bytecode source. uuid/image and file are mutually exclusive.")
	flag.BoolVar(&paramData.CrdFlag, "crd", false,
		"Flag to indicate all attributes should be pulled from the EbpfProgram CRD. Used in Kubernetes deployments and is mutually exclusive with all other parameters.")
	if progType == ProgTypeTc {
		flag.StringVar(&direction_str, "direction", "",
			"Direction to apply program (ingress, egress).")
	}
	flag.Parse()

	if paramData.CrdFlag {
		if flag.NFlag() != 1 {
			return paramData, fmt.Errorf("\"crd\" is mutually exclusive with all other parameters.")
		} else {
			return paramData, nil
		}
	}

	// "-iface" is the interface to run bpf program on. If not provided, then
	// use value loaded from gocounter.toml file. If not provided, error.
	//    ./go-xdp-counter -iface eth0
	if (progType == ProgTypeTc || progType == ProgTypeXdp) && len(paramData.Iface) == 0 {
		return paramData, fmt.Errorf("interface is required")
	}

	if progType == ProgTypeTc {
		// "-direction" is the direction in which to run the bpf program. Valid values
		// are "ingress" and "egress". If not provided, then use value loaded from
		// gocounter.toml file. If not provided, error.
		// ./go-tc-counter -iface eth0 -direction ingress
		if len(direction_str) == 0 {
			return paramData, fmt.Errorf("direction is required")
		}

		if direction_str == "ingress" {
			paramData.Direction = bpfdHelpers.Ingress
		} else if direction_str == "egress" {
			paramData.Direction = bpfdHelpers.Egress
		} else {
			return paramData, fmt.Errorf("invalid direction (%s). valid options are ingress or egress.", direction_str)
		}
	}

	// "-priority" is the priority to load bpf program at. If not provided, then
	// use value loaded from gocounter.toml file. If not provided, defaults to 50.
	//    ./go-xdp-counter -iface eth0 -priority 45
	if paramData.Priority < 0 {
		paramData.Priority = 50
	}

	// "-uuid" and "-location" are mutually exclusive and "-uuid" takes precedence.
	// Parse Commandline first.

	// "-uuid" is a UUID for the bytecode that has already loaded into bpfd. If not
	// provided, check "-file" and "-image".
	//    ./go-xdp-counter -iface eth0 -uuid 53ac77fc-18a9-42e2-8dd3-152fc31ba979
	if len(cmdlineUuid) == 0 {
		// "-path" is a file path for the bytecode source. If not provided, check toml file.
		//    ./go-xdp-counter -iface eth0 -path /var/bpfd/bytecode/bpf_bpfel.o
		if len(cmdlineFile) != 0 {
			// "-location" was entered so it is a URL
			paramData.BytecodeSource = &gobpfd.LoadRequestCommon{
				Location: &gobpfd.LoadRequestCommon_File{File: cmdlineFile},
			}

			paramData.BytecodeSrc = SrcFile
		}
		// "-image" is a container registry url for the bytecode source. If not provided, check toml file.
		//    ./go-xdp-counter -p eth0 -image quay.io/bpfd-bytecode/go-xdp-counter:latest
		if len(cmdlineImage) != 0 {
			// "-location" was entered so it is a URL
			paramData.BytecodeSource = &gobpfd.LoadRequestCommon{
				Location: &gobpfd.LoadRequestCommon_Image{Image: &gobpfd.BytecodeImage{
					Url: cmdlineImage,
				}},
			}

			paramData.BytecodeSrc = SrcImage
		}
	} else {
		// "-uuid" was entered so it is a UUID
		paramData.Uuid = cmdlineUuid
		paramData.BytecodeSrc = SrcUuid
	}

	// If bytecode source not entered not entered on Commandline, set to default.
	if paramData.BytecodeSrc == SrcNone {
		// Else default to local bytecode file
		path, err := filepath.Abs(defaultBytecodeFile)
		if err != nil {
			return paramData, fmt.Errorf("couldn't find bpf elf file: %v", err)
		}

		paramData.BytecodeSource = &gobpfd.LoadRequestCommon{
			Location: &gobpfd.LoadRequestCommon_File{File: path},
		}
		paramData.BytecodeSrc = SrcFile
	}

	if paramData.BytecodeSrc == SrcUuid {
		log.Printf("Using Input: Interface=%s Source=%s",
			paramData.Iface, paramData.Uuid)
	} else {
		log.Printf("Using Input: Interface=%s Priority=%d Source=%+v",
			paramData.Iface, paramData.Priority, &paramData.BytecodeSource)
	}

	return paramData, nil
}
