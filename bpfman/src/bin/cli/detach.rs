// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::{detach, setup};

use crate::args::DetachArgs;

pub(crate) fn execute_detach(args: &DetachArgs) -> Result<(), anyhow::Error> {
    let (config, root_db) = setup()?;
    detach(&config, &root_db, args.link_id)?;
    Ok(())
}
