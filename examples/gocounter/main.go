//go:build linux
// +build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/redhat-et/bpfd/clients/gobpfd"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Stats struct {
	Packets uint64
}

func main() {
	iface := os.Args[1]
	if iface == "" {
		log.Fatal("interface is required")
	}

	ctx := context.Background()
	// Set up a connection to the server.
	conn, err := grpc.DialContext(ctx, "localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	c := gobpfd.NewLoaderClient(conn)

	path, err := filepath.Abs("../../bpfd-ebpf/.output/xdp_counter.bpf.o")
	if err != nil {
		conn.Close()
		log.Fatal(err)
	}

	loadRequest := &gobpfd.LoadRequest{
		Path:        path,
		SectionName: "stats",
		ProgramType: gobpfd.ProgramType_XDP,
		Priority:    50,
		Iface:       iface,
	}

	// 1. Load Program using bpfd
	var res *gobpfd.LoadResponse
	res, err = c.Load(ctx, loadRequest)
	if err != nil {
		conn.Close()
		log.Fatal(err)
	}
	id := res.GetId()
	log.Printf("Program registered with %s id\n", id)

	// 2. Set up defer to unload program when this is closed
	defer func(id string) {
		_, err = c.Unload(ctx, &gobpfd.UnloadRequest{Iface: iface, Id: id})
		if err != nil {
			conn.Close()
			log.Fatal(err)
		}
		conn.Close()
	}(id)

	// 3. Set up a UDS to receive the Map FD
	const sockAddr = "/tmp/map.sock"
	syscall.Unlink(sockAddr)

	sock, err := net.ListenUnixgram("unixgram", &net.UnixAddr{sockAddr, "unix"})
	if err != nil {
		log.Fatal(err)
	}
	defer sock.Close()

	fdChan := make(chan int)
	go func(res chan int) {
		oob := make([]byte, unix.CmsgSpace(4))
		_, _, _, _, err := sock.ReadMsgUnix([]byte{}, oob)
		if err != nil {
			log.Fatal(err)
		}
		cmsgs, err := unix.ParseSocketControlMessage(oob)
		if err != nil {
			panic(err)
		}
		fds, err := unix.ParseUnixRights(&cmsgs[0])
		if err != nil {
			panic(err)
		}
		res <- fds[0]
	}(fdChan)

	// 4. Poll our map for changes
	_, err = c.GetMap(ctx, &gobpfd.GetMapRequest{
		Iface:      iface,
		Id:         id,
		MapName:    "xdp_stats_map",
		SocketPath: sockAddr,
	})
	if err != nil {
		log.Fatal(err)
	}

	mapFd := <-fdChan
	defer syscall.Close(mapFd)

	statsMap, err := ebpf.NewMapFromFD(mapFd)
	if err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			key := uint32(2)
			buf, err := statsMap.LookupBytes(key)
			if err != nil {
				log.Fatal(err)
			}

			var stats Stats
			r := bytes.NewReader(buf)
			err = binary.Read(r, binary.LittleEndian, &stats)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%d packets received\n", stats.Packets)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("Exiting...")
}
