// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

mod bpf;
mod config;
mod errors;
mod pull_bytecode;
mod rpc;
mod static_program;

use anyhow::Context;
use bpf::BpfManager;
use bpfd_api::{
    certs::get_tls_config,
    util::directories::CFGDIR_STATIC_PROGRAMS,
    v1::{loader_server::LoaderServer, ProceedOn},
};
pub use config::config_from_file;
use config::Config;
use log::info;
use rpc::{BpfdLoader, Command};
pub use static_program::programs_from_directory;
use static_program::StaticPrograms;
use tokio::sync::mpsc;
use tonic::transport::{Server, ServerTlsConfig};

use self::rpc::intercept;

const CN_NAME: &str = "bpfd";

pub async fn serve(
    config: Config,
    dispatcher_bytes: &'static [u8],
    static_programs: Vec<StaticPrograms>,
) -> anyhow::Result<()> {
    let (tx, mut rx) = mpsc::channel(32);
    let addr = "[::1]:50051".parse().unwrap();

    let loader = BpfdLoader::new(tx);

    let (ca_cert, identity) = get_tls_config(
        &config.tls.ca_cert,
        &config.tls.key,
        &config.tls.cert,
        CN_NAME,
        true,
        true,
    )
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
            eprintln!("Error = {:?}", e);
        }
    });

    let mut bpf_manager = BpfManager::new(&config, dispatcher_bytes);
    bpf_manager.rebuild_state()?;

    // Load any static programs first
    if !static_programs.is_empty() {
        info!("Loading static programs from {}", CFGDIR_STATIC_PROGRAMS);

        for programs in static_programs {
            for program in programs.programs {
                let mut proc_on = Vec::new();
                if !program.proceed_on.is_empty() {
                    for i in program.proceed_on.iter() {
                        match ProceedOn::try_from(i.to_string()) {
                            Ok(action) => proc_on.push(action as i32),
                            Err(e) => {
                                eprintln!("ERROR: {}", e);
                                std::process::exit(1);
                            }
                        };
                    }
                }

                let uuid = bpf_manager.add_program(
                    program.interface,
                    program.path,
                    program.priority,
                    program.section_name,
                    proc_on,
                    String::from("bpfd"),
                )?;
                info!("Loaded static program {} with UUID {}", program.name, uuid)
            }
        }
    };

    // Start receiving messages
    while let Some(cmd) = rx.recv().await {
        match cmd {
            Command::Load {
                iface,
                path,
                priority,
                section_name,
                proceed_on,
                username,
                responder,
            } => {
                let res = bpf_manager.add_program(
                    iface,
                    path,
                    priority,
                    section_name,
                    proceed_on,
                    username,
                );
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::Unload {
                id,
                iface,
                username,
                responder,
            } => {
                let res = bpf_manager.remove_program(id, iface, username);
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
