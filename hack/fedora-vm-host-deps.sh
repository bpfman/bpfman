#!/usr/bin/env bash
#
# Install the host-side tooling hack/fedora-vm.sh needs: qemu for both
# x86_64 and aarch64 guests plus the aarch64 UEFI firmware (the virt
# machine has no default firmware), virtiofsd (the Rust daemon, 1.13+
# for --translate-uid/--translate-gid), genisoimage, qemu-img, ssh,
# ssh-keygen and curl. /dev/kvm must be present and accessible; on most
# distros that means membership of the kvm group or a udev rule.
#
# Fedora installs everything from dnf. Ubuntu/Debian installs qemu and
# genisoimage from apt, but noble packages virtiofsd 1.10, which
# predates the uid/gid translation the harness depends on, so a new
# enough virtiofsd is built from crates.io into ~/.local when nothing
# capable is reachable (this needs cargo, libseccomp-dev and
# libcap-ng-dev, which are installed alongside). Nothing is linked
# onto PATH: the harness probes ~/.local/bin and /usr/libexec (where
# the Fedora and Debian packages put the binary) itself, so this
# script writes only through the package manager and under your home.
# Other distros get the requirements list; a Nix host takes the tools
# from a devshell providing qemu and virtiofsd.
#
# Usage: hack/fedora-vm-host-deps.sh
#   Re-run safely; package installs are idempotent.

set -euo pipefail

sudo_cmd=
if [ "$(id -u)" -ne 0 ]; then
    sudo_cmd=sudo
fi

# resolve_virtiofsd prints the path of a virtiofsd that supports
# --translate-uid/--translate-gid (1.13+): $VIRTIOFSD when set (an
# explicit override never falls through -- it must itself be capable),
# otherwise the first capable candidate among PATH, the cargo install
# target this script uses, and the libexec directory the Fedora and
# Debian packages install into, off PATH. Capability decides, not
# position: noble's packaged /usr/libexec/virtiofsd is 1.10 and must
# lose to a good build elsewhere. Keep in sync with the copy in
# hack/fedora-vm.sh.
resolve_virtiofsd() {
    if [[ -n "${VIRTIOFSD:-}" ]]; then
        if "$VIRTIOFSD" --help 2>/dev/null | grep -q -- --translate-uid; then
            echo "$VIRTIOFSD"
            return 0
        fi
        echo "error: VIRTIOFSD=$VIRTIOFSD does not support --translate-uid (need virtiofsd 1.13+)" >&2
        return 1
    fi

    local candidate
    for candidate in "$(command -v virtiofsd || true)" "$HOME/.local/bin/virtiofsd" /usr/libexec/virtiofsd; do
        [[ -n "$candidate" && -x "$candidate" ]] || continue
        if "$candidate" --help 2>/dev/null | grep -q -- --translate-uid; then
            echo "$candidate"
            return 0
        fi
    done

    echo "error: no virtiofsd with --translate-uid (1.13+) on PATH, in ~/.local/bin or /usr/libexec; run hack/fedora-vm-host-deps.sh or set VIRTIOFSD" >&2
    return 1
}

if command -v dnf >/dev/null; then
    # edk2-aarch64 supplies the UEFI image an aarch64 guest boots from;
    # the virt machine has no default firmware. It is noarch, so an
    # x86_64 host installs it too and can smoke-test VM_ARCH=aarch64.
    $sudo_cmd dnf install -y qemu-system-x86 qemu-system-aarch64 qemu-img virtiofsd genisoimage openssh-clients curl edk2-aarch64
elif command -v apt-get >/dev/null; then
    $sudo_cmd apt-get update
    $sudo_cmd apt-get install -y --no-install-recommends \
        qemu-system-x86 qemu-system-arm qemu-efi-aarch64 qemu-utils \
        genisoimage openssh-client curl libseccomp-dev libcap-ng-dev
    # Noble's packaged virtiofsd (/usr/libexec, off PATH) is 1.10,
    # which predates --translate-uid; build 1.13 from crates.io into
    # ~/.local when nothing capable is reachable. The harness probes
    # ~/.local/bin itself, so the build needs no linking onto PATH.
    if ! resolve_virtiofsd >/dev/null 2>&1; then
        if ! command -v cargo >/dev/null; then
            echo "error: packaged virtiofsd is missing or predates --translate-uid," >&2
            echo "       and cargo is not available to build 1.13 from crates.io." >&2
            echo "       Install rustup/cargo and re-run." >&2
            exit 1
        fi
        cargo install virtiofsd --version 1.13.2 --root "$HOME/.local"
    fi
else
    echo "error: no supported package manager (dnf, apt-get) found." >&2
    echo "       Provide these tools yourself: qemu-system-x86_64," >&2
    echo "       qemu-system-aarch64 with an aarch64 UEFI image, qemu-img," >&2
    echo "       virtiofsd 1.13+ (the Rust daemon), genisoimage, ssh," >&2
    echo "       ssh-keygen, curl. On a Nix host use a devshell providing" >&2
    echo "       qemu and virtiofsd." >&2
    exit 1
fi

virtiofsd_path=$(resolve_virtiofsd) || exit 1

[ -e /dev/kvm ] || echo "warning: /dev/kvm absent; the harness will fall back to TCG (slow)" >&2
echo "Host tooling for hack/fedora-vm.sh is in place (virtiofsd: $virtiofsd_path)."
