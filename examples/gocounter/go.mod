module github.com/redhat-et/bpfd/examples/gocounter

go 1.19

replace github.com/redhat-et/bpfd/clients/gobpfd => ../../clients/gobpfd

replace github.com/redhat-et/bpfd/examples/pkg/config-mgmt => ../pkg/config-mgmt

require (
	github.com/cilium/ebpf v0.9.3
	github.com/redhat-et/bpfd/clients/gobpfd v0.0.0-20221213142718-d9b708634d05
	github.com/redhat-et/bpfd/examples/pkg/config-mgmt v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.51.0
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	golang.org/x/net v0.0.0-20220722155237-a158d28d115b // indirect
	golang.org/x/sys v0.0.0-20220928140112-f11e5e49a4ec // indirect
	golang.org/x/text v0.4.0 // indirect
	google.golang.org/genproto v0.0.0-20220502173005-c8bf987b8c21 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
)
