// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::detach;

use crate::args::DetachArgs;

pub(crate) fn execute_detach(args: &DetachArgs) -> Result<(), anyhow::Error> {
    detach(args.link_id)?;
    Ok(())
}
