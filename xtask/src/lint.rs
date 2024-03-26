use std::{path::PathBuf, process::Command};

use clap::Parser;

use crate::workspace::WORKSPACE_ROOT;

#[derive(Debug, Parser)]
pub struct Options {}

/// Run linter
pub fn lint() -> Result<(), anyhow::Error> {
    let root = PathBuf::from(WORKSPACE_ROOT.to_string());
    let scripts_dir = root.join("scripts");

    let args = vec!["-E", "./lint.sh"];

    println!("scripts_dir: {}", scripts_dir.display());
    let status = Command::new("sh")
        .current_dir(&scripts_dir)
        .args(args)
        .status()
        .expect("failed to run lint");
    assert!(status.success());
    Ok(())
}
