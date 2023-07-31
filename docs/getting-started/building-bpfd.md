# Setup and Building bpfd

This section describes how to build bpfd.
If this is the first time building bpfd, jump to the
[Development Environment Setup](#development-environment-setup) section for help installing
the tooling.

## Clone the bpfd Repo

You can build and run bpfd from anywhere. However, if you plan to make changes
to the bpfd operator, it will need to be under your `GOPATH` because Kubernetes
Code-generator does not work outside of `GOPATH` [issue
86753](https://github.com/kubernetes/kubernetes/issues/86753).  Assuming your
`GOPATH` is set to the typical `$HOME/go`, your repo should live in
`$HOME/go/src/github.com/bpfd-dev/bpfd`

```
mkdir -p $HOME/go/src/github.com/bpfd-dev
cd $HOME/go/src/github.com/bpfd-dev
git clone git@github.com:bpfd-dev/bpfd.git
```

## Building bpfd

To just test with the latest bpfd, containerized image are stored in `quay.io/bpfd`
(see [bpfd Container Images](../developer-guide/image-build.md)).
To build with local changes, use the following commands.

If you are building bpfd for the first time OR the eBPF code has changed:

```console
cargo xtask build-ebpf --libbpf-dir /path/to/libbpf
```

If protobuf files have changed:

```console
cargo xtask build-proto
```

To build bpfd and bpfctl:

```console
cargo build
```

## Development Environment Setup

To build bpfd, the following packages must be installed.

### Install Rust Toolchain

For further detailed instructions, see
[Rust Stable & Rust Nightly](https://www.rust-lang.org/tools/install).

```console
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
export PATH="$HOME/.cargo/bin:$PATH"
rustup toolchain install nightly -c rustfmt,clippy,rust-src
```

### Install LLVM

LLVM 11 or later must be installed.
Linux package managers should provide a recent enough release.

`dnf` based OS:

```console
sudo dnf install llvm-devel clang-devel elfutils-libelf-devel
```

`apt` based OS:

```console
sudo apt install clang lldb lld libelf-dev gcc-multilib
```

### Install Protobuf Compiler

For further detailed instructions, see [protoc](https://grpc.io/docs/protoc-installation/).

`dnf` based OS:

```console
sudo dnf install protobuf-compiler
```

`apt` based OS:

```console
sudo apt install protobuf-compiler
```

### Install GO protobuf Compiler Extensions

See [Quick Start Guide for gRPC in Go](https://grpc.io/docs/languages/go/quickstart/) for
installation instructions.

### Local libbpf

Checkout a local copy of libbpf.

```console
git clone https://github.com/libbpf/libbpf --branch v0.8.0
```

### Install perl

Install `perl`:

`dnf` based OS:

```console
sudo dnf install perl
```

`apt` based OS:

```console
sudo apt install perl
```
