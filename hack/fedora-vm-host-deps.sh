#!/usr/bin/env bash
#
# Install the host-side tooling hack/fedora-vm.sh needs: qemu,
# virtiofsd (the Rust daemon, 1.13+ for --translate-uid/--translate-gid),
# genisoimage, qemu-img, ssh, ssh-keygen and curl. /dev/kvm must be
# present and accessible; on most distros that means membership of the
# kvm group or a udev rule.
#
# Fedora installs everything from dnf. Ubuntu/Debian installs qemu and
# genisoimage from apt, but noble packages virtiofsd 1.10, which
# predates the uid/gid translation the harness depends on, so a new
# enough virtiofsd is built from crates.io when the packaged one is
# missing or too old (this needs cargo, libseccomp-dev and
# libcap-ng-dev, which are installed alongside). Other distros get the
# requirements list; a Nix host takes them from a devshell providing
# qemu and virtiofsd.
#
# Usage: hack/fedora-vm-host-deps.sh
#   Re-run safely; package installs are idempotent.

set -euo pipefail

sudo_cmd=
if [ "$(id -u)" -ne 0 ]; then
    sudo_cmd=sudo
fi

# virtiofsd_new_enough succeeds when a virtiofsd on PATH supports the
# uid/gid translation options (1.13+).
virtiofsd_new_enough() {
    command -v virtiofsd >/dev/null && virtiofsd --help 2>/dev/null | grep -q -- --translate-uid
}

if command -v dnf >/dev/null; then
    $sudo_cmd dnf install -y qemu-system-x86 qemu-img virtiofsd genisoimage openssh-clients curl
elif command -v apt-get >/dev/null; then
    $sudo_cmd apt-get update
    $sudo_cmd apt-get install -y --no-install-recommends \
        qemu-system-x86 qemu-utils genisoimage openssh-client curl \
        libseccomp-dev libcap-ng-dev
    # Debian/Ubuntu ship virtiofsd in /usr/libexec, off PATH, and (as
    # of noble) at 1.10; build 1.13 from crates.io when what is
    # reachable is missing or too old.
    if [ -x /usr/libexec/virtiofsd ] && ! command -v virtiofsd >/dev/null; then
        $sudo_cmd ln -sf /usr/libexec/virtiofsd /usr/local/bin/virtiofsd
    fi
    if ! virtiofsd_new_enough; then
        if ! command -v cargo >/dev/null; then
            echo "error: packaged virtiofsd is missing or predates --translate-uid," >&2
            echo "       and cargo is not available to build 1.13 from crates.io." >&2
            echo "       Install rustup/cargo and re-run." >&2
            exit 1
        fi
        cargo install virtiofsd --version 1.13.2 --root "$HOME/.local"
        $sudo_cmd ln -sf "$HOME/.local/bin/virtiofsd" /usr/local/bin/virtiofsd
    fi
else
    echo "error: no supported package manager (dnf, apt-get) found." >&2
    echo "       Provide these tools yourself: qemu-system-x86_64, qemu-img," >&2
    echo "       virtiofsd 1.13+ (the Rust daemon), genisoimage, ssh," >&2
    echo "       ssh-keygen, curl. On a Nix host use a devshell providing" >&2
    echo "       qemu and virtiofsd." >&2
    exit 1
fi

if ! virtiofsd_new_enough; then
    echo "error: virtiofsd on PATH does not support --translate-uid (need 1.13+)." >&2
    exit 1
fi

[ -e /dev/kvm ] || echo "warning: /dev/kvm absent; the harness will fall back to TCG (slow)" >&2
echo "Host tooling for hack/fedora-vm.sh is in place."
