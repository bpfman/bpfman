//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	bpfmanHelpers "github.com/bpfman/bpfman/bpfman-operator/pkg/helpers"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	configMgmt "github.com/bpfman/bpfman/examples/pkg/config-mgmt"
	"github.com/cilium/ebpf"
)

const (
	KprobeBpfProgramMapIndex = "kprobe_stats_map"

	// KprobeMapsMountPoint is the "go-kprobe-counter-maps" volumeMount "mountPath" from "deployment.yaml"
	KprobeMapsMountPoint = "/run/kprobe/maps"
)

type KprobeStats struct {
	Counter uint64
}

func processKprobe(stop chan os.Signal) {

	initMutex.Lock()

	// pull the BPFMAN config management data to determine if we're running on a
	// system with BPFMAN available.
	paramData, err := configMgmt.ParseParamData(configMgmt.ProgTypeKprobe, DefaultByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	// determine the path to the kprobe_stats_map, whether provided via CRD
	// or BPFMAN or otherwise.
	var mapPath string
	// If running in a Kubernetes deployment, the eBPF program is already loaded.
	// Only need the map path, which is at a known location in the pod using VolumeMounts
	// and the CSI Driver.
	if paramData.CrdFlag {
		// 3. Get access to our map
		mapPath = fmt.Sprintf("%s/%s", KprobeMapsMountPoint, KprobeBpfProgramMapIndex)
	} else { // if not on k8s, find the map path from the system
		ctx := context.Background()

		// connect to the BPFMAN server
		conn, err := configMgmt.CreateConnection(ctx)
		if err != nil {
			log.Printf("failed to create client connection: %v", err)
			return
		}

		c := gobpfman.NewBpfmanClient(conn)

		// If the bytecode src is a Program ID, skip the loading and unloading of the bytecode.
		if paramData.BytecodeSrc != configMgmt.SrcProgId {
			var loadRequest *gobpfman.LoadRequest
			if paramData.MapOwnerId != 0 {
				mapOwnerId := uint32(paramData.MapOwnerId)
				loadRequest = &gobpfman.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "kprobe_counter",
					ProgramType: *bpfmanHelpers.Kprobe.Uint32(),
					Attach: &gobpfman.AttachInfo{
						Info: &gobpfman.AttachInfo_KprobeAttachInfo{
							KprobeAttachInfo: &gobpfman.KprobeAttachInfo{
								FnName: "try_to_wake_up",
							},
						},
					},
					MapOwnerId: &mapOwnerId,
				}
			} else {
				loadRequest = &gobpfman.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "kprobe_counter",
					ProgramType: *bpfmanHelpers.Kprobe.Uint32(),
					Attach: &gobpfman.AttachInfo{
						Info: &gobpfman.AttachInfo_KprobeAttachInfo{
							KprobeAttachInfo: &gobpfman.KprobeAttachInfo{
								FnName: "try_to_wake_up",
							},
						},
					},
				}
			}

			// 1. Load Program using bpfman
			var res *gobpfman.LoadResponse
			res, err = c.Load(ctx, loadRequest)
			if err != nil {
				conn.Close()
				log.Print(err)
				return
			}

			kernelInfo := res.GetKernelInfo()
			if kernelInfo != nil {
				paramData.ProgId = uint(kernelInfo.GetId())
			} else {
				conn.Close()
				log.Printf("kernelInfo not returned in LoadResponse")
				return
			}
			log.Printf("Program registered with id %d\n", paramData.ProgId)

			// 2. Set up defer to unload program when this is closed
			defer func(id uint) {
				log.Printf("unloading program: %d\n", id)
				_, err = c.Unload(ctx, &gobpfman.UnloadRequest{Id: uint32(id)})
				if err != nil {
					conn.Close()
					log.Print(err)
					return
				}
				conn.Close()
			}(paramData.ProgId)

			// 3. Get access to our map
			mapPath, err = configMgmt.CalcMapPinPath(res.GetInfo(), "kprobe_stats_map")
			if err != nil {
				log.Print(err)
				return
			}
		} else {
			// 2. Set up defer to close connection
			defer func(id uint) {
				log.Printf("Closing Connection for Program: %d\n", id)
				conn.Close()
			}(paramData.ProgId)

			// 3. Get access to our map
			mapPath, err = configMgmt.RetrieveMapPinPath(ctx, c, paramData.ProgId, "kprobe_stats_map")
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

	initMutex.Unlock()

	// retrieve and report on the number of times the kprobe is executed.
	index := uint32(0)
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			var stats []KprobeStats
			var totalCount uint64

			if err := statsMap.Lookup(&index, &stats); err != nil {
				log.Printf("map lookup failed: %v", err)
				return
			}

			for _, stat := range stats {
				totalCount += stat.Counter
			}

			log.Printf("Kprobe: count: %d\n", totalCount)
		}
	}()

	<-stop

	log.Printf("Exiting Kprobe...\n")
}
