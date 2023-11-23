use std::{path::PathBuf, process::Command, string::String};

use clap::Parser;
use lazy_static::lazy_static;
use serde_json::Value;

#[derive(Debug, Parser)]
pub struct Options {}

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

pub fn build(_opts: Options) -> anyhow::Result<()> {
    build_bpfman(&_opts)?;
    build_csi(&_opts)?;
    Ok(())
}

fn build_bpfman(_opts: &Options) -> anyhow::Result<()> {
    let root = PathBuf::from(WORKSPACE_ROOT.to_string());
    let out_dir = root.join("bpfman-api/src");
    let proto_dir = root.join("proto");

    let protos = &["bpfman.proto"];
    let includes = &[proto_dir.to_str().unwrap()];
    tonic_build::configure()
        .out_dir(out_dir)
        .compile(protos, includes)?;

    // protoc -I=./bpfman/proto --go_out=paths=source_relative:./clients/gobpfman ./bpfman/proto/bpfman.proto
    let status = Command::new("protoc")
        .current_dir(&root)
        .args([
            "-I=./proto",
            "--go_out=paths=source_relative:./clients/gobpfman/v1",
            "bpfman.proto",
        ])
        .status()
        .expect("failed to build bpf program");
    assert!(status.success());
    let status = Command::new("protoc")
        .current_dir(&root)
        .args([
            "-I=./proto",
            "--go-grpc_out=./clients/gobpfman/v1",
            "--go-grpc_opt=paths=source_relative",
            "bpfman.proto",
        ])
        .status()
        .expect("failed to build bpf program");
    assert!(status.success());
    Ok(())
}

fn build_csi(_opts: &Options) -> anyhow::Result<()> {
    let root = PathBuf::from(WORKSPACE_ROOT.to_string());
    let out_dir = root.join("csi/src");
    let proto_dir = root.join("proto");

    let protos = &["csi.proto"];
    let includes = &[proto_dir.to_str().unwrap()];

    tonic_build::configure()
        .out_dir(out_dir)
        .compile(protos, includes)?;
    Ok(())
}
