#!/usr/bin/env bash
#
# Manual formatter fixture (bash): load and attach one program of each
# attachable kind against the real bin/bpfman, then print the rendered
# `-o text` output of program/link get and the lists so the formatting
# can be eyeballed by hand.
#
# Unlike the bpfman-shell version this is a plain shell script: every
# command's output prints, nothing is captured-and-swallowed, and ids are
# threaded between load/attach and get via `-o json | jq`.
#
# Run from the repository root:
#
#   sudo hack/program-load.sh
#
# The fixture runs against its own runtime root (/run/go-bpfman via
# BPFMAN_RUNTIME_DIR) so it never shares /run/bpfman with a daemon, the
# e2e suite, or the Rust implementation (whose CLI mounts a fresh bpffs
# over a runtime root it does not recognise, shadowing existing pins).
#
# This intentionally does not detach links or unload programs. Clean up
# after inspection with:
#
#   sudo bin/bpfman-e2e-cleanup --runtime-dir /run/go-bpfman
#   sudo ip link del bpfmanfmt0
#
# Note: the kprobe/kretprobe/fentry/fexit cases target do_unlinkat, which
# is inlined away on some kernels/arches; the script aborts at the first
# such failure on those hosts.

set -euo pipefail

export BPFMAN_RUNTIME_DIR=${BPFMAN_RUNTIME_DIR:-/run/go-bpfman}

BPFMAN=./bin/bpfman
HOST_LINK=bpfmanfmt0
PEER_LINK=bpfmanfmt1
TESTDATA=e2e/testdata/bpf

prog_id() { jq -r '.programs[0].record.program_id'; }
link_id() { jq -r '.record.id'; }

# A host veth to attach the xdp/tc/tcx programs to. Both ends are deleted
# by `ip link del bpfmanfmt0`.
if ! ip link show "$HOST_LINK" >/dev/null 2>&1; then
	ip link add "$HOST_LINK" type veth peer name "$PEER_LINK"
	ip link set "$HOST_LINK" up
fi

# A stable uprobe target: libc's malloc, resolved from a system binary.
libc=$(ldd "$(command -v cat)" 2>/dev/null | awk '/libc\.so/ {print $3; exit}')
if [[ -z "${libc:-}" ]]; then
	echo "could not resolve libc path for the uprobe target" >&2
	exit 1
fi

# --- load + attach one program of each kind ---

# The xdp program alone carries an application label so the program
# list renders both a populated APPLICATION cell and empty ones.
xdp_id=$("$BPFMAN" program load file "$TESTDATA/xdp_pass.bpf.o" --programs xdp:pass --application demo-app -m fixture=program-load -o json | prog_id)
xdp_link_id=$("$BPFMAN" link attach xdp "$xdp_id" "$HOST_LINK" --priority 50 -m fixture=program-load -m kind=xdp -o json | link_id)

tc_id=$("$BPFMAN" program load file "$TESTDATA/tc_counter.bpf.o" --programs tc:stats -m fixture=program-load -o json | prog_id)
tc_link_id=$("$BPFMAN" link attach tc "$tc_id" "$HOST_LINK" ingress --priority 60 --proceed-on ok,shot,dispatcher_return -m fixture=program-load -m kind=tc -o json | link_id)

tcx_id=$("$BPFMAN" program load file "$TESTDATA/tcx_counter.bpf.o" --programs tcx:tcx_stats -m fixture=program-load -o json | prog_id)
tcx_link_id=$("$BPFMAN" link attach tcx "$tcx_id" "$HOST_LINK" ingress --priority 70 -m fixture=program-load -m kind=tcx -o json | link_id)

tracepoint_id=$("$BPFMAN" program load file "$TESTDATA/tracepoint_counter.bpf.o" --programs tracepoint:tracepoint_kill_recorder -m fixture=program-load -o json | prog_id)
tracepoint_link_id=$("$BPFMAN" link attach tracepoint "$tracepoint_id" syscalls/sys_enter_kill -m fixture=program-load -m kind=tracepoint -o json | link_id)

kprobe_id=$("$BPFMAN" program load file "$TESTDATA/kprobe_counter.bpf.o" --programs kprobe:kprobe_counter -m fixture=program-load -o json | prog_id)
kprobe_link_id=$("$BPFMAN" link attach kprobe "$kprobe_id" do_unlinkat -m fixture=program-load -m kind=kprobe -o json | link_id)

kretprobe_id=$("$BPFMAN" program load file "$TESTDATA/kprobe_counter.bpf.o" --programs kretprobe:kprobe_counter -m fixture=program-load -o json | prog_id)
kretprobe_link_id=$("$BPFMAN" link attach kprobe "$kretprobe_id" do_unlinkat -m fixture=program-load -m kind=kretprobe -o json | link_id)

uprobe_id=$("$BPFMAN" program load file "$TESTDATA/uprobe_exact.bpf.o" --programs uprobe:uprobe_counter -m fixture=program-load -o json | prog_id)
uprobe_link_id=$("$BPFMAN" link attach uprobe "$uprobe_id" "$libc" --fn-name malloc -m fixture=program-load -m kind=uprobe -o json | link_id)

uretprobe_id=$("$BPFMAN" program load file "$TESTDATA/uprobe_exact.bpf.o" --programs uretprobe:uprobe_counter -m fixture=program-load -o json | prog_id)
uretprobe_link_id=$("$BPFMAN" link attach uprobe "$uretprobe_id" "$libc" --fn-name malloc -m fixture=program-load -m kind=uretprobe -o json | link_id)

fentry_id=$("$BPFMAN" program load file "$TESTDATA/fentry_exact.bpf.o" --programs fentry:test_fentry:do_unlinkat -m fixture=program-load -o json | prog_id)
fentry_link_id=$("$BPFMAN" link attach fentry "$fentry_id" -m fixture=program-load -m kind=fentry -o json | link_id)

fexit_id=$("$BPFMAN" program load file "$TESTDATA/fexit_exact.bpf.o" --programs fexit:test_fexit:do_unlinkat -m fixture=program-load -o json | prog_id)
fexit_link_id=$("$BPFMAN" link attach fexit "$fexit_id" -m fixture=program-load -m kind=fexit -o json | link_id)

# --- multi-link examples ---
#
# Attach the xdp and tracepoint programs a second time so the lists
# show a program with more than one link: the xdp program twice on the
# same interface (a two-member dispatcher chain, pos-0 and pos-1) and
# the tracepoint program on a second tracepoint.

_=$("$BPFMAN" link attach xdp "$xdp_id" "$HOST_LINK" --priority 55 --proceed-on pass,drop -m fixture=program-load -m kind=xdp -o json | link_id)
_=$("$BPFMAN" link attach tracepoint "$tracepoint_id" syscalls/sys_exit_kill -m fixture=program-load -m kind=tracepoint -o json | link_id)

# --- image-loaded examples ---
#
# Load two programs from the published bytecode images (the same ones
# the e2e corpus exercises) so the lists and get views show file- and
# image-sourced programs side by side. Requires network access to
# quay.io on the first run; IfNotPresent reuses the cached image after
# that.

image_xdp_id=$("$BPFMAN" program load image quay.io/bpfman-bytecode/go-xdp-counter --programs xdp:xdp_stats --pull-policy IfNotPresent -m fixture=program-load -o json | prog_id)
image_xdp_link_id=$("$BPFMAN" link attach xdp "$image_xdp_id" "$HOST_LINK" --priority 80 -m fixture=program-load -m kind=xdp-image -o json | link_id)

image_tracepoint_id=$("$BPFMAN" program load image quay.io/bpfman-bytecode/go-tracepoint-counter --programs tracepoint:tracepoint_kill_recorder --pull-policy IfNotPresent -m fixture=program-load -o json | prog_id)
image_tracepoint_link_id=$("$BPFMAN" link attach tracepoint "$image_tracepoint_id" syscalls/sys_enter_kill -m fixture=program-load -m kind=tracepoint-image -o json | link_id)

# --- map-sharing example ---
#
# Load a second copy of the kprobe counter that borrows the first
# one's maps via --map-owner-id, so the get views show a multi-member
# Maps Used By on both the owner and the borrower, and the borrower a
# populated Map Owner ID.

mapshare_id=$("$BPFMAN" program load file "$TESTDATA/kprobe_counter.bpf.o" --programs kprobe:kprobe_counter --map-owner-id "$kprobe_id" -m fixture=program-load -o json | prog_id)
mapshare_link_id=$("$BPFMAN" link attach kprobe "$mapshare_id" do_unlinkat -m fixture=program-load -m kind=kprobe-mapshare -o json | link_id)

# --- flag-variation examples ---
#
# Exercise the less-travelled load and attach flags so the get views
# render every optional field: a uprobe with global-data overrides and
# a PID filter, and an xdp attach through a named network namespace,
# which also populates the dispatcher list's netns columns. The peer
# end of the veth pair moves into the namespace; clean up with:
#
#   sudo ip netns del bpfmanfmt-ns
NETNS_NAME=bpfmanfmt-ns

if ! ip netns list 2>/dev/null | grep -q "^$NETNS_NAME"; then
	ip netns add "$NETNS_NAME"
	ip link set "$PEER_LINK" netns "$NETNS_NAME"
	ip -n "$NETNS_NAME" link set "$PEER_LINK" up
fi

netns_xdp_link_id=$("$BPFMAN" link attach xdp "$xdp_id" "$PEER_LINK" --priority 50 --netns "/var/run/netns/$NETNS_NAME" -m fixture=program-load -m kind=xdp-netns -o json | link_id)

uprobe_flags_id=$("$BPFMAN" program load file "$TESTDATA/uprobe_exact.bpf.o" --programs uprobe:uprobe_counter -g expected_pid=0x00000000 -g weight=0x0100000000000000 -m fixture=program-load -o json | prog_id)
uprobe_flags_link_id=$("$BPFMAN" link attach uprobe "$uprobe_flags_id" "$libc" --fn-name malloc --pid $$ -m fixture=program-load -m kind=uprobe-flags -o json | link_id)

# --- show the rendered output ---

show() {
	local kind=$1 pid=$2 lid=$3
	echo
	echo "=== $kind ==="
	"$BPFMAN" program get "$pid"
	"$BPFMAN" link get "$lid"
}

show xdp        "$xdp_id"        "$xdp_link_id"
show tc         "$tc_id"         "$tc_link_id"
show tcx        "$tcx_id"        "$tcx_link_id"
show tracepoint "$tracepoint_id" "$tracepoint_link_id"
show kprobe     "$kprobe_id"     "$kprobe_link_id"
show kretprobe  "$kretprobe_id"  "$kretprobe_link_id"
show uprobe     "$uprobe_id"     "$uprobe_link_id"
show uretprobe  "$uretprobe_id"  "$uretprobe_link_id"
show fentry     "$fentry_id"     "$fentry_link_id"
show fexit      "$fexit_id"      "$fexit_link_id"
show xdp-image        "$image_xdp_id"        "$image_xdp_link_id"
show tracepoint-image "$image_tracepoint_id" "$image_tracepoint_link_id"
show kprobe-mapshare  "$mapshare_id"         "$mapshare_link_id"
show xdp-netns        "$xdp_id"              "$netns_xdp_link_id"
show uprobe-flags     "$uprobe_flags_id"     "$uprobe_flags_link_id"

echo
echo "=== program list ==="
"$BPFMAN" program list

echo
echo "=== link list ==="
"$BPFMAN" link list

echo
echo "=== dispatcher list ==="
"$BPFMAN" dispatcher list
