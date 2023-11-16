use std::{path::PathBuf, process::Command, string::String};

use clap::Parser;
use lazy_static::lazy_static;
use serde_json::Value;

#[derive(Debug, Parser)]
pub struct Options {
    /// Optional: Copy from the release target
    #[clap(long)]
    pub release: bool,
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

/// Copy the binaries
pub fn copy(opts: Options) -> Result<(), anyhow::Error> {
    let root = PathBuf::from(WORKSPACE_ROOT.to_string());
    let scripts_dir = root.join("scripts");

    let mut args = vec!["-E", "./setup.sh", "setup"];
    if opts.release {
        args.push("--release");
    }

    println!("scripts_dir: {}", scripts_dir.display());
    let status = Command::new("sudo")
        .current_dir(&scripts_dir)
        .args(args)
        .status()
        .expect("failed to copy binaries");
    assert!(status.success());
    Ok(())
}
