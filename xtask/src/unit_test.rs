use std::process::Command;

use clap::Parser;

#[derive(Debug, Parser)]
pub struct Options {
    /// Optional: Build the release target
    #[clap(long)]
    pub release: bool,
}

/// Run unit-test
pub fn unit_test(opts: Options) -> Result<(), anyhow::Error> {
    let mut args = vec!["test"];
    if opts.release {
        args.push("--release")
    }
    let status = Command::new("cargo")
        .args(&args)
        .status()
        .expect("failed to run rust unit-test");
    assert!(status.success());

    let args = vec!["test", "./...", "-coverprofile", "cover.out"];
    let status = Command::new("go")
        .args(&args)
        .status()
        .expect("failed to run go unit-test");
    assert!(status.success());

    Ok(())
}
