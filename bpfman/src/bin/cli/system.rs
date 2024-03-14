// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::serve::serve;
use bpfman_api::config::Config;

use crate::args::{ServiceArgs, SystemSubcommand};

impl SystemSubcommand {
    pub(crate) async fn execute(&self, config: &Config) -> anyhow::Result<()> {
        match self {
            SystemSubcommand::Service(args) => execute_service(args, config).await,
        }
    }
}

pub(crate) async fn execute_service(args: &ServiceArgs, config: &Config) -> anyhow::Result<()> {
    //TODO https://github.com/bpfman/bpfman/issues/881
    serve(config, args.csi_support, args.timeout, &args.socket_path).await?;
    Ok(())
}
