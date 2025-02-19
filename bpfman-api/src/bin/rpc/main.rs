// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use std::{env, fs::create_dir_all, path::PathBuf, str::FromStr};

use anyhow::Context;
use clap::{Args, Parser};
use log::debug;
use systemd_journal_logger::{connected_to_journal, JournalLog};

use crate::{build::CLAP_LONG_VERSION, serve::serve};

mod rpc;
mod serve;
mod storage;

const BPFMAN_ENV_LOG_LEVEL: &str = "RUST_LOG";

const RTDIR_SOCK: &str = "/run/bpfman-sock";
// The CSI socket must be in it's own sub directory so we can easily create a dedicated
// K8s volume mount for it.
const RTDIR_BPFMAN_CSI: &str = "/run/bpfman/csi";

shadow_rs::shadow!(build);

#[derive(Parser, Debug)]
#[command(version=CLAP_LONG_VERSION)]
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

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Rpc::parse();
    initialize_rpc(args.csi_support)?;
    //TODO https://github.com/bpfman/bpfman/issues/881
    serve(args.csi_support, args.timeout, &args.socket_path).await?;

    Ok(())
}
