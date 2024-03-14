// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::{errors::BpfmanError, BpfManager};
use log::warn;

use crate::{args::GetArgs, table::ProgTable};

pub(crate) async fn execute_get(
    bpf_manager: &mut BpfManager,
    args: &GetArgs,
) -> Result<(), BpfmanError> {
    match bpf_manager.get_program(args.id) {
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
