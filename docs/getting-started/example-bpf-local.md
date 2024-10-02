# Deploying Example eBPF Programs On Local Host

This section describes running bpfman and the example eBPF programs on a local host.

## Example Overview

Assume the following command is run:

```console
cd bpfman/examples/go-xdp-counter/
go run -exec sudo . -iface eno3
```

The diagram below shows `go-xdp-counter` example, but the other examples operate in
a similar fashion.

![go-xdp-counter On Host](../img/gocounter-on-host.png)

Following the diagram (Purple numbers):

1. When `go-xdp-counter` userspace is started, it will send a gRPC request over unix
   socket to `bpfman-rpc` requesting `bpfman` to load the `go-xdp-counter` eBPF bytecode located
   on disk at `bpfman/examples/go-xdp-counter/bpf_bpfel.o` at a priority of 50 and on interface `eno3`.
   These values are configurable as we will see later, but for now we will use the defaults
   (except interface, which is required to be entered).
2. `bpfman` will load it's `dispatcher` eBPF program, which links to the `go-xdp-counter` eBPF program
   and return a kernel Program ID referencing the running program.
3. `bpfman list` can be used to show that the eBPF program was loaded.
4. Once the `go-xdp-counter` eBPF bytecode is loaded, the eBPF program will write packet counts
   and byte counts to a shared map.
5. `go-xdp-counter` userspace program periodically reads counters from the shared map and logs
   the value.

Below are the steps to run the example program described above and then some additional examples
that use the `bpfman` CLI to load and unload other eBPF programs.
See [Launching bpfman](../getting-started/launching-bpfman.md) for more detailed instructions on
building and loading bpfman.
This tutorial assumes bpfman has been built, `bpfman-rpc` is running, and the `bpfman` CLI is in $PATH.

## Running Example Programs

[Example eBPF Programs](./example-bpf.md) describes how the example programs work,
how to build them, and how to run the different examples.
[Build](./example-bpf.md/#building-locally) the `go-xdp-counter` program before continuing.

To run the `go-xdp-counter` program, determine the host interface to attach the eBPF
program to and then start the go program.
In this example, `eno3` will be used, as shown in the diagram at the top of the page. 
The output should show the count and total bytes of packets as they pass through the
interface as shown below:

```console
cd bpfman/examples/go-xdp-counter/

go run -exec sudo . --iface eno3
2023/07/17 17:43:58 Using Input: Interface=eno3 Priority=50 Source=/home/$USER/src/bpfman/examples/go-xdp-counter/bpf_bpfel.o
2023/07/17 17:43:58 Program registered with id 6211
2023/07/17 17:44:01 4 packets received
2023/07/17 17:44:01 580 bytes received

2023/07/17 17:44:04 4 packets received
2023/07/17 17:44:04 580 bytes received

2023/07/17 17:44:07 8 packets received
2023/07/17 17:44:07 1160 bytes received

:
```

In another terminal, use the CLI to show the `go-xdp-counter` eBPF bytecode was loaded.

```console
sudo bpfman list
 Program ID  Name       Type  Load Time
 6211        xdp_stats  xdp   2023-07-17T17:43:58-0400
```

Finally, press `<CTRL>+c` when finished with `go-xdp-counter`.

```console
:

2023/07/17 17:44:34 28 packets received
2023/07/17 17:44:34 4060 bytes received

^C2023/07/17 17:44:35 Exiting...
2023/07/17 17:44:35 Unloading Program: 6211
```

## Using CLI to Manage eBPF Programs

bpfman provides a CLI to interact with the `bpfman` Library.
Find a deeper dive into CLI syntax in [CLI Guide](./cli-guide.md).
We will load the simple `xdp-pass` program, which allows all traffic to pass through the attached
interface, `eno3` in this example.
The source code,
[xdp_pass.bpf.c](https://github.com/bpfman/bpfman/blob/main/tests/integration-test/bpf/xdp_pass.bpf.c),
is located in the [integration-test](https://github.com/bpfman/bpfman/tree/main/tests/integration-test)
directory and there is also a prebuilt image:
[quay.io/bpfman-bytecode/xdp_pass:latest](https://quay.io/bpfman-bytecode/).

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface eno3 --priority 100
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6213
 Map Owner ID:  None
 Map Used By:   6213
 Priority:      100
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6213
 Name:                             pass
 Type:                             xdp
 Loaded At:                        2023-07-17T17:48:10-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [2724]
 BTF ID:                           2834
 Size Translated (bytes):          96
 JITed:                            true
 Size JITed (bytes):               67
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

`bpfman load image` returns the same data as the `bpfman get` command.
From the output, the Program Id of `6213` can be found in the `Kernel State` section.
The Program Id can be used to perform a `bpfman get` to retrieve all relevant program
data and a `bpfman unload` when the program needs to be unloaded.

```console
sudo bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400
```

We can recheck the details about the loaded program with the `bpfman get` command:

```console
sudo bpfman get 6213
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6213
 Map Owner ID:  None
 Map Used By:   6213
 Priority:      100
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6213
 Name:                             pass
 Type:                             xdp
 Loaded At:                        2023-07-17T17:48:10-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [2724]
 BTF ID:                           2834
 Size Translated (bytes):          96
 JITed:                            true
 Size JITed (bytes):               67
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

Then unload the program:

```console
sudo bpfman unload 6213
```
