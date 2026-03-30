// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

fn main() {
    println!("cargo:rerun-if-changed=src");
    println!("cargo:rerun-if-changed=build.rs");
    println!("cargo:rerun-if-changed=Cargo.toml");
    println!("cargo:rerun-if-env-changed=BPFMAN_BUILD_INFO");
    println!(
        "cargo:rustc-env=BPFMAN_BUILD_INFO={}",
        std::env::var("BPFMAN_BUILD_INFO")
            .unwrap_or_else(|_| env!("CARGO_PKG_VERSION").to_string())
    );
}
