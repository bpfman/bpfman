//go:build linux
// +build linux

package main

import (
	"os"
	"os/signal"
	"syscall"
)

const (
	DefaultByteCodeFile = "bpf_bpfel.o"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/app_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	processKprobe(stop)

	processTracepoint(stop)

	processTC(stop)

	processUprobe(stop)

	processXdp(stop)
}
