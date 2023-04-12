// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

mod bpf;
mod certs;
mod command;
mod dispatcher_config;
mod errors;
mod multiprog;
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
use futures::{channel::oneshot, FutureExt};
use log::{debug, info};
use rpc::{intercept, BpfdLoader};
use static_program::get_static_programs;
use tokio::{
    net::UnixListener,
    select,
    signal::unix::{signal, SignalKind},
    sync::mpsc,
};
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::{Server, ServerTlsConfig};
use utils::{get_ifindex, set_dir_permissions, set_file_permissions};

use crate::command::{Metadata, Program, ProgramData, XdpProgram, XdpProgramInfo};

const MAPS_MODE: u32 = 0o0660;
const SOCK_MODE: u32 = 0o0770;

pub async fn serve(config: Config, static_program_path: &str) -> anyhow::Result<()> {
    let (tx, mut rx) = mpsc::channel(32);
    let mut shutdown_senders = Vec::new();
    let mut task_handles = Vec::new();
    let endpoint = &config.grpc.endpoint;

    // Listen on Unix socket
    let unix = endpoint.unix.clone();
    if Path::new(&unix).exists() {
        // Attempt to remove the socket, since bind fails if it exists
        remove_file(&unix)?;
    }

    let uds = UnixListener::bind(&unix)?;
    let uds_stream = UnixListenerStream::new(uds);
    set_file_permissions(&unix, SOCK_MODE).await;

    let loader = BpfdLoader::new(tx.clone());

    let (shutdown_tx, shutdown_rx) = oneshot::channel();
    shutdown_senders.push(shutdown_tx);

    let serve = Server::builder()
        .add_service(LoaderServer::new(loader))
        .serve_with_incoming_shutdown(uds_stream, shutdown_rx.map(drop));

    task_handles.push(tokio::spawn(async move {
        info!("Listening on {}", unix);
        if let Err(e) = serve.await {
            panic!("Error = {e:?}");
        }
    }));

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

    let (shutdown_tx, shutdown_rx) = oneshot::channel();
    shutdown_senders.push(shutdown_tx);

    let serve = Server::builder()
        .tls_config(tls_config)?
        .add_service(LoaderServer::with_interceptor(loader, intercept))
        .serve_with_shutdown(addr, shutdown_rx.map(drop));

    task_handles.push(tokio::spawn(async move {
        info!("Listening on {addr}");
        if let Err(e) = serve.await {
            panic!("Error = {e:?}");
        }
    }));

    task_handles.push(tokio::spawn(async move {
        let mut sigint = signal(SignalKind::interrupt()).unwrap();
        let mut sigterm = signal(SignalKind::terminate()).unwrap();
        select! {
            _ = sigint.recv() => {debug!("Received SIGINT")},
            _ = sigterm.recv() => {debug!("Received SIGTERM")},
        }

        for handle in shutdown_senders {
            handle.send(()).unwrap_or_default();
        }
    }));

    let mut bpf_manager = BpfManager::new(&config);
    bpf_manager.rebuild_state().await?;

    let static_programs = get_static_programs(static_program_path).await?;

    // Load any static programs first
    if !static_programs.is_empty() {
        for prog in static_programs {
            let uuid = bpf_manager.add_program(prog, None).await?;
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
                        Ok(prog) => bpf_manager.add_program(prog, id).await,
                        Err(e) => Err(e),
                    }
                } else {
                    Err(BpfdError::InvalidInterface)
                };

                // If program was successfully loaded, allow map access by bpfd group members.
                if let Ok(uuid) = &res {
                    let maps_dir = format!("{RTDIR_FS_MAPS}/{uuid}");
                    set_dir_permissions(&maps_dir, MAPS_MODE).await;
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
                        Ok(prog) => bpf_manager.add_program(prog, id).await,
                        Err(e) => Err(e),
                    }
                } else {
                    Err(BpfdError::InvalidInterface)
                };

                // If program was successfully loaded, allow map access by bpfd group members.
                if let Ok(uuid) = &res {
                    let maps_dir = format!("{RTDIR_FS_MAPS}/{}", uuid.clone());
                    set_dir_permissions(&maps_dir, MAPS_MODE).await;
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
                        Ok(prog) => bpf_manager.add_program(prog, id).await,
                        Err(e) => Err(e),
                    }
                };

                // If program was successfully loaded, allow map access by bpfd group members.
                if let Ok(uuid) = &res {
                    let maps_dir = format!("{RTDIR_FS_MAPS}/{uuid}");
                    set_dir_permissions(&maps_dir, MAPS_MODE).await;
                }

                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::Unload {
                id,
                username,
                responder,
            } => {
                let res = bpf_manager.remove_program(id, username).await;
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

    for handle in task_handles {
        handle.await?;
    }

    Ok(())
}
