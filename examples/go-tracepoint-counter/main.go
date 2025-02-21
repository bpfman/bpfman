//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	configMgmt "github.com/bpfman/bpfman/examples/pkg/config-mgmt"
	"github.com/cilium/ebpf"
)

const (
	TracepointProgramName = "go-tracepoint-counter-example"
	BpfProgramMapIndex    = "tracepoint_stats_map"
	DefaultByteCodeFile   = "bpf_x86_bpfel.o"

	// MapsMountPoint is the "go-tracepoint-counter-maps" volumeMount "mountPath" from "deployment.yaml"
	MapsMountPoint = "/run/tracepoint/maps"
)

type Stats struct {
	Calls uint64
}

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" -target amd64,arm64,ppc64le,s390x bpf ./bpf/tracepoint_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// pull the BPFMAN config management data to determine if we're running on a
	// system with BPFMAN available.
	paramData, err := configMgmt.ParseParamData(configMgmt.ProgTypeTracepoint, DefaultByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	// determine the path to the tracepoint_stats_map, whether provided via CRD
	// or BPFMAN or otherwise.
	var mapPath string
	// If running in a Kubernetes deployment, the eBPF program is already loaded.
	// Only need the map path, which is at a known location in the pod using VolumeMounts
	// and the CSI Driver.
	if paramData.CrdFlag {
		// 3. Get access to our map
		mapPath = fmt.Sprintf("%s/%s", MapsMountPoint, BpfProgramMapIndex)
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
			loadRequest := &gobpfman.LoadRequest{
				Bytecode: paramData.BytecodeSource,
				Info: []*gobpfman.LoadInfo{
					{
						Name:        "tracepoint_kill_recorder",
						ProgramType: gobpfman.BpfmanProgramType_TRACEPOINT,
					},
				},
			}
			if paramData.MapOwnerId != 0 {
				mapOwnerId := uint32(paramData.MapOwnerId)
				loadRequest.MapOwnerId = &mapOwnerId
			}

			// 1. Load Program using bpfman
			var loadRes *gobpfman.LoadResponse
			loadRes, err = c.Load(ctx, loadRequest)
			if err != nil {
				conn.Close()
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
					Info: &gobpfman.AttachInfo_TracepointAttachInfo{
						TracepointAttachInfo: &gobpfman.TracepointAttachInfo{
							Tracepoint: "syscalls/sys_enter_kill",
						},
					},
				},
			}

			var attachRes *gobpfman.AttachResponse
			attachRes, err = c.Attach(ctx, attachRequest)
			if err != nil {
				log.Print(err)
				return
			}

			paramData.LinkId = uint(attachRes.LinkId)
			log.Printf("Program attached with link id %d\n", paramData.LinkId)

			// 3. Set up defer to unload program when this is closed
			defer func(id uint) {
				log.Printf("Closing Connection for Program: %d\n", id)
				_, err = c.Unload(ctx, &gobpfman.UnloadRequest{Id: uint32(id)})
				if err != nil {
					conn.Close()
					log.Print(err)
					return
				}
				conn.Close()
			}(paramData.ProgId)

			// 4. Get access to our map
			mapPath, err = configMgmt.CalcMapPinPath(prog.GetInfo(), "kprobe_stats_map")
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
			mapPath, err = configMgmt.RetrieveMapPinPath(ctx, c, paramData.ProgId, "tracepoint_stats_map")
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
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			var stats []Stats
			var totalCalls uint64

			if err := statsMap.Lookup(&index, &stats); err != nil {
				log.Printf("map lookup failed: %v", err)
				return
			}

			for _, stat := range stats {
				totalCalls += stat.Calls
			}

			log.Printf("SIGUSR1 signal count: %d\n", totalCalls)
		}
	}()

	<-stop

	log.Printf("Exiting...\n")
}
