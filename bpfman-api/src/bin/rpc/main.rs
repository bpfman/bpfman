// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use std::{env, fs::create_dir_all, path::PathBuf, str::FromStr, sync::Arc};

use anyhow::Context;
use bpfman::{
    add_program,
    config::Config,
    errors::BpfmanError,
    get_program, list_programs, pull_bytecode, remove_program, setup,
    types::{BytecodeImage, ListFilter, Program},
};
use clap::{Args, Parser};
use log::debug;
use systemd_journal_logger::{connected_to_journal, JournalLog};
use tokio::{sync::Mutex, task::spawn_blocking};

use crate::serve::serve;

mod rpc;
mod serve;
mod storage;

const BPFMAN_ENV_LOG_LEVEL: &str = "RUST_LOG";

const RTDIR_SOCK: &str = "/run/bpfman-sock";
// The CSI socket must be in it's own sub directory so we can easily create a dedicated
// K8s volume mount for it.
const RTDIR_BPFMAN_CSI: &str = "/run/bpfman/csi";

#[derive(Parser, Debug)]
#[command(long_about = "A rpc server proxy for the bpfman library")]
#[command(name = "bpfman-rpc")]
pub(crate) struct Rpc {
    /// Optional: Enable CSI support. Only supported when run in a Kubernetes
    /// environment with bpfman-agent.
    #[clap(long, verbatim_doc_comment)]
    pub(crate) csi_support: bool,
    /// Optional: Shutdown after N seconds of inactivity. Use 0 to disable.
    #[clap(long, verbatim_doc_comment, default_value = "15")]
    pub(crate) timeout: u64,
    #[clap(long, default_value = "/run/bpfman-sock/bpfman.sock")]
    /// Optional: Configure the location of the bpfman unix socket.
    pub(crate) socket_path: PathBuf,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct ServiceArgs {
    /// Optional: Shutdown after N seconds of inactivity. Use 0 to disable.
    #[clap(long, verbatim_doc_comment, default_value = "15")]
    pub(crate) timeout: u64,
    #[clap(long, default_value = "/run/bpfman-sock/bpfman.sock")]
    /// Optional: Configure the location of the bpfman unix socket.
    pub(crate) socket_path: PathBuf,
}

fn manage_rpc_journal_log_level() {
    // env_logger uses the environment variable RUST_LOG to set the log
    // level. Parse RUST_LOG to set the log level for journald.
    log::set_max_level(log::LevelFilter::Error);
    if env::var(BPFMAN_ENV_LOG_LEVEL).is_ok() {
        let rust_log = log::LevelFilter::from_str(&env::var(BPFMAN_ENV_LOG_LEVEL).unwrap());
        match rust_log {
            Ok(value) => log::set_max_level(value),
            Err(e) => log::error!("Invalid Log Level: {}", e),
        }
    }
}

fn initialize_rpc(csi_support: bool) -> anyhow::Result<()> {
    if connected_to_journal() {
        // If bpfman is running as a service, log to journald.
        JournalLog::new()?
            .with_extra_fields(vec![("VERSION", env!("CARGO_PKG_VERSION"))])
            .install()
            .expect("unable to initialize journal based logs");
        manage_rpc_journal_log_level();
        debug!("Log using journald");
    } else {
        // Ignore error if already initialized.
        let _ = env_logger::try_init();
        debug!("Log using env_logger");
    }

    create_dir_all(RTDIR_SOCK).context("unable to create socket directory")?;
    if csi_support {
        create_dir_all(RTDIR_BPFMAN_CSI).context("unable to create CSI directory")?;
    }

    Ok(())
}

pub struct AsyncBpfman {}
impl AsyncBpfman {
    pub(crate) fn new() -> Self {
        Self {}
    }

    fn setup(&self) -> anyhow::Result<(Config, sled::Db)> {
        setup().map_err(|e| e.into())
    }

    pub(crate) async fn add_program(&self, program: Program) -> anyhow::Result<Program> {
        let (config, root_db) = self.setup()?;
        match spawn_blocking(move || add_program(&config, &root_db, program)).await {
            Ok(result) => result.map_err(|e| e.into()),
            Err(e) => Err(BpfmanError::InternalError(e.to_string()).into()),
        }
    }

    pub(crate) async fn get_program(&self, id: u32) -> anyhow::Result<Program> {
        let (_, root_db) = self.setup()?;
        match spawn_blocking(move || get_program(&root_db, id)).await {
            Ok(result) => result.map_err(|e| e.into()),
            Err(e) => Err(BpfmanError::InternalError(e.to_string()).into()),
        }
    }

    pub(crate) async fn list_programs(&self, filter: ListFilter) -> anyhow::Result<Vec<Program>> {
        let (_, root_db) = self.setup()?;
        match spawn_blocking(move || list_programs(&root_db, filter)).await {
            Ok(result) => result.map_err(|e| e.into()),
            Err(e) => Err(BpfmanError::InternalError(e.to_string()).into()),
        }
    }

    pub(crate) async fn remove_program(&self, id: u32) -> anyhow::Result<()> {
        let (config, root_db) = self.setup()?;
        match spawn_blocking(move || remove_program(&config, &root_db, id)).await {
            Ok(result) => result.map_err(|e| e.into()),
            Err(e) => Err(BpfmanError::InternalError(e.to_string()).into()),
        }
    }

    pub(crate) async fn pull_bytecode(&self, image: BytecodeImage) -> anyhow::Result<()> {
        let (_, root_db) = self.setup()?;
        match spawn_blocking(move || pull_bytecode(&root_db, image)).await {
            Ok(result) => result,
            Err(e) => Err(BpfmanError::InternalError(e.to_string()).into()),
        }
    }
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Rpc::parse();
    let async_bpfman = AsyncBpfman::new();
    let bpfman_lock: Arc<Mutex<_>> = Arc::new(Mutex::new(async_bpfman));

    initialize_rpc(args.csi_support)?;
    //TODO https://github.com/bpfman/bpfman/issues/881
    serve(
        bpfman_lock,
        args.csi_support,
        args.timeout,
        &args.socket_path,
    )
    .await?;

    Ok(())
}
