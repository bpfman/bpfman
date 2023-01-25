// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

mod bpf;
mod certs;
mod command;
mod errors;
mod multiprog;
mod pull_bytecode;
mod rpc;
mod static_program;
mod utils;

use anyhow::{bail, Context};
use bpf::BpfManager;
use bpfd_api::{
    config::Config,
    util::directories::CFGDIR_STATIC_PROGRAMS,
    v1::{loader_server::LoaderServer, ProceedOn},
};
pub use certs::get_tls_config;
use command::{AttachType, Command, NetworkMultiAttach, TcProgram, TracepointProgram};
use errors::BpfdError;
use log::{info, warn};
use rpc::{intercept, BpfdLoader};
pub use static_program::programs_from_directory;
use static_program::StaticPrograms;
use tokio::sync::mpsc;
use tonic::transport::{Server, ServerTlsConfig};
use utils::get_ifindex;

use crate::command::{
    Metadata, NetworkMultiAttachInfo, Program, ProgramData, ProgramType, XdpProgram,
};

pub async fn serve(config: Config, static_programs: Vec<StaticPrograms>) -> anyhow::Result<()> {
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

    let mut bpf_manager = BpfManager::new(&config);
    bpf_manager.rebuild_state()?;

    // Load any static programs first
    if !static_programs.is_empty() {
        info!("Loading static programs from {CFGDIR_STATIC_PROGRAMS}");
        for programs in static_programs {
            for program in programs.programs {
                let prog_type = program.program_type.parse()?;
                let prog = match prog_type {
                    ProgramType::Xdp => {
                        if let Some(m) = program.network_attach {
                            let proc_on = if !m.proceed_on.is_empty() {
                                let mut p = Vec::new();
                                for i in m.proceed_on.iter() {
                                    match ProceedOn::try_from(i.to_string()) {
                                        Ok(action) => p.push(action as i32),
                                        Err(e) => {
                                            eprintln!("ERROR: {e}");
                                            std::process::exit(1);
                                        }
                                    };
                                }
                                command::ProceedOn(p)
                            } else {
                                command::ProceedOn::default_xdp()
                            };
                            let if_index = get_ifindex(&m.interface)?;
                            let metadata = Metadata::new(m.priority, program.section_name.clone());
                            Program::Xdp(XdpProgram::new(
                                ProgramData::new_from_location(
                                    program.location,
                                    program.section_name.clone(),
                                    String::from("bpfd"),
                                )
                                .await?,
                                NetworkMultiAttachInfo::new(
                                    m.interface,
                                    if_index,
                                    metadata,
                                    proc_on,
                                ),
                            ))
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
                location,
                section_name,
                program_type,
                attach_type:
                    AttachType::NetworkMultiAttach(NetworkMultiAttach {
                        iface,
                        priority,
                        proceed_on,
                        direction,
                        position: _,
                    }),
                username,
                responder,
            } => {
                let res = if let Ok(if_index) = get_ifindex(&iface) {
                    // If proceedOn is empty, then replace with the default
                    let proc_on = if proceed_on.0.is_empty() {
                        match program_type {
                            command::ProgramType::Xdp => command::ProceedOn::default_xdp(),
                            command::ProgramType::Tc => command::ProceedOn::default_tc(),
                            _ => proceed_on,
                        }
                    } else {
                        // FIXME: when proceed-on is supported for TC programs just return: proceed_on
                        match program_type {
                            command::ProgramType::Xdp => proceed_on,
                            command::ProgramType::Tc => {
                                warn!("proceed-on config not supported yet for TC and my have unintended behavior");
                                proceed_on
                            }
                            _ => proceed_on,
                        }
                    };

                    let prog_data_result =
                        ProgramData::new_from_location(location, section_name.clone(), username)
                            .await;

                    match prog_data_result {
                        Ok(prog_data) => {
                            let prog_result: Result<Program, BpfdError> = match program_type {
                                command::ProgramType::Xdp => Ok(Program::Xdp(XdpProgram {
                                    data: prog_data.clone(),
                                    info: NetworkMultiAttachInfo {
                                        if_index,
                                        current_position: None,
                                        metadata: command::Metadata {
                                            priority,
                                            // This could have been overridden by image tags
                                            name: prog_data.section_name,
                                            attached: false,
                                        },
                                        proceed_on: proc_on,
                                        if_name: iface,
                                    },
                                })),
                                command::ProgramType::Tc => Ok(Program::Tc(TcProgram {
                                    data: prog_data.clone(),
                                    info: NetworkMultiAttachInfo {
                                        if_index,
                                        current_position: None,
                                        metadata: command::Metadata {
                                            priority,
                                            name: prog_data.section_name,
                                            attached: false,
                                        },
                                        proceed_on: proc_on,
                                        if_name: iface,
                                    },
                                    direction: direction.unwrap(),
                                })),
                                _ => Err(BpfdError::InvalidProgramType(program_type.to_string())),
                            };

                            match prog_result {
                                Ok(prog) => bpf_manager.add_program(prog),
                                Err(e) => Err(e),
                            }
                        }
                        Err(e) => Err(BpfdError::BpfBytecodeError(e)),
                    }
                } else {
                    Err(BpfdError::InvalidInterface)
                };
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::Load {
                location,
                section_name,
                attach_type: AttachType::SingleAttach(attach),
                username,
                responder,
                program_type,
            } => {
                let prog_data_result =
                    ProgramData::new_from_location(location, section_name, username).await;

                let res = match prog_data_result {
                    Ok(prog_data) => {
                        let prog_result: Result<Program, BpfdError> = match program_type {
                            command::ProgramType::Tracepoint => {
                                Ok(Program::Tracepoint(TracepointProgram {
                                    data: prog_data,
                                    info: attach,
                                }))
                            }
                            _ => Err(BpfdError::InvalidProgramType(program_type.to_string())),
                        };

                        match prog_result {
                            Ok(prog) => bpf_manager.add_program(prog),
                            Err(e) => Err(e),
                        }
                    }
                    Err(e) => Err(BpfdError::BpfBytecodeError(e)),
                };
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
            Command::List { responder } => {
                let progs = bpf_manager.list_programs();
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(progs);
            }
        }
    }
    Ok(())
}
