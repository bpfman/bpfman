// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::anyhow;
use args::Commands;
use bpfman::{
    utils::{initialize_bpfman, open_config_file},
    BpfManager,
};
use clap::Parser;
use get::execute_get;
use list::execute_list;
use unload::execute_unload;

mod args;
mod get;
mod image;
mod list;
mod load;
mod table;
mod unload;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = crate::args::Cli::parse();
    initialize_bpfman()?;

    cli.command.execute().await
}

impl Commands {
    pub(crate) async fn execute(&self) -> Result<(), anyhow::Error> {
        let config = open_config_file();

        let mut bpf_manager = BpfManager::new(config.clone()).await;

        match self {
            Commands::Load(l) => l.execute(&mut bpf_manager).await,
            Commands::Unload(args) => execute_unload(&mut bpf_manager, args).await,
            Commands::List(args) => execute_list(&mut bpf_manager, args).await,
            Commands::Get(args) => execute_get(&mut bpf_manager, args)
                .await
                .map_err(|e| anyhow!("get error: {e}")),
            Commands::Image(i) => i.execute(&mut bpf_manager).await,
        }?;

        Ok(())
    }
}
