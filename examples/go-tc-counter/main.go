//go:build linux
// +build linux

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	bpfdHelpers "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/helpers"
	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	configMgmt "github.com/bpfd-dev/bpfd/examples/pkg/config-mgmt"
	"github.com/cilium/ebpf"
)

type Stats struct {
	Packets uint64
	Bytes   uint64
}

const (
	DefaultConfigPath   = "/etc/bpfd/bpfd.toml"
	DefaultByteCodeFile = "bpf_bpfel.o"
	TcProgramName       = "go-tc-counter-example"
	BpfProgramMapIndex  = "tc_stats_map"
)

const (
	TC_ACT_OK = 0
)

//go:generate bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/tc_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Parse Input Parameters (CmdLine and Config File)
	paramData, err := configMgmt.ParseParamData(configMgmt.ProgTypeTc, DefaultConfigPath, DefaultByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	var action string
	var direction bpfdHelpers.TcProgramDirection
	if paramData.Direction == configMgmt.TcDirectionIngress {
		action = "received"
		direction = bpfdHelpers.Ingress
	} else {
		action = "sent"
		direction = bpfdHelpers.Egress
	}

	var mapPath string

	// If running in a Kubernetes deployment, read the map path from the Bpf Program CRD
	if paramData.CrdFlag {
		c := bpfdHelpers.GetClientOrDie()

		maps, err := bpfdHelpers.GetMaps(c, TcProgramName, []string{BpfProgramMapIndex})
		if err != nil {
			log.Printf("error getting bpf stats map: %v\n", err)
			return
		}

		mapPath = maps[BpfProgramMapIndex]

	} else {
		ctx := context.Background()

		configFileData := configMgmt.LoadConfig(DefaultConfigPath)
		creds, err := configMgmt.LoadTLSCredentials(configFileData.Tls)
		if err != nil {
			log.Printf("Failed to generate credentials for new client: %v", err)
			return
		}

		// Set up a connection to the server.
		conn, err := configMgmt.CreateConnection(configFileData.Grpc.Endpoints, ctx, creds)
		if err != nil {
			log.Printf("failed to create client connection: %v", err)
			return
		}
		c := gobpfd.NewBpfdClient(conn)

		// If the bytecode src is a Program ID, skip the loading and unloading of the bytecode.
		if paramData.BytecodeSrc != configMgmt.SrcProgId {
			var loadRequest *gobpfd.LoadRequest
			if paramData.MapOwnerId != 0 {
				mapOwnerId := uint32(paramData.MapOwnerId)
				loadRequest = &gobpfd.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "stats",
					ProgramType: *bpfdHelpers.Tc.Uint32(),
					Attach: &gobpfd.AttachInfo{
						Info: &gobpfd.AttachInfo_TcAttachInfo{
							TcAttachInfo: &gobpfd.TCAttachInfo{
								Priority:  int32(paramData.Priority),
								Iface:     paramData.Iface,
								Direction: direction.String(),
							},
						},
					},
					MapOwnerId: &mapOwnerId,
				}
			} else {
				loadRequest = &gobpfd.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "stats",
					ProgramType: *bpfdHelpers.Xdp.Uint32(),
					Attach: &gobpfd.AttachInfo{
						Info: &gobpfd.AttachInfo_TcAttachInfo{
							TcAttachInfo: &gobpfd.TCAttachInfo{
								Priority:  int32(paramData.Priority),
								Iface:     paramData.Iface,
								Direction: direction.String(),
							},
						},
					},
				}
			}

			// 1. Load Program using bpfd
			var res *gobpfd.LoadResponse
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
				log.Printf("Unloading Program: %d\n", id)
				_, err = c.Unload(ctx, &gobpfd.UnloadRequest{Id: uint32(id)})
				if err != nil {
					conn.Close()
					log.Print(err)
					return
				}
				conn.Close()
			}(paramData.ProgId)

			// 3. Get access to our map
			mapPath, err = configMgmt.CalcMapPinPath(res.GetInfo(), "tc_stats_map")
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
			mapPath, err = configMgmt.RetrieveMapPinPath(ctx, c, paramData.ProgId, "tc_stats_map")
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
			key := uint32(TC_ACT_OK)
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

			log.Printf("%d packets %s\n", totalPackets, action)
			log.Printf("%d bytes %s\n\n", totalBytes, action)
		}
	}()

	<-stop

	log.Printf("Exiting...\n")
}
