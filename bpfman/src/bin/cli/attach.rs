// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::bail;
use bpfman::{
    attach_program, get_program, setup,
    types::{AttachInfo, TcProceedOn, XdpProceedOn},
};
use log::warn;

use crate::{
    args::{AttachArgs, AttachCommands},
    load::parse_metadata,
    table::ProgTable,
};

pub(crate) fn execute_attach(args: &AttachArgs) -> anyhow::Result<()> {
    let (config, root_db) = setup()?;

    match get_program(&root_db, args.program_id) {
        Ok(program) => {
            let link = attach_program(
                &config,
                &root_db,
                args.program_id,
                args.command
                    .get_attach_info(&program.get_data().get_application_from_metadata())?,
            )?;

            if let Ok(p) = ProgTable::new_link(&program, &link) {
                p.print();
            }
        }
        Err(e) => {
            warn!("unable to retrieve program {}: {}", args.program_id, e);
            bail!(e)
        }
    }
    Ok(())
}

impl AttachCommands {
    pub(crate) fn get_attach_info(
        &self,
        application: &Option<String>,
    ) -> Result<AttachInfo, anyhow::Error> {
        match self {
            AttachCommands::Xdp {
                iface,
                priority,
                proceed_on,
                netns,
                metadata,
            } => {
                let proc_on = match XdpProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(AttachInfo::Xdp {
                    priority: *priority,
                    iface: iface.to_string(),
                    proceed_on: proc_on,
                    netns: netns.clone(),
                    metadata: parse_metadata(metadata, application),
                })
            }
            AttachCommands::Tc {
                direction,
                iface,
                priority,
                proceed_on,
                netns,
                metadata,
            } => {
                match direction.as_str() {
                    "ingress" | "egress" => (),
                    other => bail!("{} is not a valid direction", other),
                };
                let proc_on = match TcProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(AttachInfo::Tc {
                    priority: *priority,
                    iface: iface.to_string(),
                    direction: direction.to_string(),
                    proceed_on: proc_on,
                    netns: netns.clone(),
                    metadata: parse_metadata(metadata, application),
                })
            }
            AttachCommands::Tcx {
                direction,
                iface,
                priority,
                netns,
                metadata,
            } => {
                match direction.as_str() {
                    "ingress" | "egress" => (),
                    other => bail!("{} is not a valid direction", other),
                };
                Ok(AttachInfo::Tcx {
                    priority: *priority,
                    iface: iface.to_string(),
                    direction: direction.to_string(),
                    netns: netns.clone(),
                    metadata: parse_metadata(metadata, application),
                })
            }
            AttachCommands::Tracepoint {
                tracepoint,
                metadata,
            } => Ok(AttachInfo::Tracepoint {
                tracepoint: tracepoint.to_string(),
                metadata: parse_metadata(metadata, application),
            }),
            AttachCommands::Kprobe {
                fn_name,
                offset,
                container_pid,
                metadata,
            } => {
                if container_pid.is_some() {
                    bail!("kprobe container option not supported yet");
                }
                let offset = offset.unwrap_or(0);
                Ok(AttachInfo::Kprobe {
                    fn_name: fn_name.to_string(),
                    offset,
                    container_pid: *container_pid,
                    metadata: parse_metadata(metadata, application),
                })
            }
            AttachCommands::Uprobe {
                fn_name,
                offset,
                target,
                pid,
                container_pid,
                metadata,
            } => {
                let offset = offset.unwrap_or(0);
                Ok(AttachInfo::Uprobe {
                    fn_name: fn_name.clone(),
                    offset,
                    target: target.to_string(),
                    pid: *pid,
                    container_pid: *container_pid,
                    metadata: parse_metadata(metadata, application),
                })
            }
            AttachCommands::Fentry { metadata } => Ok(AttachInfo::Fentry {
                metadata: parse_metadata(metadata, application),
            }),
            AttachCommands::Fexit { metadata } => Ok(AttachInfo::Fexit {
                metadata: parse_metadata(metadata, application),
            }),
        }
    }
}
