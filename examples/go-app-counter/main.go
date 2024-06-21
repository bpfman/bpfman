//go:build linux
// +build linux

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	configMgmt "github.com/bpfman/bpfman/examples/pkg/config-mgmt"
	"google.golang.org/grpc"
)

const (
	DefaultByteCodeFile       = "bpf_bpfel.o"
	ApplicationMapsMountPoint = "/run/application/maps"
)

var appMutex = &sync.Mutex{}
var c gobpfman.BpfmanClient
var ctx context.Context

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/app_counter.c -- -I.:/usr/include/bpf:/usr/include/linux

func main() {
	var conn *grpc.ClientConn

	// pull the BPFMAN config management data to determine if we're running on a
	// system with BPFMAN available.
	paramData, err := configMgmt.ParseParamData(configMgmt.ProgTypeApplication, DefaultByteCodeFile)
	if err != nil {
		log.Printf("error processing parameters: %v\n", err)
		return
	}

	// If not running on Kubernetes, create connection to bpfman
	if !paramData.CrdFlag {
		ctx = context.Background()

		conn, err = configMgmt.CreateConnection(ctx)
		if err != nil {
			log.Printf("failed to create client connection: %v", err)
			return
		}
		defer conn.Close()
		c = gobpfman.NewBpfmanClient(conn)
	}

	// Create a context that can be cancelled
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure we cancel when we're finished

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Increment the wait group counter before starting each goroutine

	// Start your goroutines, passing the cancellable context
	wg.Add(1)
	go func() {
		defer wg.Done() // Decrement the wait group counter when the goroutine finishes
		processKprobe(cancelCtx, &paramData)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		processTracepoint(cancelCtx, &paramData)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		processTC(cancelCtx, &paramData)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		processUprobe(cancelCtx, &paramData)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		processXdp(cancelCtx, &paramData)
	}()

	// Listen for interrupt signal to gracefully shut down the goroutines
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	// Cancel the context, signaling all goroutines to stop
	log.Printf("Calling cancel()...\n")
	cancel()

	// Wait for all goroutines to finish
	log.Printf("Waiting for all goroutines to finish...\n")
	wg.Wait()

	log.Printf("Exiting go-app-counter...\n")
}

func getMapPinPath(progId uint, map_name string) (string, error) {
	appMutex.Lock()
	defer appMutex.Unlock()
	return configMgmt.RetrieveMapPinPath(ctx, c, progId, map_name)
}

func loadBpfProgram(loadRequest *gobpfman.LoadRequest) (*gobpfman.LoadResponse, error) {
	appMutex.Lock()
	defer appMutex.Unlock()
	return c.Load(ctx, loadRequest)
}

func unloadBpfProgram(id uint) (*gobpfman.UnloadResponse, error) {
	appMutex.Lock()
	defer appMutex.Unlock()
	return c.Unload(ctx, &gobpfman.UnloadRequest{Id: uint32(id)})
}
