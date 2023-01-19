# Welcome to bpfd

bpfd is a system daemon for managing BPF programs.

It is currently a work in progress!

## Why bpfd?

bpfd seeks to solve the following problems:

- To allow multiple XDP programs to share the same interface
- To give administrators control over who can load programs and to allow them to define rules for ordering of networking BPF programs
- To allow programs to be loaded automatically at system launch time
- To simplify the packaging and loading of BPF-based infrastructure software (i.e Kubernetes CNI plugins)

## How does it work?

bpfd is built using [Aya](https://aya-rs.dev) an BPF library written in Rust.
It offers two ways of interaction:

- `bpfctl`: a command line tool
- using GRPC

It is expected that humans will use `bpfctl` whereas other applications on the system wishing to load programs using
bpfd will use the GRPC. This allows for API bindings to be generated in any language supported by protocol buffers.
We are initially targeting Go and Rust.

In order to allow the attachment of multiple XDP programs to the same interface, we have implemented the
[libxdp multiprog protocol](https://github.com/xdp-project/xdp-tools/tree/master/lib/libxdp/protocol.org).
Offering this in bpfd allows for XDP applications whose loader was not using libxdp to benefit from this.
We are also hoping to find a way for applications linked with libxdp to use bpfd instead if it's
in use in the system.
