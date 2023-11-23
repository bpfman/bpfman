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
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
)

const (
	UnusedProgramId = 0
)

const (
	SrcNone = iota
	SrcProgId
	SrcImage
	SrcFile
)

type ProgType int

const (
	ProgTypeXdp ProgType = iota
	ProgTypeTc
	ProgTypeTracepoint
)

func (s ProgType) String() string {
	return [...]string{"xdp", "tc", "tracepoint"}[s]
}

const (
	TcDirectionIngress = iota
	TcDirectionEgress
)

type ParameterData struct {
	Iface      string
	Priority   int
	Direction  int
	CrdFlag    bool
	ProgId     uint
	MapOwnerId int
	// The bytecodesource type has to be encapsulated in a complete BytecodeLocation
	// because isBytecodeLocation_Location is not Public
	BytecodeSource *gobpfman.BytecodeLocation
	BytecodeSrc    int
}

func ParseParamData(progType ProgType, configFilePath string, bytecodeFile string) (ParameterData, error) {
	var paramData ParameterData
	paramData.BytecodeSrc = SrcNone

	var cmdlineImage, cmdlineFile, direction_str, source string

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	if progType == ProgTypeXdp || progType == ProgTypeTc {
		flag.StringVar(&paramData.Iface, "iface", "",
			"Interface to load bytecode. Required.")
		flag.IntVar(&paramData.Priority, "priority", 50,
			"Priority to load program in bpfman. Optional.")
	}
	flag.UintVar(&paramData.ProgId, "id", UnusedProgramId,
		"Optional Program ID of bytecode that has already been loaded. \"id\" and\n"+
			"\"file\"/\"image\" are mutually exclusive.\n"+
			"Example: -id 28341")
	flag.StringVar(&cmdlineImage, "image", "",
		"Image repository URL of bytecode source. \"image\" and \"file\"/\"id\" are\n"+
			"mutually exclusive.\n"+
			"Example: -image quay.io/bpfman-bytecode/go-"+progType.String()+"-counter:latest")
	flag.StringVar(&cmdlineFile, "file", "",
		"File path of bytecode source. \"file\" and \"image\"/\"id\" are mutually exclusive.\n"+
			"Example: -file /home/$USER/src/bpfman/examples/go-"+progType.String()+"-counter/bpf_bpfel.o")
	flag.BoolVar(&paramData.CrdFlag, "crd", false,
		"Flag to indicate all attributes should be pulled from the BpfProgram CRD.\n"+
			"Used in Kubernetes deployments and is mutually exclusive with all other\n"+
			"parameters.")
	if progType == ProgTypeTc {
		flag.StringVar(&direction_str, "direction", "",
			"Direction to apply program (ingress, egress). Required.")
	}
	flag.IntVar(&paramData.MapOwnerId, "map_owner_id", 0,
		"Program Id of loaded eBPF program this eBPF program will share a map with.\n"+
			"Example: -map_owner_id 9785")
	flag.Parse()

	if paramData.CrdFlag {
		if flag.NFlag() != 1 {
			return paramData, fmt.Errorf("\"crd\" is mutually exclusive with all other parameters")
		} else {
			return paramData, nil
		}
	}

	// "-iface" is the interface to run bpf program on. If not provided, error.
	//    ./go-xdp-counter -iface eth0
	if (progType == ProgTypeTc || progType == ProgTypeXdp) && len(paramData.Iface) == 0 {
		return paramData, fmt.Errorf("interface is required")
	}

	if progType == ProgTypeTc {
		// "-direction" is the direction in which to run the bpf program. Valid values
		// are "ingress" and "egress". If not provided, error.
		//    ./go-tc-counter -iface eth0 -direction ingress
		if len(direction_str) == 0 {
			return paramData, fmt.Errorf("direction is required")
		}

		if direction_str == "ingress" {
			paramData.Direction = TcDirectionIngress
		} else if direction_str == "egress" {
			paramData.Direction = TcDirectionEgress
		} else {
			return paramData, fmt.Errorf("invalid direction (%s). valid options are ingress or egress", direction_str)
		}
	}

	// "-priority" is the priority to load bpf program at. If not provided,
	// defaults to 50 from the commandline.
	//    ./go-xdp-counter -iface eth0 -priority 45

	// "-id" and "-location" are mutually exclusive and "-id" takes precedence.
	// Parse Commandline first.

	// "-id" is a ProgramID for the bytecode that has already loaded into bpfman. If not
	// provided, check "-file" and "-image".
	//    ./go-xdp-counter -iface eth0 -id 23415
	if paramData.ProgId == UnusedProgramId {
		// "-path" is a file path for the bytecode source. If not provided, check toml file.
		//    ./go-xdp-counter -iface eth0 -path /var/bpfman/bytecode/bpf_bpfel.o
		if len(cmdlineFile) != 0 {
			// "-location" was entered so it is a URL
			paramData.BytecodeSource = &gobpfman.BytecodeLocation{
				Location: &gobpfman.BytecodeLocation_File{File: cmdlineFile},
			}

			paramData.BytecodeSrc = SrcFile
			source = cmdlineFile
		}
		// "-image" is a container registry url for the bytecode source. If not provided, check toml file.
		//    ./go-xdp-counter -p eth0 -image quay.io/bpfman-bytecode/go-xdp-counter:latest
		if len(cmdlineImage) != 0 {
			// "-location" was entered so it is a URL
			paramData.BytecodeSource = &gobpfman.BytecodeLocation{
				Location: &gobpfman.BytecodeLocation_Image{Image: &gobpfman.BytecodeImage{
					Url: cmdlineImage,
				}},
			}

			paramData.BytecodeSrc = SrcImage
			source = cmdlineImage
		}
	} else {
		// "-id" was entered so it is a Program ID
		paramData.BytecodeSrc = SrcProgId
	}

	// If bytecode source not entered not entered on Commandline, set to default.
	if paramData.BytecodeSrc == SrcNone {
		// Else default to local bytecode file
		path, err := filepath.Abs(bytecodeFile)
		if err == nil {
			_, err = os.Stat(path)
		}
		if err != nil {
			log.Printf("Unable to find  bytecode file: %s", bytecodeFile)
			return paramData, fmt.Errorf("couldn't find bpf elf file: %v", err)
		}

		paramData.BytecodeSource = &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: path},
		}
		paramData.BytecodeSrc = SrcFile
		source = path
	}

	if paramData.BytecodeSrc == SrcProgId {
		log.Printf("Using Input: Interface=%s Source=%d",
			paramData.Iface, paramData.ProgId)
	} else {
		log.Printf("Using Input: Interface=%s Priority=%d Source=%+v",
			paramData.Iface, paramData.Priority, source)
	}

	return paramData, nil
}

func RetrieveMapPinPath(ctx context.Context, c gobpfman.BpfmanClient, progId uint, map_name string) (string, error) {
	var mapPath string

	getRequest := &gobpfman.GetRequest{
		Id: uint32(progId),
	}
	//var getResponse *gobpfman.GetResponse
	getResponse, err := c.Get(ctx, getRequest)
	if err != nil {
		return mapPath, err
	}
	return CalcMapPinPath(getResponse.GetInfo(), map_name)
}

func CalcMapPinPath(programInfo *gobpfman.ProgramInfo, map_name string) (string, error) {
	if programInfo != nil {
		return fmt.Sprintf("%s/%s", programInfo.GetMapPinPath(), map_name), nil
	} else {
		return "", fmt.Errorf("couldn't find map path in response")
	}
}
