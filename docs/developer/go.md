# Go

An example application that uses the `bpfd-go` bindings can be found [here](https://github.com/redhat-et/bpfd/tree/main/examples/gocounter)

## Prerequisites 

** Additive to the the ones defined in the [main README.md](../../README.md) ** 

2. libbpf development package to get the required bpf c headers 

Fedora 

` sudo dnf install libbpf-devel ` 

Ubuntu 

` sudo apt-get install libbpf-dev ` 

3. Cilium's `bpf2go` binary

`go install github.com/cilium/ebpf/cmd/bpf2go@master` 

## Building and running
