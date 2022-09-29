// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

mod bpf;
mod certs;
mod command;
mod errors;
mod pull_bytecode;
mod rpc;
mod static_program;
mod utils;

use anyhow::{bail, Context};
use bpf::BpfManager;
use bpfd_api::{
    config::Config,
    util::directories::CFGDIR_STATIC_PROGRAMS,
    v1::{loader_server::LoaderServer, ProceedOn,},
};
pub use certs::get_tls_config;
use command::{AttachType, Command, NetworkMultiAttach};
use errors::BpfdError;
use log::info;
use rpc::{intercept, BpfdLoader};
pub use static_program::programs_from_directory;
use static_program::StaticPrograms;
use tokio::sync::mpsc;
use tonic::transport::{Server, ServerTlsConfig};
use utils::get_ifindex;

use crate::command::{NetworkMultiAttachInfo, Program, ProgramData, ProgramType, Metadata};

pub async fn serve(
    config: Config,
    dispatcher_bytes_xdp: &'static [u8],
    dispatcher_bytes_tc: &'static [u8],
    static_programs: Vec<StaticPrograms>,
) -> anyhow::Result<()> {
    let (tx, mut rx) = mpsc::channel(32);
    let addr = "[::1]:50051".parse().unwrap();

    let loader = BpfdLoader::new(tx);

    let (ca_cert, identity) = get_tls_config(&config.tls)
        .await
        .context("CA Cert File does not exist")?;

    let tls_config = ServerTlsConfig::new()
        .identity(identity)
        .client_ca_root(ca_cert);

    let serve = Server::builder()
        .tls_config(tls_config)?
        .add_service(LoaderServer::with_interceptor(loader, intercept))
        .serve(addr);

    tokio::spawn(async move {
        info!("Listening on [::1]:50051");
        if let Err(e) = serve.await {
            eprintln!("Error = {e:?}");
        }
    });

    let mut bpf_manager = BpfManager::new(&config, dispatcher_bytes_xdp, dispatcher_bytes_tc);
    bpf_manager.rebuild_state()?;

    // Load any static programs first
    if !static_programs.is_empty() {
        info!("Loading static programs from {CFGDIR_STATIC_PROGRAMS}",);
        for programs in static_programs {
            for program in programs.programs {
                let prog_type = program.program_type.parse()?;
                let prog = match prog_type {
                    ProgramType::Xdp => {
                        let mut proc_on = Vec::new();
                        if let Some(m) = program.network_attach {
                            if !m.proceed_on.is_empty() {
                                for i in m.proceed_on.iter() {
                                    match ProceedOn::try_from(i.to_string()) {
                                        Ok(action) => proc_on.push(action as i32),
                                        Err(e) => {
                                            eprintln!("ERROR: {}", e);
                                            std::process::exit(1);
                                        }
                                    };
                                }
                            }
                            let if_index = get_ifindex(&m.interface)?;
                            Program::Xdp(
                                ProgramData {
                                    path: program.path,
                                    section_name: program.section_name.clone(),
                                    owner: String::from("bpfd"),
                                },
                                NetworkMultiAttachInfo {
                                    if_index,
                                    current_position: None,
                                    metadata: Metadata {
                                        priority: m.priority,
                                        name: program.section_name.clone(),
                                        attached: false,
                                    },
                                    proceed_on: proc_on,
                                    if_name: m.interface,
                                    direction: None,
                                },
                            )
                        } else {
                            bail!("invalid attach type for xdp program")
                        }
                    }
                    _ => unimplemented!(),
                };
                let uuid = bpf_manager.add_program(prog)?;
                info!("Loaded static program {} with UUID {}", program.name, uuid)
            }
        }
    };

    // Start receiving messages
    while let Some(cmd) = rx.recv().await {
        match cmd {
            Command::Load {
                path,
                section_name,
                program_type,
                attach_type:
                    AttachType::NetworkMultiAttach(NetworkMultiAttach {
                        iface,
                        priority,
                        proceed_on,
                        direction,
                    }),
                username,
                responder,
            } => {
                let res = if let Ok(if_index) = get_ifindex(&iface) {
                    let prog = match program_type {
                        command::ProgramType::Xdp => Program::Xdp(
                            ProgramData {
                                path,
                                owner: username,
                                section_name: section_name.clone(),
                            },
                            NetworkMultiAttachInfo {
                                if_index,
                                current_position: None,
                                metadata: command::Metadata {
                                    priority,
                                    name: section_name,
                                    attached: false,
                                },
                                proceed_on,
                                if_name: iface,
                                direction: None,
                            },
                        ),
                        command::ProgramType::Tc => unimplemented!(),
                        command::ProgramType::Tracepoint => unimplemented!(),
                    };
                    bpf_manager.add_program(prog)
                } else {
                    Err(BpfdError::InvalidInterface)
                };
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::Load {
                path,
                section_name,
                attach_type: AttachType::SingleAttach(attach),
                username,
                responder,
                program_type,
            } => {
                let res = bpf_manager.add_single_attach_program(
                    path,
                    program_type,
                    section_name,
                    attach,
                    username,
                );
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::Unload {
                id,
                username,
                responder,
            } => {
                let res = bpf_manager.remove_program(id, username);
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::List { iface, responder } => {
                let res = bpf_manager.list_programs(iface);
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
        }
    }
    Ok(())
}
