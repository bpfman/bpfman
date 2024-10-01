#!/usr/bin/env bash

# Synopsis of the Script
#
# This script sets up an environment for cross-compiling Rust projects
# targeting multiple architectures, including `amd64`, `aarch64`,
# `ppc64le`, and `s390x`. It handles the installation of the
# appropriate toolchains, system libraries, and Rust targets to enable
# both native and cross-compilation. The script automates tasks, such
# as managing different architecture names across multiple tools and
# packages (Debian, Rust, GCC), ensuring dynamic linking, and
# correctly configuring build settings based on the target
# architecture.
#
# Why We Need Multiple Maps
#
# Different tools and environments use varying conventions to identify
# architectures. To ensure smooth cross-compilation, the script uses
# several maps to translate architecture names and select the correct
# toolchain components. Here's why each map is needed:
#
# 1. Architecture Alias Map (`arch_alias_map`)
#
#    - Purpose: Translates user-friendly or alternative architecture
#      names (e.g., `ppc64le`, `amd64`) into the standard architecture
#      names expected by the system and tools like Debian, Rust, and
#      GCC.
#
#    - Why it's needed: Different tools and environments (such as
#      Debian, Rust, and GCC) often use different names for the same
#      architecture. For example, Rust uses `aarch64` while Debian
#      uses `arm64`. This map ensures that architecture inputs are
#      correctly translated and standardised across the script, making
#      it compatible with the tools in use.
#
# 2. Rust Target Map (`rust_target_map`)
#
#    - Purpose: Maps canonical architectures to Rust's target triple
#      format (e.g., `x86_64-unknown-linux-gnu`).
#
#    - Why it's needed: Rust requires a specific target triple to
#      build for non-native architectures. This map allows the script
#      to select the correct target based on the architecture.
#
# 3. GCC Toolchain Map (`gcc_toolchain_map`)
#
#    - Purpose: Maps canonical architectures to their corresponding
#      GCC toolchain triplet (e.g., `x86_64-linux-gnu`).
#
#    - Why it's needed: GCC uses different triplet names for cross-
#      compilation toolchains. This map ensures that the correct
#      toolchain is used for each architecture when compiling C code
#      (e.g., using the `cc` linker in Rust builds).
#
# 4. GCC Package Map (`gcc_pkg_toolchain_map`)
#
#    - Purpose: Maps canonical architectures to the names of the
#      corresponding GCC toolchain packages in Debian (e.g., `gcc-
#      x86-64-linux-gnu`).
#
#    - Why it's needed: When setting up the environment, the script
#      needs to install the appropriate GCC toolchain for the target
#      architecture. This map provides the Debian package name for
#      each toolchain.
#
# 5. Debian Architecture Map (`debian_arch_map`)
#
#    - Purpose: Maps canonical architectures to their corresponding
#      Debian architecture name (e.g., `amd64`, `arm64`, `ppc64el`).
#
#    - Why it's needed: When managing libraries and foreign
#      architectures in Debian (such as with `libssl-dev`), the script
#      needs to know the correct architecture name for package
#      management. This map ensures that the proper architecture name
#      is used when installing cross-compiled libraries.
#
# Each map in the script is designed to bridge the gap between
# different naming conventions used by Rust, GCC, and Debian. Without
# these maps, it would be difficult to automate the process of
# selecting the correct toolchain, libraries, and build configuration
# for cross-compilation across multiple architectures. The maps ensure
# consistency and correctness in the complex environment of
# multi-architecture cross-compilation.
#
# Note About OpenSSL
#
# In this script, we ensure that OpenSSL is always dynamically linked,
# and we never build a binary that statically links OpenSSL. Static
# linking of OpenSSL can create potential security risks, as any
# vulnerability in OpenSSL would require rebuilding the binary to
# incorporate the security fix. By dynamically linking OpenSSL, the
# system’s shared libraries can be updated independently of the
# binary, ensuring that security patches are applied automatically.
#
# To enforce this, we:
#
# 1. Set `OPENSSL_STATIC=0`, which explicitly disables static linking
#    for OpenSSL.
#
# 2. Set `OPENSSL_NO_VENDOR=1` to avoid using a vendored version of
#    OpenSSL that may build statically. This ensures the build uses
#    the system-provided OpenSSL library instead.
#
# 3. Use `RUSTFLAGS="-C target-feature=-crt-static"` to further ensure
#    that the C runtime and libraries like OpenSSL are dynamically
#    linked.
#
# This guarantees that the resulting binaries are secure and can
# benefit from system-level OpenSSL updates without needing to rebuild
# or redeploy the binary.

set -o errexit
set -o nounset
set -o pipefail

host_arch=$(uname -m)

# Architecture alias map to translate alternative names (e.g., amd64)
# to standard architecture names.
declare -A arch_alias_map
arch_alias_map=(
    ["aarch64"]="arm64"
    ["amd64"]="x86_64"   # Map amd64 to x86_64 for compatibility.
    ["arm64"]="arm64"
    ["ppc64el"]="ppc64el"
    ["ppc64le"]="ppc64el"
    ["s390x"]="s390x"
    ["x86_64"]="x86_64"
)

# Rust target mappings.
declare -A rust_target_map
rust_target_map=(
    ["arm64"]="aarch64-unknown-linux-gnu"
    ["ppc64el"]="powerpc64le-unknown-linux-gnu"
    ["s390x"]="s390x-unknown-linux-gnu"
    ["x86_64"]="x86_64-unknown-linux-gnu"
)

# GCC toolchain mappings.
declare -A gcc_toolchain_map
gcc_toolchain_map=(
    ["arm64"]="aarch64-linux-gnu"
    ["ppc64el"]="powerpc64le-linux-gnu"
    ["s390x"]="s390x-linux-gnu"
    ["x86_64"]="x86_64-linux-gnu"
)

# GCC package mappings.
declare -A gcc_pkg_toolchain_map
gcc_pkg_toolchain_map=(
    ["arm64"]="gcc-aarch64-linux-gnu"
    ["ppc64el"]="gcc-powerpc64le-linux-gnu"
    ["s390x"]="gcc-s390x-linux-gnu"
    ["x86_64"]="gcc-x86-64-linux-gnu"
)

# Debian architecture for package management (if different from
# canonical).
declare -A debian_arch_map
debian_arch_map=(
    ["arm64"]="arm64"
    ["ppc64el"]="ppc64el"
    ["s390x"]="s390x"
    ["x86_64"]="amd64"
)

# Function to canonicalise the architecture input.
canonicalise_arch() {
    local arch_input="$1"
    if [[ -n "${arch_alias_map[$arch_input]+set}" ]]; then
        echo "${arch_alias_map[$arch_input]}"
    else
        echo "Unsupported architecture: $arch_input" >&2
        exit 1
    fi
}

# Dynamically set supported_archs to the list of keys from
# arch_alias_map. ${!arch_alias_map[@]} retrieves all the keys
# from the associative array arch_alias_map, which represent the
# architectures the script supports. This ensures supported_archs
# always stays in sync with arch_alias_map without manual
# duplication.
supported_archs=("${!arch_alias_map[@]}")

# Check if the target architecture is provided as an argument.
if [ $# -eq 0 ]; then
    echo "Usage: ${0##*/} <target-architecture> [build-type]" >&2
    echo "Optional build types: debug (default), release."
    echo "Supported architectures: ${supported_archs[*]}" >&2
    exit 1
fi

# Specify the target architecture.
target_arch_input=$1
shift

# Set default build type to debug if not provided and check if the
# second argument is specified.
build_type="debug"
if [ $# -gt 0 ]; then
    if [[ "$1" == "release" || "$1" == "debug" ]]; then
        build_type=$1
        shift
    fi
    # Anything that doesn't match is available in $@.
fi

if [[ ! " ${supported_archs[*]} " =~ ${target_arch_input} ]]; then
    echo "Error: Unsupported architecture: $target_arch_input."
    echo "Supported architectures: ${supported_archs[*]}."
    exit 1
fi

echo "Target architecture: $target_arch_input."
echo "Build type: $build_type."

# Canonicalise the architecture input.
target_arch=$(canonicalise_arch "$target_arch_input")

# Determine if we are cross-compiling.
cross_compiling=0
if [ "$(canonicalise_arch "$host_arch")" != "$target_arch" ]; then
    cross_compiling=1
fi

# Get Rust target based on the canonical architecture.
rust_target="${rust_target_map[$target_arch]}"

# Get GCC toolchain name based on the canonical architecture.
gcc_target="${gcc_toolchain_map[$target_arch]}"
cc="${gcc_target}-gcc"
linker="${gcc_target}-gcc"
sysroot="/usr/${gcc_target}"
lib_dir="/usr/lib/${gcc_target}"

# Get Debian architecture name for package management.
debian_arch="${debian_arch_map[$target_arch]}"

# Set the appropriate version of libssl-dev depending on the
# architecture.
libssl_dev="libssl-dev"
if [ $cross_compiling -eq 1 ]; then
    libssl_dev="libssl-dev:${debian_arch}"
fi

# Add foreign architectures only if we're cross-compiling.
if [ $cross_compiling -eq 1 ]; then
    ${SUDO:-} dpkg --add-architecture "$debian_arch"
fi

# Update package lists (required if an architecture has been added).
${SUDO:-} apt-get update

# Install required dependencies for all targets.
${SUDO:-} apt-get install -y \
          clang \
          cmake \
          direnv \
          git \
          libelf-dev \
          libssl-dev \
          llvm \
          perl \
          pkg-config \
          protobuf-compiler

# Install cross-compilation toolchains and OpenSSL for the target
# architecture.
if [ $cross_compiling -eq 1 ]; then
    # Cross-compiling: host and target architectures are different.
    ${SUDO:-} apt-get install -y "${gcc_pkg_toolchain_map[$target_arch]}" "$libssl_dev"
else
    # Native compilation: host and target architectures are the same.
    ${SUDO:-} apt-get install -y gcc "$libssl_dev"
fi

# Check if the compiler and linker exist.
if ! type -P "$cc" >/dev/null; then
    echo "Error: GCC compiler ($cc) not found. Please ensure the toolchain is installed." >&2
    exit 1
fi

if ! type -P "$linker" >/dev/null; then
    echo "Error: Linker ($linker) not found. Please ensure the toolchain is installed." >&2
    exit 1
fi

if [ $cross_compiling -eq 1 ]; then
    # Correct the paths for pkg-config and to find OpenSSL.
    export PKG_CONFIG_SYSROOT_DIR="/usr/${gcc_target}"
    export PKG_CONFIG_PATH="/usr/lib/${gcc_target}/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
fi

# Install Rust target for the specific architecture.
rustup target add "$rust_target"

# Set RUSTFLAGS to use the correct cross-linker if cross-compiling.
if [ $cross_compiling -eq 1 ]; then
    export RUSTFLAGS="-C linker=$linker${RUSTFLAGS:+ $RUSTFLAGS}"
fi

# Ensure dynamic linking of OpenSSL and prevent static linking of CRT.
export OPENSSL_NO_VENDOR=1
export OPENSSL_STATIC=0
export RUSTFLAGS="${RUSTFLAGS:+$RUSTFLAGS }-C target-feature=-crt-static"

echo "Building bpfman for $target_arch using:"
echo "  - C compiler (CC): $cc"
echo "  - Debian architecture: $debian_arch"
echo "  - GCC target: $gcc_target"
echo "  - Library directory: $lib_dir"
echo "  - Linker: $linker"
echo "  - PKG_CONFIG path: ${PKG_CONFIG_PATH:-<not set>}"
echo "  - PKG_CONFIG sysroot dir: ${PKG_CONFIG_SYSROOT_DIR:-<not set>}"
echo "  - Rust target: $rust_target"
echo "  - System root: $sysroot"

# Determine if we are doing a debug or release build.
if [ "$build_type" == "release" ]; then
    CARGO_BUILD_FLAG="--release"
else
    CARGO_BUILD_FLAG=""
fi

# Set CC only if we are cross-compiling.
if [ $cross_compiling -eq 1 ]; then
    CC=$cc
else
    unset CC  # Don't explicitly set CC if we're not cross-compiling.
fi

cargo build --target "$rust_target" $CARGO_BUILD_FLAG "$@"
