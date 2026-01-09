// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::process::Command;

const BPF_DIR: &str = "../bpf";

fn main() {
    buildinfo::generate_version_info();

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
