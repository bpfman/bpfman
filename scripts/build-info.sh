#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Copyright Authors of bpfman
#
# Generate the BPFMAN_BUILD_INFO string for embedding in binaries.
# Usage: eval "$(scripts/build-info.sh)"
#    or: export BPFMAN_BUILD_INFO="$(scripts/build-info.sh --value)"

set -e

version=$(cargo metadata --no-deps --format-version 1 2>/dev/null | \
    sed -n 's/.*"workspace_default_members":\["\([^#]*\)#\([^"]*\)".*/\2/p')
if [ -z "$version" ]; then
    version="unknown"
fi

git_version=$(git describe --tags --always --dirty 2>/dev/null || \
    git rev-parse --short=10 HEAD 2>/dev/null || true)

git_origin=$(git config --get remote.origin.url 2>/dev/null || true)

timestamp=${BPFMAN_BUILD_TIMESTAMP:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}

rustc_version=$(rustc --version)

info="${version} ("
if [ -n "$git_version" ]; then
    info="${info}${git_version} "
fi
if [ -n "$git_origin" ]; then
    info="${info}${git_origin} "
fi
info="${info}${timestamp}) ${rustc_version}"

case "${1:-}" in
    --value)
        printf '%s' "$info"
        ;;
    *)
        printf 'export BPFMAN_BUILD_INFO="%s"\n' "$info"
        ;;
esac
