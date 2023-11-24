// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman_api::{
    config::Config,
    v1::{bpfman_client::BpfmanClient, UnloadRequest},
};
use clap::Args;

use crate::cli::select_channel;

#[derive(Args, Debug)]
pub(crate) struct UnloadArgs {
    /// Required: Program id to be unloaded.
    id: u32,
}

pub(crate) fn execute_unload(args: &UnloadArgs, config: &mut Config) -> Result<(), anyhow::Error> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).expect("failed to select channel");
            let mut client = BpfmanClient::new(channel);
            let request = tonic::Request::new(UnloadRequest { id: args.id });
            let _response = client.unload(request).await?.into_inner();
            Ok::<(), anyhow::Error>(())
        })
}
