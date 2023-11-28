# Troubleshooting

This section provides a list of common issues and solutions when working with `bpfman`.

## XDP

### XDP Program Fails to Load

When attempting to load an XDP program and the program fails to load:

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface veth92cd99b --priority 100
Error: status: Aborted, message: "An error occurred. dispatcher attach failed on interface veth92cd99b: `bpf_link_create` failed", details: [], metadata: MetadataMap { headers: {"content-type": "application/grpc", "date": "Tue, 28 Nov 2023 13:37:02 GMT", "content-length": "0"} }
```

The log may look something like this:

```console
Nov 28 08:36:58 ebpf03 bpfman[2081732]: The bytecode image: quay.io/bpfman-bytecode/xdp_pass:latest is signed
Nov 28 08:36:59 ebpf03 bpfman[2081732]: Loading program bytecode from container image: quay.io/bpfman-bytecode/xdp_pass:latest
Nov 28 08:37:01 ebpf03 bpfman[2081732]: The bytecode image: quay.io/bpfman/xdp-dispatcher:v2 is signed
Nov 28 08:37:02 ebpf03 bpfman[2081732]: BPFMAN load error: Error(
                                            "dispatcher attach failed on interface veth92cd99b: `bpf_link_create` failed",
                                        )
```

The issue may be the there is already an external XDP program loaded on the given interface.
bpfman allows multiple XDP programs on an interface by loading a `dispatcher` program
which is the XDP program and additional programs are loaded as extensions to the `dispatcher`.
Use `bpftool` to determine if any programs are already loaded on an interface:

```console
$ sudo bpftool net list dev veth92cd99b
xdp:
veth92cd99b(32) generic id 8733

tc:
veth92cd99b(32) clsact/ingress tc_dispatcher id 8922

flow_dissector:
```
