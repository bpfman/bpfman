// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::bail;
use bpfman::{
    get_link, list_programs, setup,
    types::{ListFilter, METADATA_APPLICATION_TAG},
};
use log::warn;

use crate::{
    args::{ListLinkArgs, ListProgramArgs, ListSubcommand},
    table::ProgTable,
};

impl ListSubcommand {
    pub(crate) fn execute(&self) -> anyhow::Result<()> {
        match self {
            ListSubcommand::Programs(args) => execute_program_list(args),
            ListSubcommand::Program(args) => execute_program_list(args),
            ListSubcommand::Links(args) => execute_link_list(args),
            ListSubcommand::Link(args) => execute_link_list(args),
        }
    }
}

pub(crate) fn execute_program_list(args: &ListProgramArgs) -> anyhow::Result<()> {
    let prog_type_filter = args.program_type.map(|p| p as u32);

    let filter = ListFilter::new(
        prog_type_filter,
        parse_metadata(&args.metadata_selector, &args.application),
        !args.all,
    );

    let mut table = ProgTable::new_program_list();
    let (_, root_db) = setup()?;
    for r in list_programs(&root_db, filter)? {
        if let Err(e) = table.add_program_response(r) {
            bail!(e)
        }
    }
    table.print();
    Ok(())
}

pub(crate) fn execute_link_list(args: &ListLinkArgs) -> anyhow::Result<()> {
    let prog_type_filter = args.program_type.map(|p| p as u32);

    let filter = ListFilter::new(
        prog_type_filter,
        parse_metadata(&args.metadata_selector, &args.application),
        true,
    );

    let mut table = ProgTable::new_link_list();
    let (_, root_db) = setup()?;
    for program in list_programs(&root_db, filter)? {
        let data = program.get_data();
        if let Ok(link_ids) = data.get_link_ids() {
            if !link_ids.is_empty() {
                for link_id in link_ids {
                    match get_link(&root_db, link_id) {
                        Ok(link) => {
                            if let Err(e) = table.add_link_response(&program, &link) {
                                bail!(e)
                            }
                        }
                        Err(e) => {
                            warn!("BPFMAN get error: {}", e);
                        }
                    }
                }
            }
        }
    }
    table.print();
    Ok(())
}

fn parse_metadata(
    metadata: &Option<Vec<(String, String)>>,
    application: &Option<String>,
) -> HashMap<String, String> {
    let mut data: HashMap<String, String> = HashMap::new();
    let mut found = false;

    if let Some(metadata) = metadata {
        for (k, v) in metadata {
            if k == METADATA_APPLICATION_TAG {
                found = true;
            }
            data.insert(k.to_string(), v.to_string());
        }
    }
    if let Some(app) = application {
        if found {
            warn!(
                "application entered but {} already in metadata, ignoring application",
                METADATA_APPLICATION_TAG
            );
        } else {
            data.insert(METADATA_APPLICATION_TAG.to_string(), app.to_string());
        }
    }

    data
}
