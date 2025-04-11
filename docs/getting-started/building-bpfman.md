# Setup and Building bpfman

This section describes how to build bpfman.
If this is the first time building bpfman, the
[Development Environment Setup](#development-environment-setup) section describes all packages needed
to build bpfman.

There is also an option to run prebuilt images from a given release or from an RPM, as opposed to
building locally.
Jump to:

* [Run bpfman From Release Image](./running-release.md) for installing from a prebuilt fixed release.
* [Run bpfman From RPM](./running-rpm.md) for installing from a prebuilt RPM.

## Kernel Versions

eBPF is still a relatively new technology that is being actively developed.
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
* **Relaxed CAP_BPF Requirement:** Prior to Kernel 5.19, all eBPF system calls required CAP_BPF.
  This required userspace programs that wanted to access eBPF maps to have the CAP_BPF Linux capability.
  With the kernel 5.19 change, CAP_BPF is only required for load and unload requests.
* **TCX:** TCX has performance improvements over TC and adds support in the kernel for multiple TCX
  programs to run on a given TC hook point. TCX support was added in Kernel 6.6.

bpfman tested on older kernel versions:

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

### Install SSL Library

`dnf` based OS:

```console
sudo dnf install openssl-devel
```

`apt` based OS:

```console
sudo apt install libssl-dev
```

### Install bpf Helper Header Files

`apt` based OS:

```console
sudo apt install libbpf-dev
```

### Install Protobuf Compiler

If any of the [Protobuf files](https://github.com/bpfman/bpfman/tree/main/proto) need to be updated,
then the protobuf-compiler will need to be installed.
See [RPC Protobuf Generation](../developer-guide/develop-operator.md#rpc-protobuf-generation) for bpfman
use of protobufs and see [protoc](https://grpc.io/docs/protoc-installation/) for more detailed installation
instructions.

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

### Install docker or podman

To build the `bpfman-agent` and `bpfman-operator` using the provided Makefile and the
`make build-images` command, `docker` or `podman` needs to be installed.
There are several existing guides:

* Fedora: [https://developer.fedoraproject.org/tools/docker/docker-installation.html](https://developer.fedoraproject.org/tools/docker/docker-installation.html)
* Linux: [https://docs.docker.com/engine/install/](https://docs.docker.com/engine/install/)

### Install Kind

Optionally, to test `bpfman` running in Kubernetes, the easiest method and the one documented
throughout the `bpfman` documentation is to run a Kubernetes Kind cluster.
See [kind](https://kind.sigs.k8s.io/) for documentation and installation instructions.
`kind` also requires `docker` to be installed.

!!! Note
    By default, bpfman-operator deploys bpfman with CSI enabled.
    CSI requires Kubernetes v1.26 due to a PR
    ([kubernetes/kubernetes#112597](https://github.com/kubernetes/kubernetes/pull/112597))
    that addresses a gRPC Protocol Error that was seen in the CSI client code and it doesn't appear
    to have been backported.
    kind v0.20.0 or later is recommended.

If the following error is seen, it means there is an older version of Kubernetes running and it
needs to be upgraded.

```console
kubectl get pods -A
NAMESPACE   NAME                               READY   STATUS             RESTARTS      AGE
bpfman      bpfman-daemon-2hnhx                2/3     CrashLoopBackOff   4 (38s ago)   2m20s
bpfman      bpfman-operator-6b6cf97857-jbvv4   2/2     Running            0             2m22s
:

kubectl logs -n bpfman bpfman-daemon-2hnhx -c node-driver-registrar
:
E0202 15:33:12.342704       1 main.go:101] Received NotifyRegistrationStatus call: &RegistrationStatus{PluginRegistered:false,Error:RegisterPlugin error -- plugin registration failed with err: rpc error: code = Internal desc = stream terminated by RST_STREAM with error code: PROTOCOL_ERROR,}
E0202 15:33:12.342723       1 main.go:103] Registration process failed with error: RegisterPlugin error -- plugin registration failed with err: rpc error: code = Internal desc = stream terminated by RST_STREAM with error code: PROTOCOL_ERROR, restarting registration container.
```

### Install bash-completion

`bpfman` uses the Rust crate `clap` for the CLI implementation.
`clap` has an optional Rust crate `clap_complete`. For `bash` shell, it leverages
`bash-completion` for CLI Command <TAB> completion.
So in order for CLI <TAB> completion to work in a `bash` shell, `bash-completion`
must be installed.
This feature is optional.

For the CLI <TAB> completion to work after installation, `/etc/profile.d/bash_completion.sh`
must be sourced in the running sessions.
New login sessions should pick it up automatically.

`dnf` based OS:

```console
sudo dnf install bash-completion
source /etc/profile.d/bash_completion.sh
```

`apt` based OS:

```console
sudo apt install bash-completion
source /etc/profile.d/bash_completion.sh
```

### Install Yaml Formatter

As part of CI, the Yaml files are validated with a Yaml formatter.
Optionally, to verify locally, install the
[YAML Language Support by Red Hat](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)
VsCode Extension, or to format in bulk, install `prettier`.

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

### Install toml Formatter

As part of CI, the toml files are validated with a toml formatter.
Optionally, to verify locally, install `taplo`.

```console
cargo install taplo-cli
```

And to verify locally:

```console
taplo fmt --check
```

## Clone the bpfman and bpfman-operator Repositories

You can build and run bpfman from anywhere.
For simplicity throughout this documentation, all examples will reference
`bpfman/` and `bpfman-operator/` to indicate which repository is being used.
bpfman-operator only needs to be cloned if deploying in Kubernetes.

```
cd $SRC_DIR
git clone https://github.com/bpfman/bpfman.git
git clone https://github.com/bpfman/bpfman-operator.git
```

## Building bpfman

If you are building bpfman for the first time OR the eBPF code has changed:

```console
cd bpfman/
cargo xtask build-ebpf --libbpf-dir /path/to/libbpf
```

If protobuf files have changed (see
[RPC Protobuf Generation](../developer-guide/develop-operator.md#rpc-protobuf-generation)):

```console
cargo xtask build-proto
```

To build bpfman:

```console
cargo build
```

## Building CLI TAB completion files

Optionally, to build the CLI TAB completion files, run the following command:

```console
cd bpfman/
cargo xtask build-completion
```

Files are generated for different shells:

```console
ls .output/completions/
_bpfman  bpfman.bash  bpfman.elv  bpfman.fish  _bpfman.ps1
```

### bash

For `bash`, this generates a file that can be used by the linux `bash-completion`
utility (see [Install bash-completion](#install-bash-completion) for installation
instructions).

If the files are generated, they are installed automatically when using the install
script (i.e. `sudo ./scripts/setup.sh install` - See
[Run as a systemd Service](example-bpf-local.md#run-as-a-systemd-service)).
To install the files manually, copy the file associated with a given shell to
`/usr/share/bash-completion/completions/`.
For example:

```console
sudo cp .output/completions/bpfman.bash /usr/share/bash-completion/completions/.

bpfman g<TAB>
```

### Other shells

Files are generated other shells (Elvish, Fish, PowerShell and zsh).
For these shells, generated file must be manually installed.

## Building CLI Manpages

Optionally, to build the CLI Manpage files, run the following command:

```console
cd bpfman/
cargo xtask build-man-page
```

If the files are generated, they are installed automatically when using the install
script (i.e. `sudo ./scripts/setup.sh install` - See
[Run as a systemd Service](example-bpf-local.md#run-as-a-systemd-service)).
To install the files manually, copy the generated files to `/usr/local/share/man/man1/`.
For example:

```console
sudo cp .output/manpage/bpfman*.1 /usr/local/share/man/man1/.
```

Once installed, use `man` to view the pages.

```console
man bpfman attach
```

!!! Note
    `bpfman` commands with subcommands (specifically `bpfman load`, `bpfman list` and `bpfman get`)
    have `-` in the manpage subcommand generation.
    So use `man bpfman load-file`, `man bpfman load-image`, `man bpfman load-image-xdp`,
    `man bpfman list-programs`, etc. to display the subcommand manpage files.

## Building bpfman-operator

Building and deploying bpfman-operator is covered in it's own section.
See [Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) and
[Developing the bpfman-operator](../developer-guide/develop-operator.md).
