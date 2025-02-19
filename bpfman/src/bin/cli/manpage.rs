// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    fs::{create_dir_all, File},
    io::Write as _,
    path::{Path, PathBuf},
};

use clap::{CommandFactory, Parser};
use flate2::{write::GzEncoder, Compression};

use crate::args::Cli;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[derive(Debug, Parser)]
pub struct Args {
    /// Output directory
    out_dir: PathBuf,
}

fn build_manpage(cmd: clap::Command, name: String, out_dir: &Path) -> Result<(), anyhow::Error> {
    let man = clap_mangen::Man::new(cmd.version(VERSION));
    let mut buffer: Vec<u8> = Default::default();
    man.render(&mut buffer)?;
    let file_path = Path::new(out_dir).join(format!("{}.1.gz", name));
    let f = File::create(&file_path)?;
    let mut e = GzEncoder::new(f, Compression::default());
    e.write_all(&buffer)?;
    e.finish()?;
    eprintln!("map page generated in {file_path:?}");
    Ok(())
}

pub fn generate(args: &Args) -> Result<(), anyhow::Error> {
    let Args { out_dir } = args;
    create_dir_all(out_dir)?;

    let cmd: clap::Command = Cli::command();
    let bpfman_name = cmd.get_name().to_owned();

    // Generate `bpfman` manpage
    build_manpage(cmd, bpfman_name.to_string(), out_dir)?;

    // Generate subcommand manpages
    let mut commands = vec![(Cli::command(), bpfman_name.to_string())];

    while let Some((cmd, name)) = commands.pop() {
        build_manpage(cmd.clone(), name.clone(), out_dir)?;

        for subcommand in cmd.get_subcommands() {
            if subcommand.is_hide_set() {
                continue;
            }
            let subcommand_name = format!("{}-{}", name, subcommand.get_name());
            commands.push((subcommand.clone(), subcommand_name));
        }
    }

    Ok(())
}
