module github.com/redhat-et/bpfd/examples/gocounter

go 1.19

replace github.com/redhat-et/bpfd/clients/gobpfd => ../../clients/gobpfd

require (
	github.com/cilium/ebpf v0.9.0
	github.com/pelletier/go-toml v1.9.5
	github.com/redhat-et/bpfd/clients/gobpfd v0.0.0-20220529154805-b196cc1fe9d0
	google.golang.org/grpc v1.47.0
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	golang.org/x/net v0.0.0-20220722155237-a158d28d115b // indirect
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/genproto v0.0.0-20220502173005-c8bf987b8c21 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
)
