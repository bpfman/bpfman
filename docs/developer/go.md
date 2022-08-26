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

```console
sudo ./scripts/setup.sh gocounter
```

This creates the certificate in a sub-directory of the `bpfctl` user (`/etc/bpfctl/certs/gocounter/`).


## Building

To rebuild the c based bpf counter program example run:

```console
    cd examples/gocounter && go generate
```

To build the userspace go client run:

```console
    cd examples/gocounter && go build
```

## Running

First start or ensure `bpfd` is up and running.

Then start the go program with:

```console
    cd examples/gocounter && sudo ./gocounter <INTERNET INTERFACE NAME>
```

The output should show the count and total bytes of packets as they pass through the
interface as shown below:

```console
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

## Running Unprivileged

To run the `gocounter` example unprivileged (without `sudo`), the following three steps must be
performed.

### Step 1: Create `bpfd` User Group

The [tutorial.md](../admin/tutorial.md) guide describes the different modes `bpfd` and be run in.
Specifically, [Unprivileged Mode](../admin/tutorial.md#unprivileged-mode) and
[Systemd Service](../admin/tutorial.md#systemd-service) sections describe how to start `bpfd` with
the `bpfd` and `bpfctl` Users and `bpfd` User Group.
`bpfd` must be started one of these two ways and `gocounter` must be run from a User that is a member
of the `bpfd` User Group.
```console
sudo usermod -a -G bpfd \$USER
exit
<LOGIN>
```

The socket that is created by `gocounter` and shared between `gocounter` and `bpfd` is created in the
`bpfd` User Group and `gocounter` must have read-write access to it:
```console
$ ls -al /etc/bpfd/sock/gocounter.sock
srwxrwx---+ 1 <USER> bpfd 0 Aug 26 11:07 /etc/bpfd/sock/gocounter.sock

```

### Step 2: Grant `gocounter` CAP_BPF Linux Capability

`gocounter` uses a map to share data between the userspace side of the program and the BPF portion.
Accessing this map requires access to the CAP_BPF capability.
Run the following command to grant `gocounter` access to the CAP_BPF capability:
```console
sudo /sbin/setcap cap_bpf=ep ./gocounter
```

Reminder: The capability must be re-granted each time `gocounter` is rebuilt.

### Step 3: Start `gocounter` without `sudo`

Start `gocounter` without `sudo`:
```console
./gocounter docker0
2022/08/26 11:07:07 Program registered with 22ba6fc1-e432-4e63-9a43-52cc3b9a7532 id
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
