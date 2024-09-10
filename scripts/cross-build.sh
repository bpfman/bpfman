#!/usr/bin/env bash

set -eu

host_arch=$(uname -m)

# Map common architecture names to their corresponding Rust target
# format.
arch_map() {
    case "$1" in
        x86_64) echo "x86_64-unknown-linux-gnu" ;;
        aarch64) echo "aarch64-unknown-linux-gnu" ;;
        ppc64le) echo "powerpc64le-unknown-linux-gnu" ;;
        s390x) echo "s390x-unknown-linux-gnu" ;;
        *) echo "Unsupported architecture: $1" && exit 1 ;;
    esac
}

# Specify the target architecture and corresponding Rust target.
target_arch=$1
if [ -z "$target_arch" ]; then
    echo "Usage: $0 <target-architecture>"
    echo "Supported architectures: amd64, aarch64, ppc64le, s390x"
    exit 1
fi

# Get Rust targets based on the architectures.
host_rust_target=$(arch_map "$host_arch")
rust_target=$(arch_map "$target_arch")

gcc_target="${target_arch}-linux-gnu"
cc="${gcc_target}-gcc"
linker="${gcc_target}-gcc"

sysroot="/usr/${gcc_target}"
lib_dir="/usr/lib/${gcc_target}"

# Debian uses 'arm64' for package management even though the Rust
# target and toolchain is 'aarch64'.
debian_arch="$target_arch"
if [ "$target_arch" = "aarch64" ]; then
    debian_arch="arm64"  # Use arm64 for Debian package management
fi

# Set the appropriate version of libssl-dev depending on the
# architecture.
libssl_dev="libssl-dev"
if [ "$host_arch" != "$target_arch" ]; then
    libssl_dev="libssl-dev:${debian_arch}"
fi

echo "Setting up cross-compilation environment for $target_arch"

# Add foreign architectures only if we're cross-compiling.
if [ "$host_arch" != "$target_arch" ]; then
    ${SUDO:-} dpkg --add-architecture "$debian_arch"
fi

# Update package lists. (Required if an architecture has been added.)
${SUDO:-} apt-get update

# Install required dependencies for all targets.
${SUDO:-} apt-get install -y \
     clang \
     cmake \
     direnv \
     gcc-multilib \
     git \
     libelf-dev \
     libssl-dev \
     llvm \
     perl \
     pkg-config \
     protobuf-compiler

# Install cross-compilation toolchains and OpenSSL for the target
# architecture.
${SUDO:-} apt-get install -y gcc-${gcc_target} "$libssl_dev"

if [ "$host_arch" != "$target_arch" ]; then
    # Correct the paths for pkg-config and to find OpenSSL.
    export PKG_CONFIG_SYSROOT_DIR="/usr/${gcc_target}"
    export PKG_CONFIG_PATH="/usr/lib/${gcc_target}/pkgconfig:${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
    echo "Configured pkg-config and OpenSSL for $target_arch"
fi

# Install Rust target for the specific architecture.
rustup target add "$rust_target"

# Set RUSTFLAGS to use the correct cross-linker if cross-compiling.
if [ "$host_arch" != "$target_arch" ]; then
    export RUSTFLAGS="-C linker=$linker ${RUSTFLAGS:-}"
fi

export RUSTFLAGS="${RUSTFLAGS:-} -C target-feature=-crt-static"

# Build the project using cargo for the specified target.
echo "Building bpfman for $target_arch using Rust target $rust_target..."
OPENSSL_STATIC=0 CC=$cc cargo build ${CARGO_RELEASE:-} --target "$rust_target"

# Output the result
echo "Build complete for $target_arch"
