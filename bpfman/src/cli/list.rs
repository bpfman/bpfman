// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::bail;
use bpfman_api::{
    config::Config,
    v1::{bpfman_client::BpfmanClient, ListRequest},
    ProgramType,
};
use clap::Args;

use crate::cli::{parse_key_val, select_channel, table::ProgTable};

#[derive(Args, Debug)]
pub(crate) struct ListArgs {
    /// Optional: List a specific program type
    /// Example: --program-type xdp
    ///
    /// [possible values: unspec, socket-filter, kprobe, tc, sched-act,
    ///                   tracepoint, xdp, perf-event, cgroup-skb,
    ///                   cgroup-sock, lwt-in, lwt-out, lwt-xmit, sock-ops,
    ///                   sk-skb, cgroup-device, sk-msg, raw-tracepoint,
    ///                   cgroup-sock-addr, lwt-seg6-local, lirc-mode2,
    ///                   sk-reuseport, flow-dissector, cgroup-sysctl,
    ///                   raw-tracepoint-writable, cgroup-sockopt, tracing,
    ///                   struct-ops, ext, lsm, sk-lookup, syscall]
    #[clap(short, long, verbatim_doc_comment, hide_possible_values = true)]
    program_type: Option<ProgramType>,

    /// Optional: List programs which contain a specific set of metadata labels
    /// that were applied when the program was loaded with `--metadata` parameter.
    /// Format: <KEY>=<VALUE>
    ///
    /// Example: --metadata-selector owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    metadata_selector: Option<Vec<(String, String)>>,

    /// Optional: List all programs.
    #[clap(short, long, verbatim_doc_comment)]
    all: bool,
}

pub(crate) fn execute_list(args: &ListArgs, config: &mut Config) -> anyhow::Result<()> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).unwrap();
            let mut client = BpfmanClient::new(channel);
            let prog_type_filter = args.program_type.map(|p| p as u32);

            let request = tonic::Request::new(ListRequest {
                program_type: prog_type_filter,
                // Transform metadata from a vec of tuples to an owned map.
                match_metadata: args
                    .metadata_selector
                    .clone()
                    .unwrap_or_default()
                    .iter()
                    .map(|(k, v)| (k.to_owned(), v.to_owned()))
                    .collect(),
                bpfman_programs_only: Some(!args.all),
            });
            let response = client.list(request).await?.into_inner();
            let mut table = ProgTable::new_list();

            for r in response.results {
                if let Err(e) = table.add_response_prog(r) {
                    bail!(e)
                }
            }
            table.print();
            Ok::<(), anyhow::Error>(())
        })
}
