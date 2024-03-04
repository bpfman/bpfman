// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::BpfManager;

use crate::args::UnloadArgs;

pub(crate) async fn execute_unload(
    bpf_manager: &mut BpfManager,
    args: &UnloadArgs,
) -> Result<(), anyhow::Error> {
    bpf_manager.remove_program(args.id).await?;
    Ok(())
}
