// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::anyhow;
use args::Commands;
use clap::Parser;
use get::execute_get;
use list::execute_list;
use log::debug;
use unload::execute_unload;

mod args;
mod completions;
mod get;
mod image;
mod list;
mod load;
mod manpage;
mod table;
mod unload;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    env_logger::try_init()?;
    debug!("Log using env_logger");

    let cli = crate::args::Cli::parse();

    cli.command.execute().await
}

impl Commands {
    pub(crate) async fn execute(&self) -> Result<(), anyhow::Error> {
        match self {
            Commands::Load(l) => l.execute().await,
            Commands::Unload(args) => execute_unload(args).await,
            Commands::List(args) => execute_list(args).await,
            Commands::Get(args) => execute_get(args)
                .await
                .map_err(|e| anyhow!("get error: {e}")),
            Commands::Image(i) => i.execute().await,
            Commands::Man(args) => manpage::generate(args),
            Commands::Completions(args) => completions::generate(args),
        }?;

        Ok(())
    }
}
