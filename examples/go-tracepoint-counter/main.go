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

const (
	TracepointProgramName = "go-tracepoint-counter-example"
	BpfProgramMapIndex    = "tracepoint_stats_map"
	PrimaryByteCodeFile   = "/run/bpfd/examples/go-tracepoint-counter/bpf_bpfel.o"
	SecondaryByteCodeFile = "bpf_bpfel.o"
	DefaultConfigPath     = "/etc/bpfd/bpfd.toml"
	DefaultMapDir         = "/run/bpfd/fs/maps"
)

type Stats struct {
	Calls uint64
}

//go:generate bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/tracepoint_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// pull the BPFD config management data to determine if we're running on a
	// system with BPFD available.
	paramData, err := configMgmt.ParseParamData(configMgmt.ProgTypeTracepoint, DefaultConfigPath, PrimaryByteCodeFile, SecondaryByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	// determine the path to the tracepoint_stats_map, whether provided via CRD
	// or BPFD or otherwise.
	var mapPath string
	if paramData.CrdFlag { // get the map path from the API resource if on k8s
		c := bpfdHelpers.GetClientOrDie()

		maps, err := bpfdHelpers.GetMaps(c, TracepointProgramName, []string{BpfProgramMapIndex})
		if err != nil {
			log.Printf("error getting bpf stats map: %v\n", err)
			return
		}

		mapPath = maps[BpfProgramMapIndex]

	} else { // if not on k8s, find the map path from the system
		ctx := context.Background()

		// get the BPFD TLS credentials
		configFileData := configMgmt.LoadConfig(DefaultConfigPath)
		creds, err := configMgmt.LoadTLSCredentials(configFileData.Tls)
		if err != nil {
			log.Printf("Failed to generate credentials for new client: %v", err)
		}

		// connect to the BPFD server
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
					Name:        "tracepoint_kill_recorder",
					ProgramType: *bpfdHelpers.Tracepoint.Uint32(),
					Attach: &gobpfd.AttachInfo{
						Info: &gobpfd.AttachInfo_TracepointAttachInfo{
							TracepointAttachInfo: &gobpfd.TracepointAttachInfo{
								Tracepoint: "syscalls/sys_enter_kill",
							},
						},
					},
					MapOwnerId: &mapOwnerId,
				}
			} else {
				loadRequest = &gobpfd.LoadRequest{
					Bytecode:    paramData.BytecodeSource,
					Name:        "tracepoint_kill_recorder",
					ProgramType: *bpfdHelpers.Tracepoint.Uint32(),
					Attach: &gobpfd.AttachInfo{
						Info: &gobpfd.AttachInfo_TracepointAttachInfo{
							TracepointAttachInfo: &gobpfd.TracepointAttachInfo{
								Tracepoint: "syscalls/sys_enter_kill",
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
				log.Printf("unloading program: %d\n", id)
				_, err = c.Unload(ctx, &gobpfd.UnloadRequest{Id: uint32(id)})
				if err != nil {
					conn.Close()
					log.Print(err)
					return
				}
				conn.Close()
			}(paramData.ProgId)

			// 3. Get access to our map
			mapPath, err = configMgmt.CalcMapPinPath(res.GetInfo(), "tracepoint_stats_map")
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
