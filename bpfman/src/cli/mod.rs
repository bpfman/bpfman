// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub(crate) mod args;
mod get;
mod image;
mod list;
mod load;
mod system;
mod table;
mod unload;

use anyhow::anyhow;
use args::Commands;
use get::execute_get;
use list::execute_list;
use tokio::sync::{broadcast, mpsc};
use unload::execute_unload;

use crate::{
    bpf::BpfManager, cli::system::initialize_bpfman, oci_utils::ImageManager,
    utils::open_config_file,
};

impl Commands {
    pub(crate) async fn execute(&self) -> Result<(), anyhow::Error> {
        initialize_bpfman().await?;

        let config = open_config_file();
        let (shutdown_tx, shutdown_rx1) = broadcast::channel(32);
        let allow_unsigned: bool = config.signing.as_ref().map_or(true, |s| s.allow_unsigned);
        let (itx, irx) = mpsc::channel(32);

        let mut image_manager = ImageManager::new(allow_unsigned, irx).await?;
        let image_manager_handle = tokio::spawn(async move {
            image_manager.run(shutdown_rx1).await;
        });
        let mut bpf_manager = BpfManager::new(config.clone(), None, Some(itx));

        match self {
            Commands::Load(l) => l.execute(&mut bpf_manager).await,
            Commands::Unload(args) => execute_unload(&mut bpf_manager, args).await,
            Commands::List(args) => execute_list(&mut bpf_manager, args).await,
            Commands::Get(args) => execute_get(&mut bpf_manager, args)
                .await
                .map_err(|e| anyhow!("get error: {e}")),
            Commands::Image(i) => i.execute(&mut bpf_manager).await,
            Commands::System(s) => s.execute(&config).await,
        }?;

        // Shutdown the image_manager thread and wait for it to finish
        shutdown_tx.send(()).unwrap();
        image_manager_handle.await?;

        Ok(())
    }
}
