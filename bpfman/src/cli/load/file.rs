// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman_api::{
    config::Config,
    v1::{bpfman_client::BpfmanClient, bytecode_location::Location, BytecodeLocation, LoadRequest},
};
use clap::Args;

use crate::cli::{
    load::programs::{parse_global, parse_global_arg, GlobalArg, LoadCommands},
    parse_key_val, select_channel,
    table::ProgTable,
};

#[derive(Args, Debug)]
pub(crate) struct LoadFileArgs {
    /// Required: Location of local bytecode file
    /// Example: --path /run/bpfman/examples/go-xdp-counter/bpf_bpfel.o
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) path: String,

    /// Required: The name of the function that is the entry point for the BPF program.
    #[clap(short, long)]
    name: String,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    global: Option<Vec<GlobalArg>>,

    /// Optional: Specify Key/Value metadata to be attached to a program when it
    /// is loaded by bpfman.
    /// Format: <KEY>=<VALUE>
    ///
    /// This can later be used to `list` a certain subset of programs which contain
    /// the specified metadata.
    /// Example: --metadata owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    metadata: Option<Vec<(String, String)>>,

    /// Optional: Program id of loaded eBPF program this eBPF program will share a map with.
    /// Only used when multiple eBPF programs need to share a map.
    /// Example: --map-owner-id 63178
    #[clap(long, verbatim_doc_comment)]
    map_owner_id: Option<u32>,

    #[clap(subcommand)]
    pub(crate) command: LoadCommands,
}

pub(crate) fn execute_load_file(args: &LoadFileArgs, config: &mut Config) -> anyhow::Result<()> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).expect("failed to select channel");
            let mut client = BpfmanClient::new(channel);

            let bytecode = Some(BytecodeLocation {
                location: Some(Location::File(args.path.clone())),
            });

            let attach = args.command.get_attach_type()?;

            let request = tonic::Request::new(LoadRequest {
                bytecode,
                name: args.name.to_string(),
                program_type: args.command.get_prog_type() as u32,
                attach,
                metadata: args
                    .metadata
                    .clone()
                    .unwrap_or_default()
                    .iter()
                    .map(|(k, v)| (k.to_owned(), v.to_owned()))
                    .collect(),
                global_data: parse_global(&args.global),
                uuid: None,
                map_owner_id: args.map_owner_id,
            });
            let response = client.load(request).await?.into_inner();

            ProgTable::new_get_bpfman(&response.info)?.print();
            ProgTable::new_get_unsupported(&response.kernel_info)?.print();
            Ok::<(), anyhow::Error>(())
        })
}
