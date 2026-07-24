# The nested Fedora VM test harness

Some e2e tests need a kernel with `bpf` in the active LSM list
(`/sys/kernel/security/lsm`). GitHub-hosted runners boot without it
and cannot be rebooted mid-job, and many developer machines lack it
too. The harness in this directory runs the whole e2e suite inside a
throwaway Fedora VM -- stock Fedora kernels ship bpf-LSM active --
with the repository shared into the guest over virtiofs, so the guest
builds and tests your working tree directly.

Four scripts cooperate; each documents its full interface in its
header.

| Script | Role |
|---|---|
| `fedora-vm-host-deps.sh` | One-off host setup: installs qemu, virtiofsd 1.13+, genisoimage and friends (dnf on Fedora, apt on Ubuntu/Debian with a crates.io build of virtiofsd where the packaged one is too old). |
| `fedora-vm.sh` | The harness: boots a fresh Fedora cloud VM, shares host directories over virtiofs (`docker -v` style), runs a command in the guest over ssh, exits with its status. |
| `install-fedora-deps.sh` | Guest provisioning: installs the Fedora build/test toolchain. Run once per content-change via `--provision`, baked into a cached disk. |
| `nested-vm-e2e.sh` | The in-guest driver: builds, runs the unit tests, the gRPC e2e suite and the `.bpfman` script corpus. Also runs directly on any Fedora box. |

## Quick start

```
hack/fedora-vm-host-deps.sh        # once per host
hack/fedora-vm.sh --provision hack/install-fedora-deps.sh --run hack/nested-vm-e2e.sh
```

The first run downloads the Fedora cloud image (cached in
`~/.cache/bpfman-fedora-vm`), boots it, runs the provisioning script
in the guest and keeps the resulting disk, keyed on the provisioning
script's content -- it rebuilds itself whenever
`install-fedora-deps.sh` changes. Subsequent runs boot the provisioned
disk, build incrementally against the `.gocache` persisted on the
share, and complete in a few minutes; a quick `--run` command
round-trips in about 45 seconds.

## Other useful shapes

```
# Interactive shell in the guest (repository shared at its host path).
hack/fedora-vm.sh --provision hack/install-fedora-deps.sh

# One-off command; exit status propagates.
hack/fedora-vm.sh --run 'uname -r && cat /sys/kernel/security/lsm'

# Share extra host directories, read-only where wanted.
hack/fedora-vm.sh -v /path/on/host:/path/in/guest:ro --run '...'

# Only build and unit-test, or only run the e2e stages.
hack/fedora-vm.sh --provision hack/install-fedora-deps.sh --run 'hack/nested-vm-e2e.sh build'
hack/fedora-vm.sh --provision hack/install-fedora-deps.sh --run 'hack/nested-vm-e2e.sh test'
```

Environment knobs (see the `fedora-vm.sh` header for the full list):
`VIRTFS_FAST=1` enables writeback virtiofs caching, recommended for
build workloads; `VM_CPUS` (default: host `nproc`) and `VM_MEMORY`
(default 4G) size the guest; `VM_ARCH=aarch64` on an x86_64 host (or
vice versa) runs a TCG-emulated cross-architecture smoke test;
`SERIAL_LOG=<file>` captures the guest console for debugging a boot
that never reaches ssh.

## How the share stays writable

Unprivileged virtiofsd sandboxes itself in a user namespace that maps
only the invoking user's uid and gid, and switches credentials to the
caller's guest identity on inode-creating operations -- so without
help, any other guest identity (including root under sudo, which the
e2e needs) fails with EINVAL. The harness squashes every guest uid/gid
to the invoking user's own via `--translate-uid`/`--translate-gid`,
which is why virtiofsd 1.13+ is required. Everything the guest creates
on the share lands owned by you.

## CI

`.github/workflows/nested-vm-e2e.yml` runs the same three stages
(provision, build, test) as separate steps on a hosted runner, with
the base image, provisioned disk and guest Go cache persisted via
`actions/cache`.
