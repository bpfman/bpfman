// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::bail;
use bpfman_api::{
    config::Config,
    v1::{
        attach_info::Info, bpfman_client::BpfmanClient, bytecode_location::Location, AttachInfo,
        BytecodeImage, BytecodeLocation, KprobeAttachInfo, LoadRequest, TcAttachInfo,
        TracepointAttachInfo, UprobeAttachInfo, XdpAttachInfo,
    },
    ProgramType, TcProceedOn, XdpProceedOn,
};

use crate::cli::{
    args::{GlobalArg, LoadCommands, LoadFileArgs, LoadImageArgs, LoadSubcommand},
    select_channel,
    table::ProgTable,
};

impl LoadSubcommand {
    pub(crate) fn execute(&self, config: &mut Config) -> anyhow::Result<()> {
        match self {
            LoadSubcommand::File(l) => execute_load_file(l, config),
            LoadSubcommand::Image(l) => execute_load_image(l, config),
        }
    }
}

pub(crate) fn execute_load_file(args: &LoadFileArgs, config: &mut Config) -> anyhow::Result<()> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).expect("failed to select channel");
            let mut client = BpfmanClient::new(channel);

            let bytecode = Some(BytecodeLocation {
                location: Some(Location::File(args.path.clone())),
            });

            let attach = args.command.get_attach_type()?;

            let request = tonic::Request::new(LoadRequest {
                bytecode,
                name: args.name.to_string(),
                program_type: args.command.get_prog_type() as u32,
                attach,
                metadata: args
                    .metadata
                    .clone()
                    .unwrap_or_default()
                    .iter()
                    .map(|(k, v)| (k.to_owned(), v.to_owned()))
                    .collect(),
                global_data: parse_global(&args.global),
                uuid: None,
                map_owner_id: args.map_owner_id,
            });
            let response = client.load(request).await?.into_inner();

            ProgTable::new_get_bpfman(&response.info)?.print();
            ProgTable::new_get_unsupported(&response.kernel_info)?.print();
            Ok::<(), anyhow::Error>(())
        })
}

pub(crate) fn execute_load_image(args: &LoadImageArgs, config: &mut Config) -> anyhow::Result<()> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).expect("failed to select channel");
            let mut client = BpfmanClient::new(channel);

            let bytecode = Some(BytecodeLocation {
                location: Some(Location::Image(BytecodeImage::try_from(&args.pull_args)?)),
            });

            let attach = args.command.get_attach_type()?;

            let request = tonic::Request::new(LoadRequest {
                bytecode,
                name: args.name.to_string(),
                program_type: args.command.get_prog_type() as u32,
                attach,
                metadata: args
                    .metadata
                    .clone()
                    .unwrap_or_default()
                    .iter()
                    .map(|(k, v)| (k.to_owned(), v.to_owned()))
                    .collect(),
                global_data: parse_global(&args.global),
                uuid: None,
                map_owner_id: args.map_owner_id,
            });
            let response = client.load(request).await?.into_inner();

            ProgTable::new_get_bpfman(&response.info)?.print();
            ProgTable::new_get_unsupported(&response.kernel_info)?.print();
            Ok::<(), anyhow::Error>(())
        })
}

impl LoadCommands {
    pub(crate) fn get_prog_type(&self) -> ProgramType {
        match self {
            LoadCommands::Xdp { .. } => ProgramType::Xdp,
            LoadCommands::Tc { .. } => ProgramType::Tc,
            LoadCommands::Tracepoint { .. } => ProgramType::Tracepoint,
            LoadCommands::Kprobe { .. } => ProgramType::Probe,
            LoadCommands::Uprobe { .. } => ProgramType::Probe,
        }
    }

    pub(crate) fn get_attach_type(&self) -> Result<Option<AttachInfo>, anyhow::Error> {
        match self {
            LoadCommands::Xdp {
                iface,
                priority,
                proceed_on,
            } => {
                let proc_on = match XdpProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(Some(AttachInfo {
                    info: Some(Info::XdpAttachInfo(XdpAttachInfo {
                        priority: *priority,
                        iface: iface.to_string(),
                        position: 0,
                        proceed_on: proc_on.as_action_vec(),
                    })),
                }))
            }
            LoadCommands::Tc {
                direction,
                iface,
                priority,
                proceed_on,
            } => {
                match direction.as_str() {
                    "ingress" | "egress" => (),
                    other => bail!("{} is not a valid direction", other),
                };
                let proc_on = match TcProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(Some(AttachInfo {
                    info: Some(Info::TcAttachInfo(TcAttachInfo {
                        priority: *priority,
                        iface: iface.to_string(),
                        position: 0,
                        direction: direction.to_string(),
                        proceed_on: proc_on.as_action_vec(),
                    })),
                }))
            }
            LoadCommands::Tracepoint { tracepoint } => Ok(Some(AttachInfo {
                info: Some(Info::TracepointAttachInfo(TracepointAttachInfo {
                    tracepoint: tracepoint.to_string(),
                })),
            })),
            LoadCommands::Kprobe {
                fn_name,
                offset,
                retprobe,
                container_pid,
            } => {
                if container_pid.is_some() {
                    bail!("kprobe container option not supported yet");
                }
                let offset = offset.unwrap_or(0);
                Ok(Some(AttachInfo {
                    info: Some(Info::KprobeAttachInfo(KprobeAttachInfo {
                        fn_name: fn_name.to_string(),
                        offset,
                        retprobe: *retprobe,
                        container_pid: *container_pid,
                    })),
                }))
            }
            LoadCommands::Uprobe {
                fn_name,
                offset,
                target,
                retprobe,
                pid,
                container_pid,
            } => {
                let offset = offset.unwrap_or(0);
                Ok(Some(AttachInfo {
                    info: Some(Info::UprobeAttachInfo(UprobeAttachInfo {
                        fn_name: fn_name.clone(),
                        offset,
                        target: target.clone(),
                        retprobe: *retprobe,
                        pid: *pid,
                        container_pid: *container_pid,
                    })),
                }))
            }
        }
    }
}

fn parse_global(global: &Option<Vec<GlobalArg>>) -> HashMap<String, Vec<u8>> {
    let mut global_data: HashMap<String, Vec<u8>> = HashMap::new();

    if let Some(global) = global {
        for g in global.iter() {
            global_data.insert(g.name.to_string(), g.value.clone());
        }
    }
    global_data
}
