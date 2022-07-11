# Go

An example application that uses the `bpfd-go` bindings can be found [here](https://github.com/redhat-et/bpfd/tree/main/examples/gocounter)

## Prerequisites

**Assuming bpfd is already installed and running on the system**

1. All [requirements defined by the `cilium/ebpf` package](https://github.com/cilium/ebpf#requirements)
2. libbpf development package to get the required bpf c headers

    Fedora:

    `sudo dnf install libbpf-devel`

    Ubuntu:

    `sudo apt-get install libbpf-dev`

3. Cilium's `bpf2go` binary

    `go install github.com/cilium/ebpf/cmd/bpf2go@master`

## Generate certificates for the gocounter bpfd client

`bpfd` uses mTLS for mutual authentication. To generate a client certificate for
the gocounter example run:

```
./scripts/certificates.sh client gocounter
```

## Building

To rebuild the c based bpf counter program example run:

```
    cd examples/gocounter && go generate
```

To build the userspace go client run:

```   
    cd examples/gocounter && go build
```

## Running

First start or ensure `bpfd` is up and running.

Then start the go program with:

```
    cd examples/gocounter && sudo ./gocounter <INTERNET INTERFACE NAME>
```

The output should show the count and total bytes of packets as they pass through the
interface as shown below:

```
sudo ./gocounter docker0
2022/07/05 17:53:57 Program registered with a2e26a4a-5bcf-4092-be07-c4f9b50031be id
0 packets received
0 bytes received

5 packets received
1191 bytes received

5 packets received
1191 bytes received

7 packets received
1275 bytes received

7 packets received
1275 bytes received

^CExiting...
```

Finally, press `ctrl+c` when finished.