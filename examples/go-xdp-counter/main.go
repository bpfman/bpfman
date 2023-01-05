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

	"github.com/cilium/ebpf"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"github.com/redhat-et/bpfd/examples/pkg/config-mgmt"
	"github.com/redhat-et/bpfd/examples/pkg/bpfd-app-client"
	"google.golang.org/grpc"
)

type Stats struct {
	Packets uint64
	Bytes   uint64
}

const (
	DefaultConfigPath     = "/etc/bpfd/gocounter.toml"
	DefaultSocketPath     = "/var/lib/bpfd/sock/gocounter.sock"
	DefaultMapDir         = "/run/bpfd/fs/maps"
	DefaultByteCodeFile   = "bpf_bpfel.o"
	BpfProgramConfigName  = "go-xdp-counter-example"
	BpfProgramMapIndex    = "xdp_stats_map"
)

const (
	XDP_ACT_OK = 2
)

//go:generate bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/xdp_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Parse Input Parameters (CmdLine and Config File)
	paramData, configFileData, err := configMgmt.ParseParamData(configMgmt.ProgTypeXdp, DefaultConfigPath, DefaultByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	var mapPath string

	// If running in a Kubernetes deployment, read the map path from the Bpf Program CRD
	if paramData.CrdFlag == true {
		mapPath, err = bpfdAppClient.GetMapPathDyn(BpfProgramConfigName, BpfProgramMapIndex)
		if err != nil {
			log.Printf("error reading BpfProgram CRD: %v\n", err)
			return
		}
	} else {
		// If the bytecode src is a UUID, skip the loading and unloading of the bytecode.
		if paramData.BytecodeSrc != configMgmt.SrcUuid {
			ctx := context.Background()

			creds, err := configMgmt.LoadTLSCredentials(configFileData.Tls)
			if err != nil {
				log.Printf("Failed to generate credentials for new client: %v", err)
				return
			}

			// Set up a connection to the server.
			conn, err := grpc.DialContext(ctx, "localhost:50051", grpc.WithTransportCredentials(creds))
			if err != nil {
				log.Printf("did not connect: %v", err)
				return
			}
			c := gobpfd.NewLoaderClient(conn)

			loadRequest := &gobpfd.LoadRequest{
				Location: paramData.BytecodeLocation,
				SectionName: "stats",
				ProgramType: gobpfd.ProgramType_XDP,
				AttachType: &gobpfd.LoadRequest_NetworkMultiAttach{
					NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
						Priority: int32(paramData.Priority),
						Iface:    paramData.Iface,
					},
				},
			}

			// 1. Load Program using bpfd
			var res *gobpfd.LoadResponse
			res, err = c.Load(ctx, loadRequest)
			if err != nil {
				conn.Close()
				log.Print(err)
				return
			}
			paramData.Uuid = res.GetId()
			log.Printf("Program registered with %s id\n", paramData.Uuid)

			// 2. Set up defer to unload program when this is closed
			defer func(id string) {
				log.Printf("Unloading Program: %s\n", id)
				_, err = c.Unload(ctx, &gobpfd.UnloadRequest{Id: id})
				if err != nil {
					conn.Close()
					log.Print(err)
					return
				}
				conn.Close()
			}(paramData.Uuid)
		}

		// 3. Get access to our map
		mapPath = fmt.Sprintf("%s/%s/xdp_stats_map", DefaultMapDir, paramData.Uuid)
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
