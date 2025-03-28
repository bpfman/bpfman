// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

fn main() {
    println!("cargo:rerun-if-changed=migrations");
    println!("cargo:rerun-if-changed=diesel.toml");
}
