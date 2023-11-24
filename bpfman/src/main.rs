// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use clap::Parser;

mod bpf;
mod cli;
mod command;
mod dispatcher_config;
mod errors;
mod multiprog;
mod oci_utils;
mod rpc;
mod serve;
mod static_program;
mod storage;
mod utils;

const BPFMAN_ENV_LOG_LEVEL: &str = "RUST_LOG";

fn main() -> anyhow::Result<()> {
    let cli = cli::Cli::parse();
    cli.command.execute()
}
