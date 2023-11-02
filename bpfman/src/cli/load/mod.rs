// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman_api::config::Config;
use clap::Subcommand;

mod file;
mod image;
mod programs;

use file::{execute_load_file, LoadFileArgs};
use image::{execute_load_image, LoadImageArgs};

#[derive(Subcommand, Debug)]
pub(crate) enum LoadSubcommand {
    /// Load an eBPF program from a local .o file.
    File(LoadFileArgs),
    /// Load an eBPF program packaged in a OCI container image from a given registry.
    Image(LoadImageArgs),
}

impl LoadSubcommand {
    pub(crate) fn execute(&self, config: &mut Config) -> anyhow::Result<()> {
        match self {
            LoadSubcommand::File(l) => execute_load_file(l, config),
            LoadSubcommand::Image(l) => execute_load_image(l, config),
        }
    }
}
