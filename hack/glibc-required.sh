#!/usr/bin/env bash
#
# Print the GLIBC symbol-version requirements of an ELF binary.
#
# A dynamically linked binary's effective minimum-glibc-runtime
# requirement is the maximum GLIBC_x.y version stamped on any of its
# dynamic-symbol references. This script extracts that information
# from the `.gnu.version_r` section via objdump -T. Useful when
# considering whether a binary built against one glibc will load on
# a host with an older one (e.g. a Fedora-built dynamic binary on
# ubi9-minimal).
#
# Usage:
#   hack/glibc-required.sh <binary>
#       Print every required GLIBC_x.y version, then the maximum.
#
#   hack/glibc-required.sh --max <binary>
#       Print only the maximum required version. Suitable for CI
#       guards:
#
#         max=$(hack/glibc-required.sh --max bin/bpfman)
#         if [ "$(printf '%s\n2.34\n' "$max" | sort -uV | tail -1)" != "2.34" ]; then
#             echo "binary requires $max, ubi9 ships 2.34" >&2
#             exit 1
#         fi
#
# End-to-end: build a foreign-arch image, extract the binary, and
# inspect it (objdump reads ELF agnostically, so this works whether
# or not your host can execute the foreign-arch binary):
#
#   # Build via the per-arch make targets:
#   make build-image-arm64          # bpfman:dev for linux/arm64
#   make build-image-ppc64le        # bpfman:dev for linux/ppc64le
#   make build-image-s390x          # bpfman:dev for linux/s390x
#   make build-image-amd64          # bpfman:dev for linux/amd64
#
#   # Pull the binary out of the image:
#   cid=$(docker create --platform arm64 bpfman:dev)
#   docker cp "$cid":/bpfman /tmp/bpfman.aarch64
#   docker rm "$cid" >/dev/null
#
#   # Inspect:
#   hack/glibc-required.sh /tmp/bpfman.aarch64
#
# `docker create` materialises a container without starting it, so
# the entrypoint never runs and the foreign-arch binary does not
# need to execute on the host.
#
# Exits 0 if the binary is statically linked (no GLIBC requirement)
# or any GLIBC_x.y references were found. Exits non-zero if the
# binary cannot be read or contains no GLIBC_x.y references and is
# not statically linked.

set -euo pipefail

mode=full
while [ "$#" -gt 0 ]; do
    case "$1" in
        --max)
            mode=max
            shift
            ;;
        --)
            shift
            break
            ;;
        -*)
            echo "unknown option: $1" >&2
            echo "usage: $0 [--max] <binary>" >&2
            exit 1
            ;;
        *)
            break
            ;;
    esac
done

if [ "$#" -ne 1 ]; then
    echo "usage: $0 [--max] <binary>" >&2
    exit 1
fi

binary="$1"

if [ ! -r "$binary" ]; then
    echo "error: cannot read $binary" >&2
    exit 1
fi

if ! command -v objdump >/dev/null; then
    echo "error: objdump not found (install via 'dnf install -y binutils')" >&2
    exit 1
fi

versions=$(objdump -T "$binary" 2>/dev/null \
    | grep -oE 'GLIBC_[0-9]+\.[0-9]+' \
    | sort -uV \
    || true)

if [ -z "$versions" ]; then
    if command -v file >/dev/null && file -b "$binary" | grep -q "statically linked"; then
        case "$mode" in
            full) echo "no GLIBC requirements: $binary is statically linked" ;;
            max)  echo "(static)" ;;
        esac
        exit 0
    fi
    echo "error: no GLIBC_ symbols found in $binary" >&2
    exit 2
fi

case "$mode" in
    full)
        printf '%s\n' "$versions"
        printf 'max: %s\n' "$(printf '%s\n' "$versions" | tail -1)"
        ;;
    max)
        printf '%s\n' "$versions" | tail -1
        ;;
esac
