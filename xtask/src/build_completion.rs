#[allow(dead_code)]
mod cli {
    include!("../../bpfman/src/cli/args.rs");
}

use std::{env, ffi::OsStr};

use clap::{CommandFactory, Parser};
use clap_complete::{Generator, Shell};
use cli::Cli;

#[derive(Debug, Parser)]
pub struct Options {}

fn write_completions_file<G: Generator + Copy, P: AsRef<OsStr>>(generator: G, out_dir: P) {
    let mut cmd = Cli::command().name("bpfman").version("0.4.0-dev");
    clap_complete::generate_to(generator, &mut cmd, "bpfman", &out_dir)
        .expect("clap complete generation failed");
}

pub fn build_completion(_opts: Options) -> Result<(), anyhow::Error> {
    let out_dir = env::var_os("OUT_DIR").expect("out dir not set");
    write_completions_file(Shell::Bash, &out_dir);
    write_completions_file(Shell::Elvish, &out_dir);
    write_completions_file(Shell::Fish, &out_dir);
    write_completions_file(Shell::PowerShell, &out_dir);
    write_completions_file(Shell::Zsh, &out_dir);
    eprintln!("completion scripts generated in {out_dir:?}");
    Ok(())
}
