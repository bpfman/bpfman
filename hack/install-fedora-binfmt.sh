#!/usr/bin/env bash
#
# Configure binfmt_misc + QEMU user-mode emulation on a Fedora host
# so that `docker run --platform linux/<arch>` works for foreign
# architectures. Required to actually run the per-arch images
# produced by `make build-image-<arch>` (linux/arm64,
# linux/ppc64le, linux/s390x) on an x86_64 host.
#
# Coverage: arm64, ppc64le, s390x. amd64 is the host arch and
# needs no emulation.
#
# What this installs:
#
#   qemu-user-binfmt           binfmt_misc drop-ins under
#                              /usr/lib/binfmt.d/ that register the
#                              static emulators with the kernel,
#                              using the fix-binary (F) flag so the
#                              interpreter file descriptor survives
#                              container mount-namespace lookups.
#                              Without F, foreign-arch containers
#                              fail with exec errors because the
#                              emulator path is not visible from the
#                              container's mount namespace.
#   qemu-user-static-aarch64   linux/arm64
#   qemu-user-static-ppc       linux/ppc64, linux/ppc64le
#   qemu-user-static-s390x     linux/s390x
#
# What this does:
#
#   1. dnf install the packages above.
#   2. systemctl restart systemd-binfmt.service so the kernel
#      re-reads the drop-ins shipped by qemu-user-binfmt and applies
#      the registrations immediately. The service is statically
#      enabled by sysinit.target, so no `enable` is required (and
#      `enable` would be rejected -- there is no [Install] section).
#
# Usage: hack/install-fedora-binfmt.sh
#   Re-run safely; dnf skips already-installed packages and
#   `systemctl restart` is idempotent.

set -euo pipefail

if ! command -v dnf >/dev/null; then
    echo "error: dnf not found; this script is Fedora-specific" >&2
    exit 1
fi

RPMS=(
    qemu-user-binfmt
    qemu-user-static-aarch64
    qemu-user-static-ppc
    qemu-user-static-s390x
)

sudo dnf install -y "${RPMS[@]}"
sudo systemctl restart systemd-binfmt.service

cat <<'EOF'

QEMU user-mode emulation registered with binfmt_misc. Smoke test:

  docker run --rm --platform linux/arm64   fedora:43 uname -m   # aarch64
  docker run --rm --platform linux/ppc64le fedora:43 uname -m   # ppc64le
  docker run --rm --platform linux/s390x   fedora:43 uname -m   # s390x

Run images produced by `make build-image-<arch>`:

  docker run --rm --platform linux/arm64 bpfman:dev

EOF
