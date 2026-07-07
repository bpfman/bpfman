#!/usr/bin/env bash
#
# Install a foreign-arch cross gcc and a matching sysroot for
# cross-compiling go-bpfman from Fedora to another Linux arch.
# After running this, `make bpfman-compile` with the appropriate
# CC/GOARCH overrides will produce a foreign-arch binary natively
# (no QEMU emulation required).
#
# The sysroot is a minimal Fedora installroot containing just the
# C runtime, kernel headers, and their dependencies, populated
# from Fedora's foreign-arch repositories via `dnf --installroot
# --forcearch`. The cross gcc is then pointed at the populated
# sysroot via --sysroot at build time.
#
# Usage: hack/install-cross-sysroot.sh <target-arch>
#   target-arch: amd64 | arm64 | ppc64le | s390x
#                amd64 is the host arch and is a no-op.
#
# Re-run safely; dnf will skip already-installed packages.

set -euo pipefail

if ! command -v dnf >/dev/null; then
    echo "error: dnf not found; this script is Fedora-specific" >&2
    exit 1
fi

if [ "$#" -ne 1 ]; then
    echo "usage: $0 <amd64|arm64|ppc64le|s390x>" >&2
    exit 1
fi

target_arch="$1"

case "$target_arch" in
    amd64)   sysroot_arch=""        cross_gcc=""                          ;;
    arm64)   sysroot_arch="aarch64" cross_gcc="gcc-aarch64-linux-gnu"     ;;
    ppc64le) sysroot_arch="ppc64le" cross_gcc="gcc-powerpc64le-linux-gnu" ;;
    s390x)   sysroot_arch="s390x"   cross_gcc="gcc-s390x-linux-gnu"       ;;
    *) echo "unsupported target arch: $target_arch" >&2; exit 1 ;;
esac

# Skip sudo when already root (e.g. inside a container during
# `docker build`); use sudo on a regular host.
sudo_cmd=
if [ "$(id -u)" -ne 0 ]; then
    sudo_cmd=sudo
fi

if [ -z "$sysroot_arch" ]; then
    echo "$target_arch is the host arch; no cross toolchain needed."
    exit 0
fi

$sudo_cmd dnf install -y "$cross_gcc"

sysroot="/sysroots/${sysroot_arch}"
$sudo_cmd mkdir -p "${sysroot}"

# --use-host-config tells dnf to read /etc/yum.repos.d from the
# host rather than expecting an already-populated repo tree inside
# the (empty) installroot. dnf5 made this explicit; without it the
# install fails with "no repositories were loaded from the
# installroot".
#
# tsflags=noscripts skips post-install rpm scriptlets. The foreign-
# arch glibc post-install runs ldconfig, and ldconfig in a foreign-
# arch sysroot is the foreign ELF binary -- not executable on the
# build host without QEMU binfmt_misc registration. Scriptlets are
# not needed for a sysroot whose only purpose is to be linked
# against; ldconfig at runtime on the target machine handles its
# own cache.
#
# install_weak_deps=False keeps the installroot lean.
$sudo_cmd dnf install -y \
    --use-host-config \
    --installroot="${sysroot}" \
    --forcearch="${sysroot_arch}" \
    --releasever="$(rpm -E %fedora)" \
    --setopt=install_weak_deps=False \
    --setopt=tsflags=noscripts \
    --nodocs \
    glibc glibc-devel glibc-static libgcc kernel-headers

$sudo_cmd rm -rf \
    "${sysroot}/var/cache" \
    "${sysroot}/var/log" \
    "${sysroot}/var/lib/dnf"

echo "cross toolchain for ${target_arch} ready: CC=${cross_gcc/gcc-/}-gcc --sysroot=${sysroot}"
