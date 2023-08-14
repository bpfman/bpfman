// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs::remove_file, net::SocketAddr, path::Path};

use anyhow::Context;
use bpfd_api::{
    config::{self, Config},
    v1::bpfd_server::BpfdServer,
};
use log::{debug, info};
use tokio::{
    join,
    net::UnixListener,
    select,
    signal::unix::{signal, SignalKind},
    sync::mpsc,
    task::JoinHandle,
};
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::{Server, ServerTlsConfig};

pub use crate::certs::get_tls_config;
use crate::{
    bpf::BpfManager, errors::BpfdError, rpc::BpfdLoader, static_program::get_static_programs,
    utils::set_file_permissions,
};

const SOCK_MODE: u32 = 0o0770;

pub async fn serve(config: Config, static_program_path: &str) -> anyhow::Result<()> {
    let (tx, rx) = mpsc::channel(32);

    let loader = BpfdLoader::new(tx.clone());
    let service = BpfdServer::new(loader);

    let (ca_cert, identity) = get_tls_config(&config.tls)
        .await
        .context("CA Cert File does not exist")?;

    let tls_config = ServerTlsConfig::new()
        .identity(identity)
        .client_ca_root(ca_cert);

    let mut listeners: Vec<_> = Vec::new();

    for endpoint in &config.grpc.endpoints {
        match endpoint {
            config::Endpoint::Tcp {
                address,
                port,
                enabled,
            } => {
                if !enabled {
                    info!("Skipping disabled endpoint on {address}, port: {port}");
                    continue;
                }

                match serve_tcp(address, *port, tls_config.clone(), service.clone()).await {
                    Ok(handle) => listeners.push(handle),
                    Err(e) => eprintln!("Error = {e:?}"),
                }
            }
            config::Endpoint::Unix { path, enabled } => {
                if !enabled {
                    info!("Skipping disabled endpoint on {path}");
                    continue;
                }

                match serve_unix(path.clone(), service.clone()).await {
                    Ok(handle) => listeners.push(handle),
                    Err(e) => eprintln!("Error = {e:?}"),
                }
            }
        }
    }

    let mut bpf_manager = BpfManager::new(config, rx);
    bpf_manager.rebuild_state().await?;

    let static_programs = get_static_programs(static_program_path).await?;

    // Load any static programs first
    if !static_programs.is_empty() {
        for prog in static_programs {
            let id = bpf_manager.add_program(prog).await?;
            info!("Loaded static program with id {}", id)
        }
    };
    join!(join_listeners(listeners), bpf_manager.process_commands());
    Ok(())
}

pub(crate) async fn shutdown_handler() {
    let mut sigint = signal(SignalKind::interrupt()).unwrap();
    let mut sigterm = signal(SignalKind::terminate()).unwrap();
    select! {
        _ = sigint.recv() => {debug!("Received SIGINT")},
        _ = sigterm.recv() => {debug!("Received SIGTERM")},
    }
}

async fn join_listeners(listeners: Vec<JoinHandle<()>>) {
    for listener in listeners {
        match listener.await {
            Ok(()) => {}
            Err(e) => eprintln!("Error = {e:?}"),
        }
    }
}

async fn serve_unix(
    path: String,
    service: BpfdServer<BpfdLoader>,
) -> anyhow::Result<JoinHandle<()>> {
    // Listen on Unix socket
    if Path::new(&path).exists() {
        // Attempt to remove the socket, since bind fails if it exists
        remove_file(&path)?;
    }

    let uds = UnixListener::bind(&path)?;
    let uds_stream = UnixListenerStream::new(uds);
    set_file_permissions(&path, SOCK_MODE).await;

    let serve = Server::builder()
        .add_service(service)
        .serve_with_incoming_shutdown(uds_stream, shutdown_handler());

    Ok(tokio::spawn(async move {
        info!("Listening on {path}");
        if let Err(e) = serve.await {
            eprintln!("Error = {e:?}");
        }
        info!("Shutdown Unix Handler {}", path);
    }))
}

async fn serve_tcp(
    address: &String,
    port: u16,
    tls_config: ServerTlsConfig,
    service: BpfdServer<BpfdLoader>,
) -> anyhow::Result<JoinHandle<()>> {
    let ip = address
        .parse()
        .map_err(|_| BpfdError::Error(format!("failed to parse listening address'{}'", address)))?;
    let addr = SocketAddr::new(ip, port);

    let serve = Server::builder()
        .tls_config(tls_config)?
        .add_service(service)
        .serve_with_shutdown(addr, shutdown_handler());

    Ok(tokio::spawn(async move {
        info!("Listening on {addr}");
        if let Err(e) = serve.await {
            eprintln!("Error = {e:?}");
        }
        info!("Shutdown TCP Handler {}", addr);
    }))
}
