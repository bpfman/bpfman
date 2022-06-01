use bpf::BpfManager;
use log::info;
use rpc::{bpfd_api::loader_server::LoaderServer, BpfdLoader, Command};
use tokio::sync::mpsc;
use tonic::transport::Server;

mod bpf;
mod errors;
mod rpc;

pub async fn serve(dispatcher_bytes: &'static [u8]) -> Result<(), Box<dyn std::error::Error>> {
    let (tx, mut rx) = mpsc::channel(32);
    let addr = "[::1]:50051".parse().unwrap();

    let loader = BpfdLoader::new(tx);

    let serve = Server::builder()
        .add_service(LoaderServer::new(loader))
        .serve(addr);

    tokio::spawn(async move {
        info!("Listening on [::1]:50051");
        if let Err(e) = serve.await {
            eprintln!("Error = {:?}", e);
        }
    });

    let mut bpf_manager = BpfManager::new(dispatcher_bytes);

    // Start receiving messages
    while let Some(cmd) = rx.recv().await {
        match cmd {
            Command::Load {
                iface,
                path,
                priority,
                section_name,
                responder,
            } => {
                let res = bpf_manager.add_program(iface, path, priority, section_name);
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::Unload {
                id,
                iface,
                responder,
            } => {
                let res = bpf_manager.remove_program(id, iface);
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::List { iface, responder } => {
                let res = bpf_manager.list_programs(iface);
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
            Command::GetMap {
                iface,
                id,
                map_name,
                socket_path,
                responder,
            } => {
                let res = bpf_manager.get_map(iface, id, map_name, socket_path);
                // Ignore errors as they'll be propagated to caller in the RPC status
                let _ = responder.send(res);
            }
        }
    }
    Ok(())
}
