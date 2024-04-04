// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::remove_program;

use crate::args::UnloadArgs;

pub(crate) async fn execute_unload(args: &UnloadArgs) -> Result<(), anyhow::Error> {
    remove_program(args.program_id).await?;
    Ok(())
}
