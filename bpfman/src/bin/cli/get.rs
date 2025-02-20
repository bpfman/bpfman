// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::{errors::BpfmanError, get_program, setup};
use log::warn;

use crate::{args::GetArgs, table::ProgTable};

pub(crate) fn execute_get(args: &GetArgs) -> Result<(), BpfmanError> {
    let (_, root_db) = setup()?;
    match get_program(&root_db, args.program_id) {
        Ok(program) => {
            if let Ok(p) = ProgTable::new_program(&program) {
                p.print();
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
