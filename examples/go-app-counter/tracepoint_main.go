//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	bpfmanHelpers "github.com/bpfman/bpfman-operator/pkg/helpers"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	configMgmt "github.com/bpfman/bpfman/examples/pkg/config-mgmt"
	"github.com/cilium/ebpf"
)

const (
	TPBpfProgramMapIndex = "tracepoint_stats_map"
)

type TPStats struct {
	Calls uint64
}

func processTracepoint(cancelCtx context.Context, paramData *configMgmt.ParameterData) {
	// determine the path to the tracepoint_stats_map, whether provided via CRD
	// or BPFMAN or otherwise.
	var mapPath string
	// If running in a Kubernetes deployment, the eBPF program is already loaded.
	// Only need the map path, which is at a known location in the pod using VolumeMounts
	// and the CSI Driver.
	if paramData.CrdFlag {
		// 3. Get access to our map
		mapPath = fmt.Sprintf("%s/%s", ApplicationMapsMountPoint, TPBpfProgramMapIndex)
	} else { // if not on k8s, find the map path from the system
		// If the bytecode src is a Program ID, skip the loading and unloading of the bytecode.
		if paramData.BytecodeSrc != configMgmt.SrcProgId {
			var loadRequest *gobpfman.LoadRequest
			if paramData.MapOwnerId != 0 {
				mapOwnerId := uint32(paramData.MapOwnerId)
				loadRequest = &gobpfman.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "tracepoint_kill_recorder",
					ProgramType: *bpfmanHelpers.Tracepoint.Uint32(),
					Attach: &gobpfman.AttachInfo{
						Info: &gobpfman.AttachInfo_TracepointAttachInfo{
							TracepointAttachInfo: &gobpfman.TracepointAttachInfo{
								Tracepoint: "syscalls/sys_enter_kill",
							},
						},
					},
					MapOwnerId: &mapOwnerId,
				}
			} else {
				loadRequest = &gobpfman.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "tracepoint_kill_recorder",
					ProgramType: *bpfmanHelpers.Tracepoint.Uint32(),
					Attach: &gobpfman.AttachInfo{
						Info: &gobpfman.AttachInfo_TracepointAttachInfo{
							TracepointAttachInfo: &gobpfman.TracepointAttachInfo{
								Tracepoint: "syscalls/sys_enter_kill",
							},
						},
					},
				}
			}

			// 1. Load Program using bpfman
			var res *gobpfman.LoadResponse
			var err error
			res, err = loadBpfProgram(loadRequest)
			if err != nil {
				log.Print(err)
				return
			}

			kernelInfo := res.GetKernelInfo()
			if kernelInfo != nil {
				paramData.ProgId = uint(kernelInfo.GetId())
			} else {
				log.Printf("kernelInfo not returned in LoadResponse")
				return
			}
			log.Printf("Program registered with id %d\n", paramData.ProgId)

			// 2. Set up defer to unload program when this is closed
			defer func(id uint) {
				log.Printf("unloading program: %d\n", id)
				_, err = unloadBpfProgram(id)
				if err != nil {
					log.Print(err)
					return
				}
			}(paramData.ProgId)

			// 3. Get access to our map
			mapPath, err = configMgmt.CalcMapPinPath(res.GetInfo(), "tracepoint_stats_map")
			if err != nil {
				log.Print(err)
				return
			}
		} else {
			// 3. Get access to our map
			var err error
			mapPath, err = getMapPinPath(paramData.ProgId, "tracepoint_stats_map")
			if err != nil {
				log.Print(err)
				return
			}
		}
	}

	// load the pinned stats map which is keeping count of kill -SIGUSR1 calls
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

	// send a SIGUSR1 signal to this program on repeat, which the BPF program
	// will report on to the stats map.
	go func() {
		for {
			err := syscall.Kill(os.Getpid(), syscall.SIGUSR1)
			if err != nil {
				log.Print("Failed to kill process with SIGUSR1:")
				log.Print(err)
				return
			}
			time.Sleep(time.Second * 1)
		}
	}()

	// retrieve and report on the number of kill -SIGUSR1 calls
	index := uint32(0)
	ticker := time.NewTicker(2 * time.Second)
	for range ticker.C {
		select {
		case <-cancelCtx.Done():
			log.Printf("Exiting Tracepoint...\n")
			return
		default:
			var stats []TPStats
			var totalCalls uint64

			if err := statsMap.Lookup(&index, &stats); err != nil {
				log.Printf("map lookup failed: %v", err)
				return
			}

			for _, stat := range stats {
				totalCalls += stat.Calls
			}

			log.Printf("Tracepoint: SIGUSR1 signal count: %d\n", totalCalls)
		}
	}
}
