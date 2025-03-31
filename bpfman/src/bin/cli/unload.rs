// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::{remove_program, setup};

use crate::args::UnloadArgs;

pub(crate) fn execute_unload(args: &UnloadArgs) -> Result<(), anyhow::Error> {
    let (config, root_db) = setup()?;
    remove_program(&config, &root_db, args.program_id)?;
    Ok(())
}
