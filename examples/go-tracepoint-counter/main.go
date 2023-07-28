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

	bpfdHelpers "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/helpers"
	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	configMgmt "github.com/bpfd-dev/bpfd/examples/pkg/config-mgmt"
	"github.com/cilium/ebpf"
	"google.golang.org/grpc"
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

		// if the bytecode src is not a UUID provided by BPFD, we'll need to
		// load the program ourselves
		if paramData.BytecodeSrc != configMgmt.SrcUuid {
			cleanup, err := loadProgram(&paramData)
			if err != nil {
				log.Printf("Failed to load BPF program: %v", err)
				return
			}
			defer cleanup(paramData.Uuid)
		}

		mapPath = fmt.Sprintf("%s/%s/tracepoint_stats_map", DefaultMapDir, paramData.Uuid)
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

func loadProgram(paramData *configMgmt.ParameterData) (func(string), error) {
	// get the BPFD TLS credentials
	configFileData := configMgmt.LoadConfig(DefaultConfigPath)
	creds, err := configMgmt.LoadTLSCredentials(configFileData.Tls)
	if err != nil {
		return nil, err
	}

	// connect to the BPFD server
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "localhost:50051", grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	c := gobpfd.NewLoaderClient(conn)
	loadRequestCommon := &gobpfd.LoadRequestCommon{
		Location:    paramData.BytecodeSource.Location,
		SectionName: "tracepoint_kill_recorder",
		ProgramType: *bpfdHelpers.Xdp.Uint32(),
	}

	loadRequest := &gobpfd.LoadRequest{
		Common: loadRequestCommon,
		AttachInfo: &gobpfd.LoadRequest_TracepointAttachInfo{
			TracepointAttachInfo: &gobpfd.TracepointAttachInfo{
				Tracepoint: "syscalls/sys_enter_kill",
			},
		},
	}

	// send the load request to BPFD
	var res *gobpfd.LoadResponse
	res, err = c.Load(ctx, loadRequest)
	if err != nil {
		conn.Close()
		return nil, err
	}
	paramData.Uuid = res.GetId()
	log.Printf("program registered with %s id\n", paramData.Uuid)

	// provide a cleanup to unload the program
	return func(id string) {
		defer conn.Close()
		log.Printf("unloading program: %s\n", id)
		_, err = c.Unload(ctx, &gobpfd.UnloadRequest{Id: id})
		if err != nil {
			conn.Close()
			log.Printf("failed to unload program %s: %v", id, err)
			return
		}
	}, nil
}
