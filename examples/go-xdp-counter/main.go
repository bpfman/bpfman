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

type Stats struct {
	Packets uint64
	Bytes   uint64
}

const (
	DefaultByteCodeFile = "bpf_x86_bpfel.o"
	XdpProgramName      = "go-xdp-counter-example"
	BpfProgramMapIndex  = "xdp_stats_map"

	// MapsMountPoint is the "go-xdp-counter-maps" volumeMount "mountPath" from "deployment.yaml"
	MapsMountPoint = "/run/xdp/maps"
)

const (
	XDP_ACT_OK = 2
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" -target amd64,arm64,ppc64le,s390x bpf ./bpf/xdp_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Parse Input Parameters (CmdLine and Config File)
	paramData, err := configMgmt.ParseParamData(configMgmt.ProgTypeXdp, DefaultByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	var mapPath string

	// If running in a Kubernetes deployment, the eBPF program is already loaded.
	// Only need the map path, which is at a known location in the pod using VolumeMounts
	// and the CSI Driver.
	if paramData.CrdFlag {
		// 3. Get access to our map
		mapPath = fmt.Sprintf("%s/%s", MapsMountPoint, BpfProgramMapIndex)
	} else {
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
						Name:        "xdp_stats",
						ProgramType: gobpfman.BpfmanProgramType_XDP,
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
					Info: &gobpfman.AttachInfo_XdpAttachInfo{
						XdpAttachInfo: &gobpfman.XDPAttachInfo{
							Priority: int32(paramData.Priority),
							Iface:    paramData.Iface,
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
			mapPath, err = configMgmt.CalcMapPinPath(prog.GetInfo(), "xdp_stats_map")
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
			mapPath, err = configMgmt.RetrieveMapPinPath(ctx, c, paramData.ProgId, "xdp_stats_map")
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

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			key := uint32(XDP_ACT_OK)
			var stats []Stats
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

			log.Printf("%d packets received\n", totalPackets)
			log.Printf("%d bytes received\n\n", totalBytes)
		}
	}()

	<-stop

	log.Printf("Exiting...\n")
}
