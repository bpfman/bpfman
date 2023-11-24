// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman_api::{
    config::Config,
    v1::{bpfman_client::BpfmanClient, GetRequest},
};
use clap::Args;

use crate::cli::{select_channel, table::ProgTable};

#[derive(Args, Debug)]
pub(crate) struct GetArgs {
    /// Required: Program id to get.
    id: u32,
}

pub(crate) fn execute_get(args: &GetArgs, config: &mut Config) -> Result<(), anyhow::Error> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).expect("failed to select channel");
            let mut client = BpfmanClient::new(channel);
            let request = tonic::Request::new(GetRequest { id: args.id });
            let response = client.get(request).await?.into_inner();

            ProgTable::new_get_bpfman(&response.info)?.print();
            ProgTable::new_get_unsupported(&response.kernel_info)?.print();
            Ok::<(), anyhow::Error>(())
        })
}
