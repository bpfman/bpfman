use std::{
    env,
    fs::{self, create_dir_all},
    path::{Path, PathBuf},
    process::Command,
    string::String,
};

use anyhow::{bail, Context, Result};
use clap::Parser;
use lazy_static::lazy_static;
use serde_json::Value;

#[derive(Debug, Copy, Clone)]
pub enum Architecture {
    BpfEl,
    BpfEb,
}

impl std::str::FromStr for Architecture {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Ok(match s {
            "bpfel-unknown-none" => Architecture::BpfEl,
            "bpfeb-unknown-none" => Architecture::BpfEb,
            _ => return Err("invalid target".to_owned()),
        })
    }
}

impl std::fmt::Display for Architecture {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            Architecture::BpfEl => "bpfel-unknown-none",
            Architecture::BpfEb => "bpfeb-unknown-none",
        })
    }
}

#[derive(Debug, Parser)]
pub struct Options {
    /// Set the endianness of the BPF target
    #[clap(default_value = "bpfel-unknown-none", long)]
    pub target: Architecture,
    /// Build the release target
    #[clap(long)]
    pub release: bool,
    /// Compile rust ebpf dispatcher
    #[clap(long)]
    pub compile_rust_ebpf: bool,
    /// Libbpf dir, required for compiling C code
    #[clap(long, action)]
    pub libbpf_dir: PathBuf,
}

lazy_static! {
    pub static ref WORKSPACE_ROOT: String = workspace_root();
}

fn workspace_root() -> String {
    let output = Command::new("cargo").arg("metadata").output().unwrap();
    if !output.status.success() {
        panic!("unable to run cargo metadata")
    }
    let stdout = String::from_utf8(output.stdout).unwrap();
    let v: Value = serde_json::from_str(&stdout).unwrap();
    v["workspace_root"].as_str().unwrap().to_string()
}

fn get_libbpf_headers<P: AsRef<Path>>(libbpf_dir: P, include_path: P) -> anyhow::Result<()> {
    let dir = include_path.as_ref();
    fs::create_dir_all(dir)?;
    let status = Command::new("make")
        .current_dir(libbpf_dir.as_ref().join("src"))
        .arg(format!("INCLUDEDIR={}", dir.as_os_str().to_string_lossy()))
        .arg("install_headers")
        .status()
        .expect("failed to build get libbpf headers");
    assert!(status.success());
    Ok(())
}

pub fn build_ebpf(opts: Options) -> anyhow::Result<()> {
    let mut src = PathBuf::from(WORKSPACE_ROOT.to_string());
    src.push("bpf");

    let mut out_path = PathBuf::from(WORKSPACE_ROOT.to_string());
    out_path.push(".output");
    create_dir_all(&out_path)?;

    let include_path = out_path.join("include");
    get_libbpf_headers(&opts.libbpf_dir, &include_path)?;
    let files = fs::read_dir(&src).unwrap();
    for file in files {
        let p = file.unwrap().path();
        if let Some(ext) = p.extension() {
            if ext == "c" {
                let mut out = PathBuf::from(&out_path);
                out.push(p.file_name().unwrap());
                out.set_extension("o");
                compile_with_clang(&p, &out, &include_path)?;
            }
        }
    }
    Ok(())
}

/// Build eBPF programs with clang and libbpf headers.
fn compile_with_clang<P: Clone + AsRef<Path>>(
    src: P,
    out: P,
    include_path: P,
) -> anyhow::Result<()> {
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
        .arg(format!("-D__TARGET_ARCH_{arch}"))
        .arg(src.as_ref().as_os_str())
        .arg("-o")
        .arg(out.as_ref().as_os_str());

    let output = cmd.output().context("Failed to execute clang")?;
    if !output.status.success() {
        bail!(
            "Failed to compile BPF programs\n \
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
