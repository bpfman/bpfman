// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
#[cfg(not(test))]
use bpfman_api::util::directories::STDIR_DB;
use clap::Parser;
use lazy_static::lazy_static;
use sled::{Config, Db};

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

#[cfg(not(test))]
lazy_static! {
    pub static ref ROOT_DB: Db = Config::default()
        .path(STDIR_DB)
        .open()
        .expect("Unable to open root database");
}

#[cfg(test)]
lazy_static! {
    pub static ref ROOT_DB: Db = Config::default()
        .temporary(true)
        .open()
        .expect("Unable to open temporary root database");
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = cli::args::Cli::parse();
    cli.command.execute().await
}
