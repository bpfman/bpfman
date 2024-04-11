#[allow(dead_code)]
mod cli {
    include!("../../bpfman/src/bin/cli/args.rs");
}

use std::{ffi::OsStr, fs::create_dir_all, path::PathBuf};

use clap::{CommandFactory, Parser};
use clap_complete::{Generator, Shell};
use cli::Cli;

use crate::workspace::WORKSPACE_ROOT;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[derive(Debug, Parser)]
pub struct Options {}

fn write_completions_file<G: Generator + Copy, P: AsRef<OsStr>>(generator: G, out_dir: P) {
    let mut cmd = Cli::command().name("bpfman").version(VERSION);
    clap_complete::generate_to(generator, &mut cmd, "bpfman", &out_dir)
        .expect("clap complete generation failed");
}

pub fn build_completion(_opts: Options) -> Result<(), anyhow::Error> {
    let mut out_dir = PathBuf::from(WORKSPACE_ROOT.to_string());
    out_dir.push(".output/completions");
    create_dir_all(&out_dir)?;

    write_completions_file(Shell::Bash, &out_dir);
    write_completions_file(Shell::Elvish, &out_dir);
    write_completions_file(Shell::Fish, &out_dir);
    write_completions_file(Shell::PowerShell, &out_dir);
    write_completions_file(Shell::Zsh, &out_dir);
    eprintln!("completion scripts generated in {out_dir:?}");
    Ok(())
}
