use std::{path::PathBuf, process::Command};

use clap::Parser;

use crate::workspace::WORKSPACE_ROOT;

#[derive(Debug, Parser)]
pub struct Options {
    /// Optional: Copy from the release target
    #[clap(long)]
    pub release: bool,
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
