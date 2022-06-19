// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::{
    env,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    process::Command,
    string::String,
};

use anyhow::{bail, Context, Result};

fn main() -> Result<()> {
    println!("cargo:rerun-if-changed=proto/bpfd.proto");
    tonic_build::compile_protos("proto/bpfd.proto")?;

    let out_path = PathBuf::from(env::var("OUT_DIR")?);
    let include_path = out_path.join("include");
    fs::create_dir_all(&include_path)?;
    build_ebpf(&out_path, &include_path)?;
    Ok(())
}

/// Extract vendored libbpf headers from libbpf-sys.
fn extract_libbpf_headers<P: AsRef<Path>>(include_path: P) -> Result<()> {
    let dir = include_path.as_ref().join("bpf");
    fs::create_dir_all(&dir)?;
    for (filename, contents) in libbpf_sys::API_HEADERS.iter() {
        let path = dir.as_path().join(filename);
        let mut file = OpenOptions::new().write(true).create(true).open(path)?;
        file.write_all(contents.as_bytes())?;
    }

    Ok(())
}
/// Build eBPF programs with clang and libbpf headers.
fn build_ebpf<P: Clone + AsRef<Path>>(out_path: P, include_path: P) -> Result<()> {
    println!("cargo:rerun-if-changed=src/bpf/xdp_dispatcher.bpf.c");

    extract_libbpf_headers(&include_path)?;

    let bpf_dir = Path::new("src").join("bpf");
    let src = bpf_dir.join("xdp_dispatcher.bpf.c");

    let out = out_path.as_ref().join("xdp_dispatcher.bpf.o");

    let clang = match env::var("CLANG") {
        Ok(val) => val,
        Err(_) => String::from("/usr/bin/clang"),
    };
    let arch = match std::env::consts::ARCH {
        "x86_64" => "x86",
        "aarch64" => "arm64",
        _ => std::env::consts::ARCH,
    };
    let mut cmd = Command::new(clang);
    cmd.arg(format!("-I{}", include_path.as_ref().to_string_lossy()))
        .arg("-g")
        .arg("-O2")
        .arg("-target")
        .arg("bpf")
        .arg("-c")
        .arg(format!("-D__TARGET_ARCH_{}", arch))
        .arg(src.as_os_str())
        .arg("-o")
        .arg(out);

    let output = cmd.output().context("Failed to execute clang")?;
    if !output.status.success() {
        bail!(
            "Failed to compile eBPF programs\n \
            stdout=\n \
            {}\n \
            stderr=\n \
            {}\n",
            String::from_utf8(output.stdout).unwrap(),
            String::from_utf8(output.stderr).unwrap()
        );
    }

    Ok(())
}
