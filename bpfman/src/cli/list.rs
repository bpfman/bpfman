// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::bail;
use bpfman_api::v1::list_response::ListResult;

use crate::{
    bpf::BpfManager,
    cli::{args::ListArgs, table::ProgTable},
    command::{ListFilter, Program},
};

pub(crate) async fn execute_list(
    bpf_manager: &mut BpfManager,
    args: &ListArgs,
) -> anyhow::Result<()> {
    let prog_type_filter = args.program_type.map(|p| p as u32);

    let filter = ListFilter::new(
        prog_type_filter,
        args.metadata_selector
            .clone()
            .unwrap_or_default()
            .iter()
            .map(|(k, v)| (k.to_owned(), v.to_owned()))
            .collect(),
        !args.all,
    );

    let mut table = ProgTable::new_list();

    // TODO(astoycos) cleanup all table printing to accept core types not bpfman_api types.
    for r in bpf_manager.list_programs(filter) {
        let list_entry = ListResult {
            info: if let Program::Unsupported(_) = r {
                None
            } else {
                Some((&r).try_into()?)
            },
            kernel_info: match (&r).try_into() {
                Ok(i) => {
                    if let Program::Unsupported(_) = r {
                        r.delete()?;
                    };
                    Ok(Some(i))
                }
                Err(e) => Err(e),
            }?,
        };
        if let Err(e) = table.add_response_prog(list_entry) {
            bail!(e)
        }
    }
    table.print();
    Ok(())
}
