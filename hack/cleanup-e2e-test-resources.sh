#!/usr/bin/env bash
#
# Emit ip(8) / tc(8) commands that drain dispatcher residue and
# delete interfaces and netns left by an interrupted e2e run. The
# scope is the harness's own naming: test netns named `B<hex>N`
# (exactly 12 hex digits, matching e2e/testnet.uniqueTestName) or
# `B<hex>Na` / `B<hex>Nb` (the isolated `net netns-veth-pair`
# builder owns one namespace per veth end), and host-side
# interfaces matching the same pattern with an optional `a` / `b`
# peer suffix. Bare-`N` host ifaces are dummies created by
# testnet.NewTestInterface; `Na` / `Nb` ends are veth pairs
# created by NewTestVethPair or by a script's `net veth-pair`
# builtin.
#
# The script does not mutate anything itself. It writes the commands
# it would run to stdout so you can read them before deciding to
# execute. Audit, then pipe to `sudo sh`:
#
#   sudo hack/cleanup-e2e-test-resources.sh             # audit
#   sudo hack/cleanup-e2e-test-resources.sh | sudo sh   # execute
#
# Relationship to cleanup-all-dispatchers.sh:
#   - This script is shotgun within the test namespace: anything
#     attached to a `B<hex>N` interface is detached and the
#     interface deleted. Enough on its own for ordinary e2e
#     residue.
#   - cleanup-all-dispatchers.sh is name-filtered (only programs
#     literally named xdp_dispatcher / tc_dispatcher) and walks
#     every netns. Use it when residue can be outside the e2e
#     namespace -- e.g. a leaked dispatcher on lo, on a production
#     NIC, or in a non-test netns.
#
# Two-step usage when both kinds of residue may be present. Run
# cleanup-all-dispatchers.sh first because it only drains; this
# script then drains again (no-op) and deletes the test interfaces
# and netns. Reversing the order would mean the all-dispatchers
# pass finds nothing on the test interfaces because they have
# already been deleted, which is harmless but pointless:
#
#   sudo hack/cleanup-all-dispatchers.sh           # audit (first)
#   sudo hack/cleanup-e2e-test-resources.sh        # audit (second)
#   { sudo hack/cleanup-all-dispatchers.sh; \
#     sudo hack/cleanup-e2e-test-resources.sh; } | sudo sh   # execute
#
# Order of the emitted command stream within this script is also
# load-bearing:
#
#   1. Drain XDP and clsact off every interface in each test netns.
#   2. Drain XDP and clsact off every host-side test interface.
#   3. Delete host-side test interfaces (deleting the host end
#      of a veth cascades the peer wherever it lives).
#   4. Delete the test netns.
#
# XDP drains go through the bpf_link API: bpfman pins its links
# under /sys/fs/bpf, so `ip link set ... xdp off` cannot evict
# them (the kernel rejects with "Can't replace active BPF XDP
# link"). Instead this script joins `bpftool -f link show -j` per
# netns and emits `rm <pin>` (drop the last reference so the
# kernel detaches and GCs the link) or `bpftool link detach id N`
# for unpinned links. Doing this before phase 3/4 is what stops
# the kernel from leaving "detached" link objects pinned in the
# bpf fs after the netdev or netns disappears.
#
# Each emitted command is independent -- a flat list, no `set -e`.
# A failure when the pipeline executes (e.g. "no such qdisc") does
# not abort the rest, which matches the harness's idempotent intent.
#
# Idempotent: re-running emits nothing once the test namespace has
# no matching state left.

set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
    echo "error: must be run as root (sudo $0)" >&2
    exit 1
fi

# ifaces_in lists every non-loopback interface in a netns. The netns
# argument is empty for the host; an explicit name routes the lookup
# through `ip -n`. The `: ` split lifts the second colon-delimited
# field (the bare ifname, possibly `name@peer-ifindex`); the `@`
# split trims the peer-index suffix back off.
ifaces_in() {
    local ns="${1:-}"
    local ip_cmd=(ip)
    if [[ -n "$ns" ]]; then
        ip_cmd=(ip -n "$ns")
    fi
    "${ip_cmd[@]}" -o link show \
        | awk -F': ' '{print $2}' \
        | awk -F'@' '{print $1}' \
        | grep -vx 'lo' || true
}

# emit_xdp_link_drains_in_ns emits `rm <pin>` (preferred) or
# `bpftool link detach id N` (fallback) for every XDP bpf_link
# attached in the given netns whose devname matches the supplied
# bash regex. The netns argument is empty for the host; a name
# routes the bpftool lookups and the emitted detach command
# through `ip netns exec NS`.
emit_xdp_link_drains_in_ns() {
    local ns="${1:-}"
    local iface_re="${2:-}"
    local nsenter=()
    local prefix=''
    if [[ -n "$ns" ]]; then
        nsenter=(ip netns exec "$ns")
        prefix="ip netns exec $ns "
    fi
    local link_json
    link_json=$("${nsenter[@]}" bpftool -f link show -j 2>/dev/null || echo '[]')
    if [[ -z "$link_json" || "$link_json" == '[]' ]]; then
        return
    fi
    local matches
    matches=$(echo "$link_json" | jq -c '.[] | select(.type=="xdp")')
    if [[ -z "$matches" ]]; then
        return
    fi
    while IFS= read -r entry; do
        [[ -z "$entry" ]] && continue
        local devname
        devname=$(echo "$entry" | jq -r '.devname // ""')
        if [[ -n "$iface_re" ]] && ! [[ "$devname" =~ $iface_re ]]; then
            continue
        fi
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

# Phase 1: drain attachments off every interface in each test netns.
# Both the host-end builder's bare-`N` netns and the isolated
# builder's `Na` / `Nb` pair are in scope; every iface inside is
# ours, so no name filter is needed for the XDP drain. The tc qdisc
# del is per-iface as before and harmless when no clsact exists.
shopt -s nullglob
for path in /run/netns/B*; do
    ns=$(basename "$path")
    if [[ ! "$ns" =~ ^B[0-9a-f]{12}N[ab]?$ ]]; then
        continue
    fi
    emit_xdp_link_drains_in_ns "$ns" ""
    while read -r iface; do
        printf 'ip netns exec %s tc qdisc del dev %s clsact\n' "$ns" "$iface"
    done < <(ifaces_in "$ns")
done

# Phase 2: drain attachments off every host-side test interface.
# Both bare-`N` dummies and `Na` / `Nb` veth ends are in scope.
emit_xdp_link_drains_in_ns "" '^B[0-9a-f]{12}N[ab]?$'
while read -r iface; do
    if [[ "$iface" =~ ^B[0-9a-f]{12}N[ab]?$ ]]; then
        printf 'tc qdisc del dev %s clsact\n' "$iface"
    fi
done < <(ifaces_in "")

# Phase 3: delete host-side test interfaces. The same enumeration
# and pattern as phase 2; deleting the host end of a veth pair
# cascades the peer wherever it lives.
while read -r iface; do
    if [[ "$iface" =~ ^B[0-9a-f]{12}N[ab]?$ ]]; then
        printf 'ip link del %s\n' "$iface"
    fi
done < <(ifaces_in "")

# Phase 4: delete the test netns themselves. Deleting an isolated
# pair's `Na` / `Nb` namespaces cascades the veth ends inside them.
for path in /run/netns/B*; do
    name=$(basename "$path")
    if [[ ${name} =~ ^B[0-9a-f]{12}N[ab]?$ ]]; then
        printf 'ip netns del %s\n' "$name"
    fi
done
