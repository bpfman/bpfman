# Setup and Building bpfd

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
  - See [Quick Start Guide for gRPC in
    Go](https://grpc.io/docs/languages/go/quickstart/) for installation
    instructions

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
[image-build.md](./image-build.md)). To build with local changes, use the following commands.

If you are building bpfd for the first time OR the BPF code has changed:

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
