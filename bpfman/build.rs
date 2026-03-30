// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::process::Command;

const BPF_DIR: &str = "../bpf";

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

    // Always invoke make - let make handle incremental builds via its
    // dependency tracking.
    let out_dir = std::env::var("OUT_DIR").expect("OUT_DIR not set");
    let status = Command::new("make")
        .arg("-C")
        .arg(BPF_DIR)
        .arg(format!("OUT_DIR={out_dir}"))
        .status()
        .expect("Failed to execute make");

    if !status.success() {
        panic!("Failed to build dispatcher bytecode - required for hermetic builds");
    }

    println!("cargo:rerun-if-changed={BPF_DIR}");
}
