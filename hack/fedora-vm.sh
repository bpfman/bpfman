#!/usr/bin/env bash
#
# Run a command inside a throwaway Fedora VM with host directories
# shared over virtiofs (docker -v style), then exit with the command's
# status. Each run gets a fresh qcow2 overlay off the base image, so no
# state carries between runs.
#
# Why a VM: attaching a BPF_PROG_TYPE_LSM program needs "bpf" in the
# active LSM list (/sys/kernel/security/lsm). Many hosts and all
# GitHub-hosted runners boot without it; a Fedora guest has it, and
# building/testing inside Fedora gives the native BPF toolchain
# (clang/libbpf-devel/bpftool) with no cross-distro workarounds.
#
# Host requirements (hack/fedora-vm-host-deps.sh installs them on
# Fedora and Ubuntu/Debian hosts): qemu-system-<arch> (x86_64 and
# aarch64), virtiofsd 1.13+ (the Rust daemon), genisoimage, ssh,
# ssh-keygen, qemu-img, curl, and /dev/kvm. The guest arch follows the
# host by default so KVM applies; VM_ARCH crosses over to TCG for
# smoke tests.
#
# Usage:
#   hack/fedora-vm.sh [options] [--run "<command>"]
#
# Options:
#   -v HOST[:GUEST[:ro]]  Share HOST at GUEST (default GUEST=HOST) over
#                         virtiofs; read-write unless :ro. Repeatable.
#                         With no -v, the repository root is shared.
#   -w GUESTDIR           Working directory for --run (default: the first
#                         read-write volume's guest path).
#   --image PATH          Base Fedora cloud image (a fresh per-run overlay
#                         is created from it; the base is never modified).
#   --provision "<cmd>"   Run <cmd> in the guest once and cache the
#                         resulting disk (keyed on the command, or on the
#                         script's content when <cmd> names a file);
#                         later runs boot from the cached disk directly.
#                         With no --run, prepare the disk and exit.
#   --run "<command>"     Run the command in the guest and exit with its
#                         status. Without it, boots interactively.
#
# A Nix store, if present on the host, is auto-shared read-only; on a
# plain Fedora host nothing Nix-specific is emitted.
#
# Environment:
#   FEDORA_IMAGE_URL  base image URL fetched when --image is omitted
#   VM_ARCH           guest arch (default: host arch; cross-arch = TCG)
#   VM_CACHE_DIR      base-image cache dir
#   VM_MEMORY (4G), VM_CPUS (nproc), SSH_PORT (2222), BOOT_TIMEOUT (300)
#   VIRTFS_FAST       1 = cache=always,writeback (near-native, default);
#                     0 = cache=auto (coherent host<->guest)
#   SERIAL_LOG        capture the guest serial console to this file

set -euo pipefail

# Guest architecture: defaults to the host's (KVM requires the two to
# match). VM_ARCH=aarch64 on an x86_64 host (or vice versa) runs under
# TCG emulation -- an order of magnitude slower, useful only for
# smoke-testing the other architecture's bring-up. Cross-arch TCG has
# no host CPU to mirror, so it gets qemu's fullest emulated CPU.
: "${VM_ARCH:=$(uname -m)}"
arch=$VM_ARCH
cpu=host
x86_cpu=host,migratable=no,+invtsc
if [[ "$arch" != "$(uname -m)" ]]; then
    cpu=max
    x86_cpu=max
    echo "warning: VM_ARCH=$arch != host $(uname -m); using TCG emulation (slow)" >&2
fi
# shellcheck disable=SC2054  # commas are qemu option syntax, not element separators
case "$arch" in
    x86_64)
        qemu_bin=qemu-system-x86_64
        arch_args=(-machine q35,accel=kvm:tcg -cpu "$x86_cpu"
                   -rtc base=utc,clock=host,driftfix=slew
                   -global kvm-pit.lost_tick_policy=discard)
        ;;
    aarch64)
        qemu_bin=qemu-system-aarch64
        # The virt machine has no default firmware; the edk2 image
        # ships with qemu and resolves via its firmware search path.
        arch_args=(-machine virt,accel=kvm:tcg,gic-version=max -cpu "$cpu"
                   -bios edk2-aarch64-code.fd)
        ;;
    *) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

: "${FEDORA_IMAGE_URL:=https://dl.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/${arch}/images/Fedora-Cloud-Base-Generic-44-1.7.${arch}.qcow2}"
: "${VM_CACHE_DIR:=${XDG_CACHE_HOME:-$HOME/.cache}/bpfman-fedora-vm}"
: "${VM_MEMORY:=4G}"
: "${VM_CPUS:=$(nproc)}"
: "${SSH_PORT:=2222}"
: "${BOOT_TIMEOUT:=300}"
: "${VIRTFS_FAST:=0}"
: "${SERIAL_LOG:=}"

image=""
run_cmd=""
workdir=""
provision_cmd=""
declare -a vol_specs=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        -v) vol_specs+=("$2"); shift 2 ;;
        -w) workdir="$2"; shift 2 ;;
        --image) image="$2"; shift 2 ;;
        --provision) provision_cmd="$2"; shift 2 ;;
        --run) run_cmd="$2"; shift 2 ;;
        -h|--help) sed -n '2,50p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
done

repo=$(cd "$(dirname "$0")/.." && pwd)

# Default share: the repository. Auto-share the Nix store read-only when
# present (so `nix` works in-guest on a Nix host) but never require it.
[[ ${#vol_specs[@]} -eq 0 ]] && vol_specs+=("$repo")
if [[ -d /nix/store ]] && ! printf '%s\n' "${vol_specs[@]}" | grep -q '^/nix\(:\|$\)'; then
    vol_specs+=("/nix:/nix:ro")
fi

for tool in "$qemu_bin" virtiofsd genisoimage ssh ssh-keygen qemu-img curl; do
    command -v "$tool" >/dev/null || { echo "error: '$tool' not on PATH" >&2; exit 1; }
done
[[ -e /dev/kvm ]] || echo "warning: /dev/kvm absent; qemu will use TCG (slow)" >&2

# Resolve the base image (explicit --image, else a cached download).
if [[ -n "$image" ]]; then
    base_image=$image
    [[ -f "$base_image" ]] || { echo "error: --image '$base_image' does not exist" >&2; exit 1; }
else
    mkdir -p "$VM_CACHE_DIR"
    base_image="$VM_CACHE_DIR/$(basename "$FEDORA_IMAGE_URL")"
    if [[ ! -f "$base_image" ]]; then
        echo "==> downloading $(basename "$base_image")"
        # The archive server drops long transfers; -C - resumes the
        # .part across attempts (and across harness reruns). The
        # progress meter is tty-only: in CI it floods the log and
        # defeats live log streaming.
        curl_progress=()
        [[ -t 2 ]] || curl_progress=(--no-progress-meter)
        for attempt in 1 2 3 4 5; do
            curl -fSL -C - --retry 3 "${curl_progress[@]}" -o "$base_image.part" "$FEDORA_IMAGE_URL" && break
            [[ "$attempt" == 5 ]] && { echo "error: download failed after $attempt attempts" >&2; exit 1; }
            echo "==> download interrupted; resuming ($attempt/5)" >&2
            sleep 3
        done
        mv "$base_image.part" "$base_image"
    fi
fi

# Parse volume specs into parallel arrays. Each: host, guest, mount opts.
declare -a v_host=() v_guest=() v_mode=()
for spec in "${vol_specs[@]}"; do
    IFS=: read -r host guest mode <<< "$spec"
    host=$(cd "$host" 2>/dev/null && pwd) || { echo "error: -v host '$host' is not a directory" >&2; exit 1; }
    v_host+=("$host")
    v_guest+=("${guest:-$host}")
    v_mode+=("${mode:-rw}")
done
# Default working directory: the first read-write volume's guest path.
if [[ -z "$workdir" ]]; then
    for i in "${!v_mode[@]}"; do [[ "${v_mode[$i]}" == rw ]] && { workdir="${v_guest[$i]}"; break; }; done
fi

work=$(mktemp -d)
declare -a virtiofsd_pids=()
qemu_pid=
# shellcheck disable=SC2329  # invoked via the EXIT trap
cleanup() {
    [[ -n "$qemu_pid" ]] && kill "$qemu_pid" 2>/dev/null || true
    for p in "${virtiofsd_pids[@]}"; do kill "$p" 2>/dev/null || true; done
    rm -rf "$work"
}
trap cleanup EXIT

# Transient SSH key for this run only; never touches the host's ~/.ssh.
ssh-keygen -q -t ed25519 -N '' -f "$work/id" -C bpfman-fedora-vm
pubkey=$(cat "$work/id.pub")

# virtiofsd cache profile.
if [[ "$VIRTFS_FAST" == 1 ]]; then
    vfs_cache=(--cache=always --writeback --thread-pool-size=128)
else
    vfs_cache=(--cache=auto --thread-pool-size=64)
fi

# Unprivileged virtiofsd sandboxes itself in a user namespace that maps
# only our own uid and gid. On inode-creating operations (create, mkdir,
# symlink) it switches credentials to the caller's guest uid/gid, and
# setresuid/setresgid to an id unmapped in that namespace fails with
# EINVAL -- so any guest identity other than exactly $(id -u):$(id -g)
# (a default cloud-init user gets its own fresh group; anything under
# sudo is 0:0) cannot create files on the share. Squash every guest
# uid/gid to our own instead: the credential switch becomes a no-op and
# everything created on the share lands as us on the host.
vfs_translate=(--translate-uid "squash-guest:0:$(id -u):4294967295"
               --translate-gid "squash-guest:0:$(id -g):4294967295")

# Build cloud-init mount/mkdir lines and qemu wiring per volume.
mount_lines=""
mkdir_lines=""
declare -a qemu_fs_args=()
for i in "${!v_host[@]}"; do
    tag="vol$i"
    opts="rw"; [[ "${v_mode[$i]}" == ro ]] && opts="ro"
    qemu_fs_args+=(-chardev "socket,id=$tag,path=$work/$tag.sock"
                   -device "vhost-user-fs-pci,queue-size=1024,chardev=$tag,tag=$tag")
    mount_lines+="  - [ \"$tag\", \"${v_guest[$i]}\", \"virtiofs\", \"$opts,_netdev,nofail\", \"0\", \"0\" ]"$'\n'
    mkdir_lines+="  - mkdir -p ${v_guest[$i]}"$'\n'
done

# virtiofsd serves exactly one vhost-user connection and then exits, so
# the daemons must be (re)started for every VM boot, not once per run.
start_virtiofsd() {
    virtiofsd_pids=()
    for i in "${!v_host[@]}"; do
        rm -f "$work/vol$i.sock"
        virtiofsd --socket-path="$work/vol$i.sock" --shared-dir="${v_host[$i]}" \
            --announce-submounts "${vfs_cache[@]}" "${vfs_translate[@]}" >>"$work/vol$i.log" 2>&1 &
        virtiofsd_pids+=("$!")
    done
    for _ in $(seq 1 50); do
        local ok=1
        for i in "${!v_host[@]}"; do [[ -S "$work/vol$i.sock" ]] || ok=0; done
        [[ "$ok" == 1 ]] && return 0
        sleep 0.1
    done
    for i in "${!v_host[@]}"; do
        echo "--- virtiofsd vol$i (${v_host[$i]}) log tail ---" >&2
        tail -5 "$work/vol$i.log" >&2 || true
    done
    fail "virtiofsd sockets never appeared"
}

# cloud-init: a uid-matched passwordless-sudo user with our transient key
# in its (guest-local) ~/.ssh, the shared dirs mounted, /tmp on tmpfs,
# and SELinux permissive (enough for virtiofs writes; no reboot needed).
# A fresh instance-id each run makes cloud-init re-apply its
# per-instance config (user, ssh key, mounts, runcmd) when booting a
# provisioned disk left over from an earlier run.
cat > "$work/meta-data" <<EOF
instance-id: bpfman-fedora-vm-$(date +%s)-$$
local-hostname: bpfman-fedora-vm
EOF
cat > "$work/user-data" <<EOF
#cloud-config
users:
  - name: ${USER}
    uid: $(id -u)
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - ${pubkey}
mounts:
${mount_lines}  - [ "tmpfs", "/tmp", "tmpfs", "size=8G,mode=1777", "0", "0" ]
runcmd:
  - setenforce 0 || true
  # Parallel Go test linking over the virtiofs share exhausts the
  # guest's default system-wide file table (ENFILE).
  - sysctl -w fs.file-max=2097152 || true
${mkdir_lines}  - [ chown, "${USER}:", "/home/${USER}" ]
  - systemctl enable --now sshd || systemctl enable --now ssh
EOF
genisoimage -quiet -output "$work/seed.iso" -volid cidata -joliet -rock \
    "$work/user-data" "$work/meta-data"

# Boot plumbing. vhost-user-fs needs shared guest memory (memfd + numa).
serial=(-serial null)
[[ -n "$SERIAL_LOG" ]] && serial=(-serial "file:$SERIAL_LOG")

ssh_opts=(-p "$SSH_PORT" -i "$work/id"
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
    -o LogLevel=ERROR -o ConnectTimeout=5 -o IdentitiesOnly=yes)
# shellcheck disable=SC2029  # arguments are meant to run in the guest
guest() { ssh "${ssh_opts[@]}" "${USER}@127.0.0.1" "$@"; }
fail() { echo "$1" >&2; [[ -n "$SERIAL_LOG" ]] && { echo "--- serial (tail) ---" >&2; tail -30 "$SERIAL_LOG" >&2; }; exit 1; }

start_vm() { # $1: boot disk (qcow2)
    start_virtiofsd
    "$qemu_bin" \
        "${arch_args[@]}" \
        -smp "$VM_CPUS" -m "$VM_MEMORY" \
        -object "memory-backend-memfd,id=mem,size=$VM_MEMORY,share=on" \
        -numa node,memdev=mem \
        "${qemu_fs_args[@]}" \
        -drive "file=$1,if=virtio,format=qcow2" \
        -drive "file=$work/seed.iso,if=virtio,media=cdrom,format=raw" \
        -netdev "user,id=net0,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22" \
        -device virtio-net-pci,netdev=net0 \
        -device virtio-rng-pci \
        -display none -nographic "${serial[@]}" &
    qemu_pid=$!
}

wait_ready() {
    echo "==> waiting for guest ssh (up to ${BOOT_TIMEOUT}s)"
    local deadline=$((SECONDS + BOOT_TIMEOUT))
    until guest true 2>/dev/null; do
        kill -0 "$qemu_pid" 2>/dev/null || fail "qemu exited before ssh came up"
        [[ "$SECONDS" -lt "$deadline" ]] || fail "timed out waiting for guest ssh"
        sleep 3
    done
    # sshd may come up before cloud-init's runcmd (setenforce, mkdirs)
    # has run; wait for cloud-init so the guest is fully configured.
    echo "==> waiting for cloud-init to finish"
    guest 'cloud-init status --wait >/dev/null 2>&1' || true
}

stop_vm() {
    guest 'sudo poweroff' 2>/dev/null || true
    for _ in $(seq 1 30); do kill -0 "$qemu_pid" 2>/dev/null || break; sleep 1; done
    kill "$qemu_pid" 2>/dev/null || true
    qemu_pid=
}

# One-off provisioning: on a cache miss, boot the pristine base, run the
# command, power off, and keep the disk. Later runs overlay off the
# cached provisioned disk and skip provisioning entirely.
boot_base=$base_image
if [[ -n "$provision_cmd" ]]; then
    if [[ -f "$provision_cmd" ]]; then
        prov_key=$(sha256sum < "$provision_cmd" | cut -c1-12)
    else
        prov_key=$(printf '%s' "$provision_cmd" | sha256sum | cut -c1-12)
    fi
    provisioned="$VM_CACHE_DIR/$(basename "$base_image" .qcow2)-prov-$prov_key.qcow2"
    if [[ ! -f "$provisioned" ]]; then
        echo "==> provisioning (one-off): ${provision_cmd}"
        prov_disk="$work/provision.qcow2"
        qemu-img create -q -f qcow2 -F qcow2 -b "$base_image" "$prov_disk" >/dev/null
        qemu-img resize -q "$prov_disk" 20G >/dev/null
        start_vm "$prov_disk"
        wait_ready
        # shellcheck disable=SC2029  # workdir/provision_cmd expand into the guest command
        guest "cd $(printf '%q' "${workdir:-/}") && ${provision_cmd}" || fail "provisioning failed"
        stop_vm
        mkdir -p "$VM_CACHE_DIR"
        mv "$prov_disk" "$provisioned"
    fi
    boot_base=$provisioned

    # With --provision and no --run, preparing the cached disk is the
    # job; exit rather than dropping into an interactive shell.
    if [[ -z "$run_cmd" ]]; then
        echo "==> provisioned disk ready: ${provisioned}"
        exit 0
    fi
fi

# Fresh per-run qcow2 overlay; grow it for build room (a provisioned
# disk is already grown).
overlay="$work/disk.qcow2"
qemu-img create -q -f qcow2 -F qcow2 -b "$boot_base" "$overlay" >/dev/null
[[ "$boot_base" == "$base_image" ]] && qemu-img resize -q "$overlay" 20G >/dev/null

start_vm "$overlay"
wait_ready

if [[ -z "$run_cmd" ]]; then
    echo "==> interactive shell (workdir ${workdir:-/}); 'sudo poweroff' to exit"
    ssh -t "${ssh_opts[@]}" "${USER}@127.0.0.1" "cd $(printf '%q' "${workdir:-/}") 2>/dev/null; exec bash -l"
    exit $?
fi

echo "==> running in guest (${workdir:-/}): ${run_cmd}"
status=0
# shellcheck disable=SC2029  # workdir/run_cmd are meant to expand into the guest command
guest "cd $(printf '%q' "${workdir:-/}") && ${run_cmd}" || status=$?
echo "==> command exit status: ${status}; powering off guest"
stop_vm
exit "$status"
