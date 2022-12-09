//go:build linux
// +build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	toml "github.com/pelletier/go-toml"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Stats struct {
	Packets uint64
	Bytes   uint64
}

type Tls struct {
	CaCert string `toml:"ca_cert"`
	Cert   string `toml:"cert"`
	Key    string `toml:"key"`
}

type Config struct {
	Iface        string `toml:"interface"`
	Priority     string `toml:"priority"`
	BytecodeUrl  string `toml:"bytecode_url"`
	BytecodeUuid string `toml:"bytecode_uuid"`
	BytecodePath string `toml:"bytecode_path"`
}

type ConfigData struct {
	Tls    Tls
	Config Config
}

const (
	DefaultConfigPath     = "/etc/bpfd/go-tc-counter.toml"
	DefaultRootCaPath     = "/etc/bpfd/certs/ca/ca.pem"
	DefaultClientCertPath = "/etc/bpfd/certs/bpfd-client/bpfd-client.pem"
	DefaultClientKeyPath  = "/etc/bpfd/certs/bpfd-client/bpfd-client.key"
	DefaultSocketPath     = "/var/lib/bpfd/sock/go-tc-counter.sock"
	DefaultMapDir         = "/run/bpfd/fs/maps"
)

const (
	srcNone = iota
	srcUuid
	srcUrl
	srcPath
)

const (
	TC_ACT_OK = 0
)

//go:generate bpf2go -cc clang -no-strip -cflags "-O2 -g -Wall" bpf ./bpf/tc_counter.c -- -I.:/usr/include/bpf:/usr/include/linux
func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	configData := loadConfig()

	var iface string
	var priority int
	var cmdlineUrl string
	var cmdlineUuid string
	var cmdlinePath string

	flag.StringVar(&iface, "iface", "",
		"Interface to load bytecode.")
	flag.IntVar(&priority, "priority", -1,
		"Priority to load program in bpfd")
	flag.StringVar(&cmdlineUuid, "uuid", "",
		"UUID of bytecode that has already been loaded. uuid, url and path are mutually exclusive.")
	flag.StringVar(&cmdlineUrl, "url", "",
		"URL of bytecode container image to pull from. uuid, url and path are mutually exclusive.")
	flag.StringVar(&cmdlinePath, "path", "",
		"Directory on host where bytecode file is located. uuid, url and path are mutually exclusive.")
	flag.Parse()

	var id string
	var bytecodePath string
	bytecodeSrc := srcNone

	// "-iface" is the interface to run bpf program on. If not provided, then
	// use value loaded from go-tc-counter.toml file. If not provided, use environment
	// variable. If not provided, error.
	// ./go-tc-counter -iface eth0
	if len(iface) == 0 {
		if configData.Config.Iface != "" {
			iface = configData.Config.Iface
		} else {
			iface = os.Getenv("BPFD_INTERFACE")
			if iface == "" {
				log.Print("interface is required")
				return
			}
		}
	}

	// "-priority" is the priority to load bpf program at. If not provided, then
	// use value loaded from go-tc-counter.toml file. If not provided, use environment
	// variable. If not provided, defaults to 50.
	// ./go-tc-counter -iface eth0 -priority 45
	if priority < 0 {
		var priorityStr string
		var errStr string
		var err error

		if configData.Config.Priority != "" {
			priorityStr = configData.Config.Priority
			errStr = "in toml"
		} else {
			priorityStr = os.Getenv("BPFD_PRIORITY")
			errStr = "in BPFD_PRIORITY"
		}

		if priorityStr != "" {
			priority, err = strconv.Atoi(priorityStr)
			if err != nil {
				log.Printf("Invalid priority %s: %s", errStr, priorityStr)
				return
			}
		} else {
			priority = 50
		}
	}

	// "-uuid", "-url" and "-path" are mutually exclusive and "-uuid" takes precedence.
	// Parse Commandline first.

	// "-uuid" is a UUID for the bytecode that has already loaded into bpfd. If not
	// ./go-tc-counter -iface eth0 -uuid 53ac77fc-18a9-42e2-8dd3-152fc31ba979
	if len(cmdlineUuid) == 0 {
		// "-url" is a URL for the bytecode in a container image. If not provided,
		// ./go-tc-counter -iface eth0 -url quay.io/bpfd/bytecode:go-tc-counter
		if len(cmdlineUrl) == 0 {
			// "-path" allows the location of the local bytecode file to be
			// overwritten.
			// ./go-tc-counter -iface eth0 -path /var/bpfd/bytecode/bpf_bpfel.o
			if len(cmdlinePath) != 0 {
				bytecodePath = cmdlinePath
				bytecodeSrc = srcPath
			}
		} else {
			// "-url" was entered so it is a URL
			bytecodePath = cmdlineUrl
			bytecodeSrc = srcUrl
		}
	} else {
		// "-uuid" was entered so it is a UUID
		id = cmdlineUuid
		bytecodeSrc = srcUuid
	}

	// If bytecode source not entered not entered on Commandline, check toml file.
	if bytecodeSrc == srcNone {
		if configData.Config.BytecodeUuid != "" {
			id = configData.Config.BytecodeUuid
			bytecodeSrc = srcUuid
		} else if configData.Config.BytecodeUrl != "" {
			bytecodePath = configData.Config.BytecodeUrl
			bytecodeSrc = srcUrl
		} else if configData.Config.BytecodePath != "" {
			bytecodePath = configData.Config.BytecodePath
			bytecodeSrc = srcPath
		}
	}

	// If bytecode source not entered not entered via toml file, check environment variables.
	if bytecodeSrc == srcNone {
		id = os.Getenv("BPFD_BYTECODE_UUID")
		if id == "" {
			bytecodePath = os.Getenv("BPFD_BYTECODE_URL")
			if bytecodePath == "" {
				bytecodePath = os.Getenv("BPFD_BYTECODE_PATH")
				if bytecodePath == "" {
					// Nothing entered so default to local file
					bytecodePath = "bpf_bpfel.o"
				}
				bytecodeSrc = srcPath
			} else {
				// BPFD_BYTECODE_URL was entered so it is a URL
				bytecodeSrc = srcUrl
			}
		} else {
			// BPFD_BYTECODE_UUID was entered so it is a UUID
			bytecodeSrc = srcUuid
		}
	}

	if bytecodeSrc == srcUuid {
		log.Printf("Using Input: Interface=%s Source=%s",
			iface, id)
	} else {
		log.Printf("Using Input: Interface=%s Priority=%d Source=%s",
			iface, priority, bytecodePath)
	}

	// If the bytecode src is a UUID, skip the loading and unloading of the bytecode.
	if bytecodeSrc != srcUuid {
		ctx := context.Background()

		creds, err := loadTLSCredentials(configData.Tls)
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

		var path string
		bytecode_url_flag := false
		if bytecodeSrc == srcPath {
			path, err = filepath.Abs(bytecodePath)
			if err != nil {
				conn.Close()
				log.Printf("Couldn't find bpf elf file: %v", err)
				return
			}
		} else if bytecodeSrc == srcUrl {
			path = bytecodePath
			bytecode_url_flag = true
		} else {
			log.Print("bytecode source not provided.")
			return
		}
		loadRequest := &gobpfd.LoadRequest{
			Path:        path,
			FromImage:   bytecode_url_flag,
			SectionName: "stats",
			ProgramType: gobpfd.ProgramType_TC,
			Direction:   gobpfd.Direction_INGRESS,
			AttachType: &gobpfd.LoadRequest_NetworkMultiAttach{
				NetworkMultiAttach: &gobpfd.NetworkMultiAttach{
					Priority: int32(priority),
					Iface:    iface,
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
		id = res.GetId()
		log.Printf("Program registered with %s id\n", id)

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
		}(id)
	}

	// 3. Get access to our map
	mapPath := fmt.Sprintf("%s/%s/tc_stats_map", DefaultMapDir, id)
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

			log.Printf("%d packets received\n", totalPackets)
			log.Printf("%d bytes received\n\n", totalBytes)
		}
	}()

	<-stop

	log.Printf("Exiting...\n")
}

func loadConfig() ConfigData {
	config := ConfigData{
		Tls: Tls{
			CaCert: DefaultRootCaPath,
			Cert:   DefaultClientCertPath,
			Key:    DefaultClientKeyPath,
		},
	}

	log.Printf("Reading %s ...\n", DefaultConfigPath)
	file, err := os.ReadFile(DefaultConfigPath)
	if err == nil {
		err = toml.Unmarshal(file, &config)
		if err != nil {
			log.Printf("Unmarshal failed: err %+v\n", err)
		}
	} else {
		log.Printf("Read %s failed: err %+v\n", DefaultConfigPath, err)
	}

	return config
}

func loadTLSCredentials(tlsFiles Tls) (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := os.ReadFile(tlsFiles.CaCert)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair(tlsFiles.Cert, tlsFiles.Key)
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
