// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use std::{thread, time::Duration};

use bpfman_api::config::Config as BpfmanConfig;
#[cfg(not(test))]
use bpfman_api::util::directories::STDIR_DB;
use clap::Parser;
use once_cell::sync::OnceCell;
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
pub static ROOT_DB: OnceCell<Db> = OnceCell::new();
#[cfg(not(test))]
pub fn root_db_init(config: &BpfmanConfig) -> Db {
    ROOT_DB
        .get_or_init(|| {
            for _n in 1..config.database.as_ref().map_or(4, |d| d.max_retries) {
                if let Ok(db) = Config::default().path(STDIR_DB).open() {
                    return db;
                } else {
                    thread::sleep(Duration::from_millis(
                        config.database.as_ref().map_or(500, |d| d.millisec_delay),
                    ));
                }
            }
            panic!("Unable to open temporary root database");
        })
        .clone()
}

#[cfg(test)]
pub static ROOT_DB: OnceCell<Db> = OnceCell::new();
#[cfg(test)]
pub fn root_db_init(config: &BpfmanConfig) -> Db {
    ROOT_DB
        .get_or_init(|| {
            for _n in 1..config.database.as_ref().map_or(4, |d| d.max_retries) {
                if let Ok(db) = Config::default().temporary(true).open() {
                    return db;
                } else {
                    thread::sleep(Duration::from_millis(
                        config.database.as_ref().map_or(500, |d| d.millisec_delay),
                    ));
                }
            }
            panic!("Unable to open temporary root database");
        })
        .clone()
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = cli::args::Cli::parse();
    cli.command.execute().await
}
