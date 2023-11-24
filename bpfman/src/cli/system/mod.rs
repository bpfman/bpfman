// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman_api::config::Config;
use clap::Subcommand;

mod service;
use service::{execute_service, ServiceArgs};

#[derive(Subcommand, Debug)]
pub(crate) enum SystemSubcommand {
    /// Load an eBPF program from a local .o file.
    Service(ServiceArgs),
}

impl SystemSubcommand {
    pub(crate) fn execute(&self, config: &Config) -> anyhow::Result<()> {
        match self {
            SystemSubcommand::Service(args) => execute_service(args, config),
        }
    }
}
