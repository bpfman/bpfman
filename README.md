# ![bpfd](./docs/img/bpfd.svg)

A system daemon for managing BPF programs.
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
See [tutorial.md](docs/admin/tutorial.md) for some examples of starting `bpfd`, managing logs, and using `bpfctl`.

In order to allow the attachment of multiple XDP programs to the same interface, we have implemented the
[libxdp multiprog protocol](https://github.com/xdp-project/xdp-tools/blob/master/lib/libxdp/protocol.org).
Offering this in bpfd allows for XDP applications whose loader was not using libxdp to benefit from this.
We are also hoping to find a way for applications linked with libxdp to use bpfd instead if it's
in use in the system.

## Development Environment Setup

- [Rust Stable & Rust Nightly](https://www.rust-lang.org/tools/install) 

```console
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
export PATH="$HOME/.cargo/bin:$PATH"
rustup toolchain install nightly -c rustfmt,clippy,rust-src
```
- LLVM 11 or later (Linux package managers should provide a recent enough release)

`dnf` based OS:
```console
sudo dnf install llvm-devel clang-devel elfutils-libelf-devel

```

`apt` based OS:
```console
sudo apt install clang lldb lld libelf-dev gcc-multilib

```

- [bpf-linker](https://github.com/aya-rs/bpf-linker)

```console
cargo install bpf-linker
```

- [protoc](https://grpc.io/docs/protoc-installation/)

`dnf` based OS:
```console
sudo dnf install protobuf-compiler
```

`apt` based OS:
```console
sudo apt install protobuf-compiler
```

- go protobuf compiler extensions
  - See [Quick Start Guide for gRPC in Go](https://grpc.io/docs/languages/go/quickstart/) for installation instructions

- A checkout of libbpf

```console
git clone https://github.com/libbpf/libbpf --branch v0.8.0
```

- Perl

`dnf` based OS:
```console
sudo dnf install perl
```

`apt` based OS:
```console
sudo apt install perl
```


## Building bpfd

To just test with the latest bpfd, containerized image are stored in `quay.io/bpfd` (see
[image-build.md](docs/developer/image-build.md)). To build with local changes, use the following commands.


If you are building bpfd for the first time OR the BPF code has changed:
```console
$ cargo xtask build-ebpf --libbpf-dir /path/to/libbpf
```

If protobuf files have changed:
```console
$ cargo xtask build-proto
```

To build bpfd and bpfctl:
```console
$ cargo build
```

## Usage

Load a sample XDP Program:
```console
$ cargo build
$ sudo ./target/debug/bpfd&
$ sudo ./target/debug/bpfctl load --path /path/to/xdp/program --section-name "pass" xdp -i wlp2s0 --priority 50
```

See [tutorial.md](docs/admin/tutorial.md) for some examples of starting `bpfd`, managing logs, and using `bpfctl`.
See [Example BPF Programs](docs/developer/example-bpf.md) for some examples of BPF programs written in Go,
interacting with `bpfd`.

## License

## bpfd-ebpf

Code in this crate is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the GPL-2 license, shall be dual licensed as above, without any additional terms or conditions.

## bpfd, bpfd-common

Rust code in all other crates is distributed under the terms of either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

The `bpfd` crate also contains BPF code that is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option. It is packaged, in object form, inside the `bpfd` binary.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL
[BSD 2 Clause]: LICENSE-BSD2
