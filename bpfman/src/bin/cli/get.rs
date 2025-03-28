// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::anyhow;
use bpfman::{errors::BpfmanError, get_link, get_program, setup, types::Link};
use log::warn;

use crate::{
    args::{GetLinkArgs, GetProgramArgs, GetSubcommand},
    table::ProgTable,
};

impl GetSubcommand {
    pub(crate) fn execute(&self) -> anyhow::Result<()> {
        match self {
            GetSubcommand::Program(args) => {
                execute_get_program(args).map_err(|e| anyhow!("get error: {e}"))
            }
            GetSubcommand::Link(args) => {
                execute_get_link(args).map_err(|e| anyhow!("get error: {e}"))
            }
        }
    }
}

pub(crate) fn execute_get_program(args: &GetProgramArgs) -> Result<(), BpfmanError> {
    let (_, root_db) = setup()?;
    match get_program(&root_db, args.program_id) {
        Ok(program) => {
            let mut links: Vec<Link> = vec![];
            let data = program.get_data();

            // If the Name does not exist, then it wasn't loaded by bpfman and
            // skip the `Bpfman State` table section.
            if data.get_name().is_ok() {
                if let Ok(link_ids) = data.get_link_ids() {
                    if !link_ids.is_empty() {
                        for link_id in link_ids {
                            match get_link(&root_db, link_id) {
                                Ok(link) => {
                                    links.push(link);
                                }
                                Err(e) => {
                                    warn!("BPFMAN get error: {}", e);
                                }
                            }
                        }
                    }
                }

                if let Ok(p) = ProgTable::new_program(&program, links) {
                    p.print();
                }
            }
            ProgTable::new_kernel_info(&program)?.print();
            Ok(())
        }
        Err(e) => {
            warn!("BPFMAN get error: {}", e);
            Err(e)
        }
    }
}

pub(crate) fn execute_get_link(args: &GetLinkArgs) -> Result<(), BpfmanError> {
    let (_, root_db) = setup()?;
    match get_link(&root_db, args.link_id) {
        Ok(link) => {
            let program = link.get_program(&root_db)?;

            if let Ok(p) = ProgTable::new_link(&program, &link) {
                p.print();
            }
            Ok(())
        }
        Err(e) => {
            warn!("BPFMAN get error: {}", e);
            Err(e)
        }
    }
}
