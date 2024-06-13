//go:build linux
// +build linux

package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const (
	DefaultByteCodeFile = "bpf_bpfel.o"
)

var initMutex = &sync.Mutex{}

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/app_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go processKprobe(stop)

	go processTracepoint(stop)

	go processTC(stop)

	go processUprobe(stop)

	go processXdp(stop)

	//select {} // wait forever

	<-stop

	log.Printf("Exiting go-app-counter...\n")
}
