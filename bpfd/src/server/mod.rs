// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

mod bpf;
mod certs;
mod errors;
mod pull_bytecode;
mod rpc;
mod static_program;

use anyhow::Context;
use bpf::BpfManager;
use bpfd_api::{
    config::Config,
    util::directories::CFGDIR_STATIC_PROGRAMS,
    v1::{loader_server::LoaderServer, ProceedOn, ProgramType},
};
pub use certs::get_tls_config;
use log::info;
use rpc::{BpfdLoader, Command};
pub use static_program::programs_from_directory;
use static_program::StaticPrograms;
use tokio::sync::mpsc;
use tonic::transport::{Server, ServerTlsConfig};

use self::rpc::intercept;

pub async fn serve(
    config: Config,
    dispatcher_bytes_xdp: &'static [u8],
    dispatcher_bytes_tc: &'static [u8],
    static_programs: Vec<StaticPrograms>,
) -> anyhow::Result<()> {
    let (tx, mut rx) = mpsc::channel(32);
    let addr = "[::1]:50051".parse().unwrap();

    let loader = BpfdLoader::new(tx);

    let (ca_cert, identity) = get_tls_config(&config)
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
        info!("Loading static programs from {}", CFGDIR_STATIC_PROGRAMS);

        for programs in static_programs {
            for program in programs.programs {
                let mut proc_on = Vec::new();
                if !program.proceed_on.is_empty() {
                    for i in program.proceed_on.iter() {
                        match ProceedOn::try_from(i.to_string()) {
                            Ok(action) => proc_on.push(action as i32),
                            Err(e) => {
                                eprintln!("ERROR: {e}");
                                std::process::exit(1);
                            }
                        };
                    }
                }
                let prog_type = ProgramType::try_from(program.program_type.to_string())?;

                let uuid = bpf_manager.add_program(
                    prog_type as i32,
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
                program_type,
                iface,
                path,
                priority,
                section_name,
                proceed_on,
                username,
                responder,
            } => {
                let res = bpf_manager.add_program(
                    program_type,
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
