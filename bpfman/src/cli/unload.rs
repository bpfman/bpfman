// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman_api::v1::{bpfman_client::BpfmanClient, UnloadRequest};

use crate::cli::{args::UnloadArgs, select_channel};

pub(crate) async fn execute_unload(args: &UnloadArgs) -> Result<(), anyhow::Error> {
    let channel = select_channel().expect("failed to select channel");
    let mut client = BpfmanClient::new(channel);
    let request = tonic::Request::new(UnloadRequest { id: args.id });
    let _response = client.unload(request).await?.into_inner();
    Ok(())
}
