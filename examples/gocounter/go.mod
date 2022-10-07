module github.com/redhat-et/bpfd/examples/gocounter

go 1.18

replace github.com/redhat-et/bpfd/clients/gobpfd => ../../clients/gobpfd

require (
	github.com/cilium/ebpf v0.9.0
	github.com/redhat-et/bpfd/clients/gobpfd v0.0.0-20220529154805-b196cc1fe9d0
	golang.org/x/sys v0.0.0-20220520151302-bc2c85ada10a
	google.golang.org/grpc v1.46.2
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	golang.org/x/net v0.0.0-20211015210444-4f30a5c0130f // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/genproto v0.0.0-20210722135532-667f2b7c528f // indirect
	google.golang.org/protobuf v1.28.0 // indirect
)
