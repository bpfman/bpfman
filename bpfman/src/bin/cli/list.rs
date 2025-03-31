// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::bail;
use bpfman::{list_programs, setup, types::ListFilter};

use crate::{args::ListArgs, table::ProgTable};

pub(crate) fn execute_list(args: &ListArgs) -> anyhow::Result<()> {
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
    let (_, root_db) = setup()?;
    for r in list_programs(&root_db, filter)? {
        if let Err(e) = table.add_response_prog(r) {
            bail!(e)
        }
    }
    table.print();
    Ok(())
}
