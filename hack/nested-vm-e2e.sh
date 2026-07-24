#!/usr/bin/env bash
#
# Build and run the e2e suite on a Fedora system whose kernel has
# "bpf" in the active LSM list (the environment BPF_PROG_TYPE_LSM
# tests will need; the corpus runs the same either way). Idempotent:
# installs build deps, builds, loads the e2e kmod, then runs the
# gRPC e2e suite and the .bpfman script corpus.
#
# Meant to be handed to hack/fedora-vm.sh (which supplies exactly such a
# Fedora guest); --provision caches a disk with the deps pre-installed:
#
#   hack/fedora-vm.sh --provision hack/install-fedora-deps.sh --run hack/nested-vm-e2e.sh
#
# but it also runs directly on any Fedora box with bpf-LSM active
# (stock Fedora kernels ship it on by default).

set -euo pipefail

cd "$(dirname "$0")/.."

# Persist the Go build cache on the repository (the virtiofs share under
# hack/fedora-vm.sh) so a rerun in a fresh VM compiles incrementally
# instead of from cold. Harmless on a normal filesystem.
export GOCACHE="$PWD/.gocache"

if ! grep -qw bpf /sys/kernel/security/lsm; then
    echo "error: bpf is not in the active LSM list ($(cat /sys/kernel/security/lsm))." >&2
    echo "       BPF_PROG_TYPE_LSM programs cannot attach here." >&2
    echo "       Use a kernel with bpf-LSM (stock Fedora) or boot with lsm=...,bpf." >&2
    exit 1
fi

# An optional stage argument narrows the run so CI can surface build
# and test as separate steps (each in its own VM boot): "build"
# compiles the binaries, test binaries and kmod, then runs the unit
# tests; "test" loads the kmod and runs the e2e tests, expecting a
# prior build on the share; no argument does both.
stage="${1:-all}"
case "$stage" in
    all|build|test) ;;
    *) echo "usage: $0 [build|test]" >&2; exit 2 ;;
esac

hack/install-fedora-deps.sh

if [[ "$stage" != test ]]; then
    make bpfman-compile
    make build-e2e-grpc build-e2e-scripts
    make e2e-kmod-build
    make test
fi

if [[ "$stage" != build ]]; then
    # The full gRPC e2e suite (its kfunc-backed tests gate
    # on the kmod loaded here) and the whole .bpfman script corpus
    # (the default !external selector still applies).
    make e2e-kmod-reload
    make test-e2e-grpc STRESS_COUNT=1
    make test-e2e-scripts STRESS_COUNT=5
fi
