# ![bpfd](./docs/img/bpfd.svg)

A system daemon for managing eBPF programs.
## Why bpfd?

bpfd seeks to solve the following problems:

- To allow multiple XDP programs to share the same interface
- To give administrators control over who can load programs and to allow them to define rules for ordering of networking eBPF programs
- To allow programs to be loaded automatically at system launch time
- To simplify the packaging and loading of eBPF-based infrastructure software (i.e Kubernetes CNI plugins)

## How does it work?

bpfd is built using [Aya](https://aya-rs.dev) an eBPF library written in Rust.
It offers two ways of interaction:

- `bpfctl`: a command line tool
- using GRPC

It is expected that humans will use `bpfctl` whereas other applications on the system wishing to load programs using
bpfd will use the GRPC. This allows for API bindings to be generated in any language supported by protocol buffers.
We are initially targeting Go and Rust.

In order to allow the attachment of multiple XDP programs to the same interface, we have implemented the
[libxdp multiprog protocol](https://github.com/xdp-project/xdp-tools/blob/master/lib/libxdp/protocol.org).
Offering this in bpfd allows for XDP applications whose loader was not using libxdp to benefit from this.
We are also hoping to find a way for applications linked with libxdp to use bpfd instead if it's
in use in the system.

## Development Requirements

- Rust Stable & Rust Nightly
- [bpf-linker](https://github.com/aya-rs/bpf-linker)
- protoc
- LLVM 11 or later
- ... and make sure the submodules are checked out

## Building bpfd

```
$ cargo xtask build-ebpf --release
$ cargo build
```

## Usage

Load a sample XDP Program:
```
$ cargo build
$ sudo ./target/debug/bpfd&
$ ./target/debug/bpfctl load /path/to/xdp/program -p xdp -i wlp2s0 --priority 50 -s "pass"
```
## License

## bpfd-ebpf

Code in this crate is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the GPL-2 license, shall be dual licensed as above, without any additional terms or conditions.

## bpfd, bpfd-common

Rust code in all other crates is distributed under the terms of either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

The `bpfd` crate also contains eBPF code that is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option. It is packaged, in object form, inside the `bpfd` binary.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL
[BSD 2 Clause]: LICENSE-BSD2
