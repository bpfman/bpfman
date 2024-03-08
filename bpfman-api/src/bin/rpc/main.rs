// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use std::path::PathBuf;

use bpfman::utils::{initialize_bpfman, open_config_file};
use clap::{Args, Parser};

use crate::serve::serve;

mod rpc;
mod serve;

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

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Rpc::parse();
    initialize_bpfman()?;

    let config = open_config_file();
    //TODO https://github.com/bpfman/bpfman/issues/881
    serve(config, args.csi_support, args.timeout, &args.socket_path).await?;

    Ok(())
}
