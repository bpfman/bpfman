#!/usr/bin/env bash

# Sync project version from Cargo.toml to all OpenShift
# Containerfiles.

set -e

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

cargo_toml="${repo_root}/Cargo.toml"

containerfiles=(
    "${repo_root}/Containerfile.bpfman.openshift"
)

if [ ! -f "$cargo_toml" ]; then
    echo "Error: Cargo.toml file not found at $cargo_toml" >&2
    exit 1
fi

# Extract version from Cargo.toml's [workspace.package] section. The
# version line appears after [workspace.package] and before the next
# section
version=$(awk '
    /^\[workspace\.package\]/ { in_section=1; next }
    /^\[/ && in_section { exit }
    in_section && /^version = / {
        gsub(/version = "|"/, "")
        print
        exit
    }
' "$cargo_toml")

if [ -z "$version" ]; then
    echo "Error: Could not extract version from $cargo_toml" >&2
    exit 1
fi

echo "Target VERSION: $version"

any_updated=false

for containerfile in "${containerfiles[@]}"; do
    if [ ! -f "$containerfile" ]; then
        echo "Warning: Containerfile not found at $containerfile, skipping..." >&2
        continue
    fi

    current_version=$(grep -oP 'version="\K[^"]+' "$containerfile" | head -1)

    if [ -z "$current_version" ]; then
        echo "Error: No version label found in $containerfile" >&2
        exit 1
    fi

    if [ "$current_version" = "$version" ]; then
        echo "[OK] $(basename "$containerfile"): already in sync ($version)"
    else
        echo "[UPDATE] $(basename "$containerfile"): updating from $current_version to $version"

        sed -i "s/version=\"[^\"]*\"/version=\"$version\"/" "$containerfile"

        if grep -q 'release="' "$containerfile"; then
            sed -i "s/release=\"[^\"]*\"/release=\"$version\"/" "$containerfile"
        fi

        any_updated=true
    fi
done

if [ "$any_updated" = true ]; then
    echo ""
    echo "Updated Containerfiles with VERSION=$version"

    if command -v git &> /dev/null; then
        echo ""
        echo "Changes made:"
        git diff --stat "${containerfiles[@]}"
    fi
else
    echo ""
    echo "All Containerfiles already in sync with VERSION=$version"
fi
