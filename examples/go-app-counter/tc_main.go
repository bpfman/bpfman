//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	bpfmanHelpers "github.com/bpfman/bpfman-operator/pkg/helpers"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	configMgmt "github.com/bpfman/bpfman/examples/pkg/config-mgmt"
	"github.com/cilium/ebpf"
)

type TcStats struct {
	Packets uint64
	Bytes   uint64
}

const (
	TCBpfProgramMapIndex = "tc_stats_map"
)

const (
	TC_ACT_OK = 0
)

func processTc(cancelCtx context.Context, paramData *configMgmt.ParameterData) {
	var action string
	var direction bpfmanHelpers.TcProgramDirection
	if paramData.Direction == configMgmt.TcDirectionIngress {
		action = "received"
		direction = bpfmanHelpers.Ingress
	} else {
		action = "sent"
		direction = bpfmanHelpers.Egress
	}

	var mapPath string

	// If running in a Kubernetes deployment, the eBPF program is already loaded.
	// Only need the map path, which is at a known location in the pod using VolumeMounts
	// and the CSI Driver.
	if paramData.CrdFlag {
		// 3. Get access to our map
		mapPath = fmt.Sprintf("%s/%s", ApplicationMapsMountPoint, TCBpfProgramMapIndex)
	} else {
		// Set up a connection to the server.
		// If the bytecode src is a Program ID, skip the loading and unloading of the bytecode.
		if paramData.BytecodeSrc != configMgmt.SrcProgId {
			loadRequest := &gobpfman.LoadRequest{
				Bytecode: paramData.BytecodeSource,
				Info: []*gobpfman.LoadInfo{
					{
						Name:        "stats",
						ProgramType: gobpfman.BpfmanProgramType_TC,
					},
				},
			}
			if paramData.MapOwnerId != 0 {
				mapOwnerId := uint32(paramData.MapOwnerId)
				loadRequest.MapOwnerId = &mapOwnerId
			}

			// 1. Load Program using bpfman
			var loadRes *gobpfman.LoadResponse
			var err error
			loadRes, err = loadBpfProgram(loadRequest)
			if err != nil {
				log.Print(err)
				return
			}

			if len(loadRes.Programs) != 1 {
				log.Printf("Expected 1 program, got %d\n", len(loadRes.Programs))
				return
			}

			prog := loadRes.Programs[0]

			kernelInfo := prog.GetKernelInfo()
			if kernelInfo != nil {
				paramData.ProgId = uint(kernelInfo.GetId())
			} else {
				log.Printf("kernelInfo not returned in LoadResponse")
				return
			}
			log.Printf("Program registered with id %d\n", paramData.ProgId)

			// 2. Attach the program
			attachRequest := &gobpfman.AttachRequest{
				Id: uint32(paramData.ProgId),
				Attach: &gobpfman.AttachInfo{
					Info: &gobpfman.AttachInfo_TcAttachInfo{
						TcAttachInfo: &gobpfman.TCAttachInfo{
							Priority:  int32(paramData.Priority),
							Iface:     paramData.Iface,
							Direction: direction.String(),
						},
					},
				},
			}

			var attachRes *gobpfman.AttachResponse
			attachRes, err = attachBpfProgram(attachRequest)
			if err != nil {
				log.Print(err)
				return
			}

			paramData.LinkId = uint(attachRes.LinkId)

			// 3. Set up defer to unload program when this is closed
			defer func(id uint) {
				log.Printf("unloading program: %d\n", id)
				_, err = unloadBpfProgram(id)
				if err != nil {
					log.Print(err)
					return
				}
			}(paramData.ProgId)

			// 4. Get access to our map
			mapPath, err = configMgmt.CalcMapPinPath(prog.GetInfo(), "tc_stats_map")
			if err != nil {
				log.Print(err)
				return
			}
		} else {
			// 4. Get access to our map
			var err error
			mapPath, err = getMapPinPath(paramData.ProgId, "tc_stats_map")
			if err != nil {
				log.Print(err)
				return
			}
		}
	}

	opts := &ebpf.LoadPinOptions{
		ReadOnly:  false,
		WriteOnly: false,
		Flags:     0,
	}
	statsMap, err := ebpf.LoadPinnedMap(mapPath, opts)
	if err != nil {
		log.Printf("Failed to load pinned Map: %s\n", mapPath)
		log.Print(err)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	for range ticker.C {
		select {
		case <-cancelCtx.Done():
			log.Printf("Exiting TC ...\n")
			return
		default:
			key := uint32(TC_ACT_OK)
			var stats []TcStats
			var totalPackets uint64
			var totalBytes uint64

			err := statsMap.Lookup(&key, &stats)
			if err != nil {
				log.Print(err)
				return
			}

			for _, cpuStat := range stats {
				totalPackets += cpuStat.Packets
				totalBytes += cpuStat.Bytes
			}

			log.Printf("TC: %s %d packets / %d bytes\n", action, totalPackets, totalBytes)
		}
	}
}
