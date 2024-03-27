// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::{errors::BpfmanError, get_program};
use log::warn;

use crate::{args::GetArgs, table::ProgTable};

pub(crate) async fn execute_get(args: &GetArgs) -> Result<(), BpfmanError> {
    match get_program(args.id).await {
        Ok(program) => {
            ProgTable::new_program(&program)?.print();
            ProgTable::new_kernel_info(&program)?.print();
            Ok(())
        }
        Err(e) => {
            warn!("BPFMAN get error: {}", e);
            Err(e)
        }
    }
}
