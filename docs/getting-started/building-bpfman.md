# Setup and Building bpfman

This section describes how to build bpfman.
If this is the first time building bpfman, jump to the
[Development Environment Setup](#development-environment-setup) section for help installing
the tooling.

There is also an option to run images from a given release as opposed to building locally.
Jump to the [Run bpfman From Release Image](./running-release.md) section for installing
from a fixed release.

## Kernel Versions

eBPF is still a relatively new technology and being actively developed.
To take advantage of this constantly evolving technology, it is best to use the newest
kernel version possible.
If bpfman needs to be run on an older kernel, this section describes some of the kernel
features bpfman relies on to work and which kernel the feature was first introduced.

Major kernel features leveraged by bpfman:

* **Program Extensions:** Program Extensions allows bpfman to load multiple XDP or TC eBPF programs
  on an interface, which is not natively supported in the kernel.
  A `dispatcher` program is loaded as the one program on a given interface, and the user's XDP or TC
  programs are loaded as extensions to the `dispatcher` program.
  Introduced in Kernel 5.6.
* **Pinning:** Pinning allows the eBPF program to remain loaded when the loading process (bpfman) is
  stopped or restarted.
  Introduced in Kernel 4.11.
* **BPF Perf Link:** Support BPF perf link for tracing programs (Tracepoint, Uprobe and Kprobe)
  which enables pinning for these program types.
  Introduced in Kernel 5.15.

Tested kernel versions:

* Fedora 34: Kernel 5.17.6-100.fc34.x86_64
    * XDP, TC, Tracepoint, Uprobe and Kprobe programs all loaded with bpfman running on localhost
      and running as systemd service.
* Fedora 33: Kernel 5.14.18-100.fc33.x86_64
    * XDP and TC programs loaded with bpfman running on localhost and running as systemd service
      once SELinux was disabled (see https://github.com/fedora-selinux/selinux-policy/pull/806).
    * Tracepoint, Uprobe and Kprobe programs failed to load because they require the `BPF Perf Link`
      support.
* Fedora 32: Kernel 5.11.22-100.fc32.x86_64
    * XDP and TC programs loaded with bpfman running on localhost once SELinux was disabled
      (see https://github.com/fedora-selinux/selinux-policy/pull/806).
    * bpfman fails to run as a systemd service because of some capabilities issues in the
      bpfman.service file.
    * Tracepoint, Uprobe and Kprobe programs failed to load because they require the `BPF Perf Link`
      support.
* Fedora 31: Kernel 5.8.18-100.fc31.x86_64
    * bpfman was able to start on localhost, but XDP and TC programs wouldn't load because
      `BPF_LINK_CREATE` call was updated in newer kernels.
    * bpfman fails to run as a systemd service because of some capabilities issues in the
      bpfman.service file.

## Clone the bpfman Repo

You can build and run bpfman from anywhere. However, if you plan to make changes
to the bpfman operator, it will need to be under your `GOPATH` because Kubernetes
Code-generator does not work outside of `GOPATH` [issue
86753](https://github.com/kubernetes/kubernetes/issues/86753).  Assuming your
`GOPATH` is set to the typical `$HOME/go`, your repo should live in
`$HOME/go/src/github.com/bpfman/bpfman`

```
mkdir -p $HOME/go/src/github.com/bpfman
cd $HOME/go/src/github.com/bpfman
git clone git@github.com:bpfman/bpfman.git
```

## Building bpfman

To just test with the latest bpfman, containerized image are stored in `quay.io/bpfman`
(see [bpfman Container Images](../developer-guide/image-build.md)).
To build with local changes, use the following commands.

If you are building bpfman for the first time OR the eBPF code has changed:

```console
cargo xtask build-ebpf --libbpf-dir /path/to/libbpf
```

If protobuf files have changed:

```console
cargo xtask build-proto
```

To build bpfman:

```console
cargo build
```

## Development Environment Setup

To build bpfman, the following packages must be installed.

### Install Rust Toolchain

For further detailed instructions, see
[Rust Stable & Rust Nightly](https://www.rust-lang.org/tools/install).

```console
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source "$HOME/.cargo/env"
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

### Install Yaml Formatter

As part of CI, the Yaml files are validated with a Yaml formatter.
Optionally, to verify locally, install the
[YAML Language Support by Red Hat](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)
VsCode Extension, or to format in bulk, install`prettier`.

To install `prettier`:

```console
npm install -g prettier
```

Then to flag which files are violating the formatting guide, run:

```console
prettier -l "*.yaml"
```

And to write changes in place, run:

```console
 prettier -f "*.yaml"
```
