// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{ffi::OsStr, fs::create_dir_all, path::PathBuf};

use clap::{CommandFactory, Parser};
use clap_complete::{Generator, Shell};

use crate::args::Cli;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[derive(Debug, Parser)]
pub struct Args {
    /// Output directory
    out_dir: PathBuf,
    /// Optional: shell to generate completions for
    #[clap(long, short)]
    shell: Option<Shell>,
}

fn write_completions_file<G: Generator + Copy, P: AsRef<OsStr>>(generator: G, out_dir: P) {
    let mut cmd = Cli::command().name("bpfman").version(VERSION);
    clap_complete::generate_to(generator, &mut cmd, "bpfman", &out_dir)
        .expect("clap complete generation failed");
}

pub fn generate(args: &Args) -> Result<(), anyhow::Error> {
    let Args { out_dir, shell } = args;
    create_dir_all(out_dir)?;

    if let Some(shell) = shell {
        write_completions_file(*shell, out_dir);
        eprintln!("completion script generated in {out_dir:?}");
        return Ok(());
    }
    write_completions_file(Shell::Bash, out_dir);
    write_completions_file(Shell::Elvish, out_dir);
    write_completions_file(Shell::Fish, out_dir);
    write_completions_file(Shell::PowerShell, out_dir);
    write_completions_file(Shell::Zsh, out_dir);
    eprintln!("completion scripts generated in {out_dir:?}");
    Ok(())
}
