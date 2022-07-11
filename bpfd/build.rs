// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

fn main() -> anyhow::Result<()> {
    println!("cargo:rerun-if-changed=proto/bpfd.proto");
    tonic_build::compile_protos("proto/bpfd.proto")?;
    Ok(())
}
