#[allow(dead_code)]
mod cli {
    include!("../../bpfman/src/cli/args.rs");
}

use std::{
    fs::{create_dir_all, remove_dir_all},
    path::{Path, PathBuf},
};

use clap::{CommandFactory, Parser};
use cli::Cli;

use crate::workspace::WORKSPACE_ROOT;

#[derive(Debug, Parser)]
pub struct Options {}

fn generate_manpage(
    cmd: clap::Command,
    name: String,
    out_dir: &PathBuf,
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
    let mut out_dir = PathBuf::from(WORKSPACE_ROOT.to_string());
    out_dir.push(".output/manpage");

    // If a command is renamed or removed, old files still remain.
    // Clean out first. Ignore error if it doesn't exist.
    let _ = remove_dir_all(&out_dir);
    create_dir_all(&out_dir)?;

    let cmd: clap::Command = Cli::command();
    let bpfman_name = cmd.get_name().to_owned();

    // Generate `bpfman` manpage
    generate_manpage(cmd, bpfman_name.to_string(), &out_dir)?;

    // Generate `bpfman <CMD>` manpages (get, list, load, unload, etc)
    for depth1 in Cli::command().get_subcommands() {
        generate_manpage(
            depth1.clone(),
            format!("{}-{}", bpfman_name, depth1.get_name()),
            &out_dir,
        )?;

        // `bpfman load` has additional subcommands (file and image)
        for depth2 in depth1.get_subcommands() {
            generate_manpage(
                depth2.clone(),
                format!(
                    "{}-{}-{}",
                    bpfman_name,
                    depth1.get_name(),
                    depth2.get_name()
                ),
                &out_dir,
            )?;

            // `bpfman load file` and `bpfman load image` have subcommands (xdp, tc, ...)
            for depth3 in depth2.get_subcommands() {
                generate_manpage(
                    depth3.clone(),
                    format!(
                        "{}-{}-{}-{}",
                        bpfman_name,
                        depth1.get_name(),
                        depth2.get_name(),
                        depth3.get_name()
                    ),
                    &out_dir,
                )?;
            }
        }
    }

    Ok(())
}
