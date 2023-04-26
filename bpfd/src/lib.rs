// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

mod bpf;
mod certs;
mod command;
mod errors;
mod multiprog;
#[path = "oci-utils/mod.rs"]
mod oci_utils;
mod rpc;
mod static_program;
mod utils;

use std::{fs::remove_file, net::SocketAddr, path::Path};

use anyhow::Context;
use bpf::BpfManager;
use bpfd_api::{config::Config, util::directories::RTDIR_FS_MAPS, v1::loader_server::LoaderServer};
pub use certs::get_tls_config;
use command::{Command, TcProgram, TcProgramInfo, TracepointProgram, TracepointProgramInfo};
use errors::BpfdError;
use log::info;
use rpc::{intercept, BpfdLoader};
use static_program::get_static_programs;
use tokio::{net::UnixListener, sync::mpsc};
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::{Server, ServerTlsConfig};
use utils::{get_ifindex, set_map_permissions};

use crate::command::{Metadata, Program, ProgramData, XdpProgram, XdpProgramInfo};

pub async fn serve(config: Config, static_program_path: &str) -> anyhow::Result<()> {
    let (tx, mut rx) = mpsc::channel(32);
    let endpoint = &config.grpc.endpoint;

    // Listen on Unix socket
    let unix = endpoint.unix.clone();
    if Path::new(&unix).exists() {
        // Attempt to remove the socket, since bind fails if it exists
        remove_file(&unix)?;
    }

    let uds = UnixListener::bind(&unix)?;
    let uds_stream = UnixListenerStream::new(uds);

    let loader = BpfdLoader::new(tx.clone());

    let serve = Server::builder()
        .add_service(LoaderServer::new(loader))
        .serve_with_incoming(uds_stream);

    tokio::spawn(async move {
        info!("Listening on {}", unix);
        if let Err(e) = serve.await {
            eprintln!("Error = {e:?}");
        }
    });

    // Listen on TCP socket
    let addr = SocketAddr::new(
        endpoint
            .address
            .parse()
            .unwrap_or_else(|_| panic!("failed to parse listening address '{}'", endpoint.address)),
        endpoint.port,
    );

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
        info!("Listening on {addr}");
        if let Err(e) = serve.await {
            eprintln!("Error = {e:?}");
        }
    });

    let mut bpf_manager = BpfManager::new(&config);
    bpf_manager.rebuild_state()?;

    let static_programs = get_static_programs(static_program_path).await?;

    // Load any static programs first
    if !static_programs.is_empty() {
        for prog in static_programs {
            let uuid = bpf_manager.add_program(prog, None)?;
            info!("Loaded static program with UUID {}", uuid)
        }
    };

    // Start receiving messages
    while let Some(cmd) = rx.recv().await {
        match cmd {
            Command::LoadXDP {
                location,
                section_name,
                id,
                global_data,
                iface,
                priority,
                proceed_on,
                username,
                responder,
            } => {
                let res = if let Ok(if_index) = get_ifindex(&iface) {
                    let prog_data =
                        ProgramData::new(location, section_name.clone(), global_data, username)
                            .await?;

                    let prog_result = Ok(Program::Xdp(XdpProgram {
                        data: prog_data.clone(),
                        info: XdpProgramInfo {
                            if_index,
                            current_position: None,
                            metadata: command::Metadata {
                                priority,
                                // This could have been overridden by image tags
                                name: prog_data.section_name,
                                attached: false,
                            },
                            proceed_on,
                            if_name: iface,
                        },
                    }));

                    match prog_result {
                        Ok(prog) => bpf_manager.add_program(prog, id),
                        Err(e) => Err(e),
                    }
                } else {
                    Err(BpfdError::InvalidInterface)
                };

                // If program was successfully loaded, allow map access by bpfd group members.
                if let Ok(uuid) = &res {
                    let maps_dir = format!("{RTDIR_FS_MAPS}/{uuid}");
                    set_map_permissions(&maps_dir).await;
                }

                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::LoadTC {
                location,
                section_name,
                id,
                global_data,
                iface,
                priority,
                direction,
                proceed_on,
                username,
                responder,
            } => {
                let res = if let Ok(if_index) = get_ifindex(&iface) {
                    let prog_data =
                        ProgramData::new(location, section_name, global_data, username).await?;

                    let prog_result = Ok(Program::Tc(TcProgram {
                        data: prog_data.clone(),
                        direction,
                        info: TcProgramInfo {
                            if_index,
                            current_position: None,
                            metadata: command::Metadata {
                                priority,
                                // This could have been overridden by image tags
                                name: prog_data.section_name,
                                attached: false,
                            },
                            proceed_on,
                            if_name: iface,
                        },
                    }));

                    match prog_result {
                        Ok(prog) => bpf_manager.add_program(prog, id),
                        Err(e) => Err(e),
                    }
                } else {
                    Err(BpfdError::InvalidInterface)
                };

                // If program was successfully loaded, allow map access by bpfd group members.
                if let Ok(uuid) = &res {
                    let maps_dir = format!("{RTDIR_FS_MAPS}/{}", uuid.clone());
                    set_map_permissions(&maps_dir).await;
                }

                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::LoadTracepoint {
                location,
                section_name,
                id,
                global_data,
                tracepoint,
                username,
                responder,
            } => {
                let res = {
                    let prog_data =
                        ProgramData::new(location, section_name, global_data, username).await?;

                    let prog_result = Ok(Program::Tracepoint(TracepointProgram {
                        data: prog_data,
                        info: TracepointProgramInfo { tracepoint },
                    }));

                    match prog_result {
                        Ok(prog) => bpf_manager.add_program(prog, id),
                        Err(e) => Err(e),
                    }
                };

                // If program was successfully loaded, allow map access by bpfd group members.
                if let Ok(uuid) = &res {
                    let maps_dir = format!("{RTDIR_FS_MAPS}/{uuid}");
                    set_map_permissions(&maps_dir).await;
                }

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
