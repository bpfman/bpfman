#!/usr/bin/env bash
#
# Install the Fedora RPMs needed to build, test, and lint go-bpfman.
# After running this once, every `make` target in the project
# Makefile is reachable on a stock Fedora system with no further
# setup.
#
# Coverage:
#
#   build/runtime: golang git make gcc clang llvm libbpf-devel
#                  kernel-headers kernel[-<flavour>]-devel (derived
#                  from the running kernel, so Asahi's kernel-16k,
#                  -rt, -debug, etc. all resolve correctly) bpftool
#                  dwarves kmod pkgconf-pkg-config iproute
#                  iproute-tc (provides `tc`) jq sqlite-devel file
#   static link:   glibc-static  (required for `make STATIC=1`)
#   protobuf:      protobuf-compiler  (provides `protoc`)
#   linters:       golangci-lint ShellCheck hadolint checkmake
#
# Not installed here:
#
#   docker:        docker-ce (from Docker's upstream repo) or
#                  moby-engine (Fedora-native). Only needed for
#                  `make build-image-dev` and friends (the runtime
#                  image build); the host BPF compile rules use
#                  clang/llvm/libbpf-devel/kernel-headers RPMs
#                  installed by this script.
#   protoc-gen-go,
#   protoc-gen-go-grpc:
#                  not packaged in Fedora. The Makefile installs
#                  them into ./bin via `go install` on demand,
#                  the same way it handles golangci-lint, so this
#                  script does not need to fetch them up front.
#
# Usage: hack/install-fedora-deps.sh
#   Re-run safely; dnf will skip already-installed packages.

set -euo pipefail

if ! command -v dnf >/dev/null; then
    echo "error: dnf not found; this script is Fedora-specific" >&2
    exit 1
fi

# Pick the kernel-devel package that matches the running kernel.
#
# The e2e kmod target (Makefile: e2e-kmod-build) needs kbuild to
# find /lib/modules/$(uname -r)/build, which only resolves when
# the exact -devel NVR is installed. The flavour varies by
# hardware: stock Fedora x86_64/aarch64 ships `kernel-devel`,
# Fedora Asahi ships `kernel-16k-devel`, `-rt` / `-debug` variants
# follow the same `kernel[-<flavour>]-devel-<NVR>` shape. Rather
# than hardcoding one name, we resolve the RPM that owns the
# running vmlinuz and substitute `-core-` -> `-devel-`. That works
# for every flavour Fedora ships.
#
# Container builds (Dockerfile.ci, Dockerfile.bpfman) run this
# script inside fedora:NN, where /boot is empty and `uname -r`
# reports the host's kernel. The file probe fails and we fall back
# to the plain `kernel-devel` Fedora ships, matching the previous
# container behaviour.
kernel_release=$(uname -r)
kernel_devel=kernel-devel
if kernel_core=$(rpm -qf "/boot/vmlinuz-${kernel_release}" 2>/dev/null); then
    case "$kernel_core" in
        kernel-core-*|kernel-*-core-*)
            kernel_devel=${kernel_core/-core-/-devel-}
            ;;
        *)
            echo "warning: unexpected kernel package '${kernel_core}'; falling back to kernel-devel" >&2
            ;;
    esac
fi

rpms=(
    bpftool
    checkmake
    clang
    dwarves
    file
    gcc
    git
    glibc-static
    golang
    golangci-lint
    hadolint
    iproute
    iproute-tc
    jq
    "$kernel_devel"
    kernel-headers
    kmod
    libbpf-devel
    llvm
    make
    pkgconf-pkg-config
    protobuf-compiler
    ShellCheck
    sqlite-devel
)

# Skip sudo when already root (e.g. inside a container during
# `docker build`); use sudo on a regular host where the user
# typically isn't root.
sudo_cmd=
if [ "$(id -u)" -ne 0 ]; then
    sudo_cmd=sudo
fi

$sudo_cmd dnf install -y "${rpms[@]}" "$@"

cat <<'EOF'

Fedora dependencies installed. Common starting points:

  make                  # dynamic build of bin/bpfman
  make test             # unit tests (race detector enabled)
  make STATIC=1         # static link, requires glibc-static
  make bpfman-proto     # regenerate proto stubs
  make build-image-dev  # local docker image (bpfman:dev)
  make help             # full target list

EOF
