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
    let root = PathBuf::from(WORKSPACE_ROOT.to_string());
    let out_dir = root.join("bpfd-api/src");
    let proto_dir = root.join("proto");

    let protos = &["bpfd.proto"];
    let includes = &[proto_dir.to_str().unwrap()];
    tonic_build::configure()
        .out_dir(out_dir)
        .compile(protos, includes)?;

    // protoc -I=./bpfd/proto --go_out=paths=source_relative:./clients/gobpfd ./bpfd/proto/bpfd.proto
    let status = Command::new("protoc")
        .current_dir(&root)
        .args(&[
            "-I=./proto",
            "--go_out=paths=source_relative:./clients/gobpfd/v1",
            "bpfd.proto",
        ])
        .status()
        .expect("failed to build bpf program");
    assert!(status.success());
    let status = Command::new("protoc")
        .current_dir(&root)
        .args(&[
            "-I=./proto",
            "--go-grpc_out=./clients/gobpfd/v1",
            "--go-grpc_opt=paths=source_relative",
            "bpfd.proto",
        ])
        .status()
        .expect("failed to build bpf program");
    assert!(status.success());
    Ok(())
}
