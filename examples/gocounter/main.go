//go:build linux
// +build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Stats struct {
	Packets uint64
	Bytes   uint64
}

const (
	DefaultRootCaPath     = "/etc/bpfd/certs/ca/ca.pem"
	DefaultClientCertPath = "/etc/bpfctl/certs/gocounter/gocounter.pem"
	DefaultClientKeyPath  = "/etc/bpfctl/certs/gocounter/gocounter.key"
	DefaultSocketPath     = "/etc/bpfd/sock/gocounter.sock"
)

//go:generate bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/xdp_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	iface := os.Args[1]
	if iface == "" {
		log.Fatal("interface is required")
	}

	ctx := context.Background()

	creds, err := loadTLSCredentials()
	if err != nil {
		log.Fatalf("Failed to generate credentials for new client: %v", err)
	}

	// Set up a connection to the server.
	conn, err := grpc.DialContext(ctx, "localhost:50051", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	c := gobpfd.NewLoaderClient(conn)

	path, err := filepath.Abs("bpf_bpfel.o")
	if err != nil {
		conn.Close()
		log.Fatalf("Couldn't find bpf elf file: %v", err)
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

	// 3. Get access to our map
	mapPath := fmt.Sprintf("/var/run/bpfd/fs/maps/%s/xdp_stats_map", id)
	opts := &ebpf.LoadPinOptions{
		ReadOnly:  false,
		WriteOnly: false,
		Flags:     0,
	}
	statsMap, err := ebpf.LoadPinnedMap(mapPath, opts)
	if err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			key := uint32(2)
			var stats []Stats
			var totalPackets uint64
			var totalBytes uint64

			err := statsMap.Lookup(&key, &stats)
			if err != nil {
				log.Fatal(err)
			}

			for _, cpuStat := range stats {
				totalPackets += cpuStat.Packets
				totalBytes += cpuStat.Bytes
			}

			fmt.Printf("%d packets received\n%d bytes received\n\n", totalPackets, totalBytes)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("Exiting...")
}

func loadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := os.ReadFile(DefaultRootCaPath)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair(DefaultClientCertPath, DefaultClientKeyPath)
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	return credentials.NewTLS(config), nil
}
