#!/usr/bin/env bash
#
# Emit ip(8) / tc(8) / bpftool(8) commands that drain every bpfman
# dispatcher attached anywhere on the host. Walks the root netns
# plus every netns under /run/netns, looks up each interface's XDP
# and TC attachments via `bpftool net show`, and for every
# attachment whose backing program is `xdp_dispatcher` or
# `tc_dispatcher` emits the command that detaches it. The kernel
# then garbage-collects the program once its last reference drops,
# unless something else keeps it alive.
#
# XDP: bpfman attaches dispatchers via the bpf_link API and pins
# the link under /sys/fs/bpf, so `ip link set ... xdp off` is the
# wrong primitive -- the kernel rejects it with "Can't replace
# active BPF XDP link". The right move is to drop the last
# reference: `rm` the link pin (the kernel detaches and GCs the
# link), or `bpftool link detach id N` if the link is unpinned.
# This script joins `bpftool net show -j` with
# `bpftool -f link show -j` by prog_id to find the pin paths.
#
# TC: a single clsact qdisc carries both ingress and egress
# filters; `tc qdisc del ... clsact` cascades both. bpfman's TC
# attachment still goes through clsact, so the qdisc delete is
# enough.
#
# The script does not mutate anything itself. It writes the commands
# it would run to stdout so you can read them before deciding to
# execute. Audit, then pipe to `sudo sh`:
#
#   sudo hack/cleanup-all-dispatchers.sh             # audit
#   sudo hack/cleanup-all-dispatchers.sh | sudo sh   # execute
#
# When and why to use:
#   - You killed the bpfman daemon or wiped its DB, and the kernel
#     still carries dispatcher programs whose owning links you no
#     longer have any handle on. `bpfman dispatcher list` shows
#     nothing (the DB is empty) but `bpftool prog show | grep
#     dispatcher` still shows them. This script lets you drain them.
#   - You want a known-clean kernel state before re-running a test
#     suite, even if some residue is unrelated to the parallel-
#     scripts harness (e.g. left over from a manual
#     `bpfman link attach`).
#
# Relationship to cleanup-e2e-test-resources.sh:
#   - cleanup-e2e-test-resources.sh handles residue inside the e2e
#     harness's `B<hex>N` namespace: it drains XDP / clsact off
#     every interface in that scope unconditionally and then
#     deletes the interfaces and netns themselves. It is enough on
#     its own when all leaked attachments are on test interfaces
#     and you do not care about residue elsewhere.
#   - This script is name-filtered (only programs literally named
#     xdp_dispatcher / tc_dispatcher are drained) so it can safely
#     run on a host where other XDP / TC programs are also present.
#     The cost is that it depends on bpftool and on those exact
#     program names; a renamed dispatcher would be missed.
#
# Two-step usage when both kinds of residue may be present (e.g.
# the daemon was running against production interfaces before it
# was killed). Run this script first so it only drains, then the
# e2e script to finish the test-namespace cleanup. Doing the
# host-wide drain after the e2e script would find nothing to drain
# on the test interfaces because they would already be gone:
#
#   sudo hack/cleanup-all-dispatchers.sh           # audit (first)
#   sudo hack/cleanup-e2e-test-resources.sh        # audit (second)
#   { sudo hack/cleanup-all-dispatchers.sh; \
#     sudo hack/cleanup-e2e-test-resources.sh; } | sudo sh   # execute
#
# Idempotent: re-running emits nothing once no dispatcher
# attachments remain.

set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
    echo "error: must be run as root (sudo $0)" >&2
    exit 1
fi

for tool in bpftool jq ip tc; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "error: required tool '$tool' not found in PATH" >&2
        exit 1
    fi
done

# prog_name_is returns 0 when the program at the given id has the
# expected name, 1 otherwise. Used to distinguish a real dispatcher
# attachment from any other XDP / TC program that happens to be
# attached to the same interface.
prog_name_is() {
    local id="$1"
    local want="$2"
    local got
    got=$(bpftool prog show id "$id" -j 2>/dev/null | jq -r '.name // empty')
    [[ "$got" == "$want" ]]
}

# emit_xdp_link_drain emits the command(s) needed to evict every
# XDP bpf_link backed by the given prog_id. Prefers `rm` on the
# link's pin file (dropping the last reference makes the kernel
# detach and GC the link); falls back to `bpftool link detach id N`
# for an unpinned link. The first argument is an empty string for
# the root netns, otherwise `ip netns exec NS ` (note trailing
# space) so the emitted command runs inside that netns.
emit_xdp_link_drain() {
    local prefix="$1"
    local prog_id="$2"
    local link_json="$3"

    local matches
    matches=$(echo "$link_json" | jq -c --argjson pid "$prog_id" \
        '.[] | select(.type=="xdp" and .prog_id==$pid)')
    if [[ -z "$matches" ]]; then
        return
    fi

    while IFS= read -r entry; do
        [[ -z "$entry" ]] && continue
        local pins
        pins=$(echo "$entry" | jq -r '.pinned[]? // empty')
        if [[ -n "$pins" ]]; then
            while IFS= read -r pin; do
                [[ -z "$pin" ]] && continue
                printf 'rm -f -- %s\n' "$pin"
            done <<< "$pins"
            continue
        fi
        local link_id
        link_id=$(echo "$entry" | jq -r '.id')
        printf '%sbpftool link detach id %s\n' "$prefix" "$link_id"
    done <<< "$matches"
}

# emit_drains walks every interface in one netns and emits the
# command that detaches any bpfman dispatcher attached to it. The
# netns argument is empty for the root netns; an explicit name
# prefixes the emitted commands with `ip -n NS` / `ip netns exec
# NS tc`.
emit_drains() {
    local ns="${1:-}"
    local nsenter=()
    local link_prefix=''
    local tc_prefix='tc'
    if [[ -n "$ns" ]]; then
        nsenter=(ip netns exec "$ns")
        link_prefix="ip netns exec $ns "
        tc_prefix="ip netns exec $ns tc"
    fi

    local net_json
    net_json=$("${nsenter[@]}" bpftool net show -j 2>/dev/null || echo '[]')
    if [[ -z "$net_json" || "$net_json" == "[]" ]]; then
        return
    fi

    # bpftool -f scans the bpf fs so each link entry carries a
    # `pinned` array. We need that to know which file to remove to
    # drop the last reference on a dispatcher link.
    local link_json
    link_json=$("${nsenter[@]}" bpftool -f link show -j 2>/dev/null || echo '[]')

    # XDP entries carry the prog id but not the name; look the name
    # up per id so we only emit drains for interfaces whose XDP is
    # actually a bpfman dispatcher. The kernel attaches via the
    # bpf_link API, so for each matching prog we find its bpf_link
    # in link_json and either rm the pin file or detach by id.
    while read -r ifname id; do
        [[ -z "$ifname" || -z "$id" ]] && continue
        if ! prog_name_is "$id" "xdp_dispatcher"; then
            continue
        fi
        emit_xdp_link_drain "$link_prefix" "$id" "$link_json"
    done < <(echo "$net_json" | jq -r '.[0].xdp[]? | "\(.devname) \(.id)"')

    # TC entries already include the program name, so the name check
    # is local. A single clsact qdisc carries both ingress and egress
    # filters; removing it cascades both, so we deduplicate by
    # ifname to avoid a redundant second `tc qdisc del`.
    local seen=()
    while read -r ifname name; do
        [[ -z "$ifname" || -z "$name" ]] && continue
        if [[ "$name" != "tc_dispatcher" ]]; then
            continue
        fi
        local already=0
        for s in "${seen[@]:-}"; do
            if [[ "$s" == "$ifname" ]]; then
                already=1
                break
            fi
        done
        if [[ "$already" -eq 1 ]]; then
            continue
        fi
        seen+=("$ifname")
        printf '%s qdisc del dev %s clsact\n' "$tc_prefix" "$ifname"
    done < <(echo "$net_json" | jq -r '.[0].tc[]? | "\(.devname) \(.name)"')
}

emit_drains ""

shopt -s nullglob
for path in /run/netns/*; do
    emit_drains "$(basename "$path")"
done
