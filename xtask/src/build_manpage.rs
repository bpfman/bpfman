#[allow(dead_code)]
mod cli {
    include!("../../bpfman/src/cli/args.rs");
}

use std::{env, ffi::OsString, path::Path};

use clap::{CommandFactory, Parser};
use cli::Cli;

#[derive(Debug, Parser)]
pub struct Options {}

fn generate_manpage(
    cmd: clap::Command,
    name: String,
    out_dir: &OsString,
) -> Result<(), anyhow::Error> {
    let man = clap_mangen::Man::new(cmd.version("0.4.0-dev"));
    let mut buffer: Vec<u8> = Default::default();
    man.render(&mut buffer)?;
    let file_path = Path::new(out_dir).join(format!("{}.1", name));
    std::fs::write(&file_path, buffer)?;
    eprintln!("map page generated in {file_path:?}");
    Ok(())
}

pub fn build_manpage(_opts: Options) -> Result<(), anyhow::Error> {
    let out_dir = env::var_os("OUT_DIR").expect("out dir not set");

    let cmd: clap::Command = Cli::command().name("bpfman");
    generate_manpage(cmd, "bpfman".to_string(), &out_dir)?;

    for subcmd in Cli::command().get_subcommands() {
        generate_manpage(
            subcmd.clone(),
            format!("bpfman-{}", subcmd.get_name()),
            &out_dir,
        )?;
    }

    Ok(())
}
