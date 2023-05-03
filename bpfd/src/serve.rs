// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs::remove_file, net::SocketAddr, path::Path};

use anyhow::Context;
use bpfd_api::{config::Config, v1::loader_server::LoaderServer};
use log::{debug, error, info};
use tokio::{
    join,
    net::UnixListener,
    select,
    signal::unix::{signal, SignalKind},
    sync::mpsc,
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
    let endpoint = &config.grpc.endpoint;

    let loader = BpfdLoader::new(tx.clone());
    let service = LoaderServer::new(loader);

    let unix = endpoint.unix.clone();
    if Path::new(&unix).exists() {
        // Attempt to remove the socket, since bind fails if it exists
        remove_file(&unix)?;
    }

    let uds = UnixListener::bind(&unix)?;
    let uds_stream = UnixListenerStream::new(uds);
    set_file_permissions(&unix, SOCK_MODE).await;

    let serve = Server::builder()
        .add_service(service.clone())
        .serve_with_incoming_shutdown(uds_stream, shutdown_handler());

    let unix_listener = async move {
        info!("Listening on {}", unix);
        if let Err(e) = serve.await {
            error!("unix socket error: {e:?}");
        }
        info!("Shutdown Unix Handler {}", unix);
    };

    let ip = endpoint.address.parse().map_err(|_| {
        BpfdError::Error(format!(
            "failed to parse listening address '{}'",
            endpoint.address
        ))
    })?;
    let port = endpoint.port;
    let addr = SocketAddr::new(ip, port);
    let (ca_cert, identity) = get_tls_config(&config.tls)
        .await
        .context("CA Cert File does not exist")?;

    let tls_config = ServerTlsConfig::new()
        .identity(identity)
        .client_ca_root(ca_cert);

    let serve = Server::builder()
        .tls_config(tls_config)?
        .add_service(service.clone())
        .serve_with_shutdown(addr, shutdown_handler());

    let tcp_listener = async move {
        info!("Listening on {addr}");
        if let Err(e) = serve.await {
            error!("tcp error: {e:?}");
        }
        info!("Shutdown TCP Handler {}", addr);
    };

    let mut bpf_manager = BpfManager::new(config, rx);
    bpf_manager.rebuild_state().await?;

    let static_programs = get_static_programs(static_program_path).await?;

    // Load any static programs first
    if !static_programs.is_empty() {
        for prog in static_programs {
            let uuid = bpf_manager.add_program(prog, None).await?;
            info!("Loaded static program with UUID {}", uuid)
        }
    };
    join!(unix_listener, tcp_listener, bpf_manager.process_commands());
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
