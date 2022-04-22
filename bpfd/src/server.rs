use aya::programs::{Extension, Link, LinkRef, ProgramFd, Xdp, XdpFlags};
use aya::{include_bytes_aligned, Bpf, BpfLoader};
use log::info;
use std::collections::HashMap;
use thiserror::Error;
use tokio::sync::mpsc::Sender;
use tokio::sync::{mpsc, oneshot};

use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};
use std::sync::{Arc, Mutex};
use tonic::{transport::Server, Request, Response, Status};
use uuid::Uuid;

use bpfd_common::*;

use bpfd_api::loader_server::{Loader, LoaderServer};
use bpfd_api::{LoadRequest, LoadResponse, UnloadRequest, UnloadResponse};

pub mod bpfd_api {
    tonic::include_proto!("bpfd");
}

#[derive(Debug, Default)]
pub struct BpfProgram {
    _section_name: String,
}

#[derive(Debug)]
pub struct BpfdLoader {
    tx: Arc<Mutex<Sender<Command>>>,
}

const DEFAULT_ACTIONS_MAP: u32 = 1 << 2;
const DEFAULT_PRIORITY: u32 = 50;

impl BpfdLoader {
    fn new(tx: mpsc::Sender<Command>) -> BpfdLoader {
        let tx = Arc::new(Mutex::new(tx));
        BpfdLoader { tx }
    }
}

#[tonic::async_trait]
impl Loader for BpfdLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
        let id = Uuid::new_v4();
        let reply = bpfd_api::LoadResponse { id: id.to_string() };
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Load {
            request,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();

        info!("sending request on mspc channel");
        // Send the GET request
        tx.send(cmd).await.unwrap();

        info!("awaiting response");
        // Await the response
        let res = resp_rx.await;
        info!("got response");
        match res {
            Ok(_) => Ok(Response::new(reply)),
            Err(_) => Err(Status::aborted("an error ocurred")),
        }
    }

    async fn unload(
        &self,
        request: Request<UnloadRequest>,
    ) -> Result<Response<UnloadResponse>, Status> {
        println!("Got an unload request: {:?}", request);
        Err(Status::unimplemented("not yet implemented"))
    }
}

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
enum Command {
    Load {
        request: LoadRequest,
        responder: Responder<Result<(), BpfdError>>,
    },
}

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

#[derive(Debug, Error)]
enum BpfdError {
    #[error("argument {0} not provided")]
    ArgumentNotProvided(String),
    #[error(transparent)]
    BpfProgramError(#[from] aya::programs::ProgramError),
    #[error(transparent)]
    BpfLoadError(#[from] aya::BpfError),
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    TermLogger::init(
        LevelFilter::Info,
        ConfigBuilder::new()
            .set_target_level(LevelFilter::Error)
            .set_location_level(LevelFilter::Error)
            .build(),
        TerminalMode::Mixed,
        ColorChoice::Auto,
    )?;

    let (tx, mut rx) = mpsc::channel(32);

    let addr = "[::1]:50051".parse().unwrap();

    let loader = BpfdLoader::new(tx);

    let serve = Server::builder()
        .add_service(LoaderServer::new(loader))
        .serve(addr);

    tokio::spawn(async move {
        if let Err(e) = serve.await {
            eprintln!("Error = {:?}", e);
        }
    });
    let dispatcher_bytes =
        include_bytes_aligned!("../../target/bpfel-unknown-none/debug/xdp-dispatcher");
    let mut bpf_manager = BpfManager::new(dispatcher_bytes);

    // Start receiving messages
    while let Some(cmd) = rx.recv().await {
        match cmd {
            Command::Load { request, responder } => {
                let res = bpf_manager.new_dispatcher(request);
                // Ignore errors
                let _ = responder.send(res);
            }
        }
    }

    Ok(())
}

/*
fn ifindex_from_ifname(if_name: &str) -> Result<u32, io::Error> {
    let c_str_if_name = CString::new(if_name)?;
    let c_if_name = c_str_if_name.as_ptr();
    // Safety: libc wrapper
    let if_index = unsafe { libc::if_nametoindex(c_if_name) };
    if if_index == 0 {
        return Err(io::Error::last_os_error());
    }
    Ok(if_index)
}
*/

#[derive(Debug)]
struct Dispatcher {
    loader: Bpf,
    link: LinkRef,
}

#[derive(Debug)]
struct BpfManager {
    dispatcher_bytes: &'static [u8],
    dispatchers: HashMap<String, Dispatcher>,
    programs: HashMap<String, Vec<Bpf>>,
}

impl BpfManager {
    fn new(dispatcher_bytes: &'static [u8]) -> Self {
        Self {
            dispatcher_bytes,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
        }
    }

    fn new_dispatcher(&mut self, request: LoadRequest) -> Result<(), BpfdError> {
        info!("new dispatcher");

        if request.iface.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("iface".to_string()));
        }
        if request.path.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("path".to_string()));
        }

        // let ifindex = ifindex_from_ifname(&request.iface).unwrap();
        let next_available_id = if let Some(prog) = self.programs.get(&request.iface) {
            info!("dispatcher loaded");
            prog.len()
        } else {
            info!("no dispatcher loaded");
            self.programs.insert(request.iface.clone(), vec![]);
            0
        };
        info!("next available program id is: {}", next_available_id);

        let config = XdpDispatcherConfig {
            num_progs_enabled: next_available_id as u8 + 1,
            chain_call_actions: [DEFAULT_ACTIONS_MAP; 10],
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        let mut dispatcher_loader = BpfLoader::new()
            .set_global("CONFIG", &config)
            .load(self.dispatcher_bytes)?;

        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut("dispatcher")
            .unwrap()
            .try_into()?;

        dispatcher.load()?;
        info!("dispatcher loaded");
        if self.programs.contains_key(&request.iface) {
            info!("attempting to attach existing programs");
            for (i, v) in self
                .programs
                .get_mut(&request.iface)
                .unwrap()
                .iter_mut()
                .enumerate()
            {
                let ext: &mut Extension = (*v).programs_mut().next().unwrap().1.try_into()?;
                let target_fn = format!("prog{}", i);
                info!("old ext reloaded");
                ext.reload(dispatcher.fd().unwrap(), &target_fn).unwrap();
                info!("old ext attached");
                ext.attach().unwrap();
            }
        } else {
            info!("no existing programs");
        }

        let mut ext_loader = BpfLoader::new()
            .extension(&request.section_name)
            .load_file(request.path)?;

        let ext: &mut Extension = ext_loader
            .program_mut(&request.section_name)
            .unwrap()
            .try_into()?;

        let target_fn = format!("prog{}", next_available_id);

        ext.load(dispatcher.fd().unwrap(), &target_fn)?;
        info!("ext loaded");
        ext.attach()?;
        info!("ext attached");

        if let Some(d) = self.dispatchers.get_mut(&request.iface) {
            d.link
                .update(
                    d.loader.programs().next().unwrap().1.fd().unwrap(),
                    dispatcher.fd().unwrap(),
                )
                .unwrap();
            d.loader = dispatcher_loader;
        } else {
            info!("attach dispatcher");
            let link = dispatcher
                .attach(&request.iface, XdpFlags::default())
                .unwrap();
            info!("dispatcher attached");
            self.dispatchers.insert(
                request.iface.clone(),
                Dispatcher {
                    loader: dispatcher_loader,
                    link,
                },
            );
        }

        let programs = self.programs.get_mut(&request.iface).unwrap();
        programs.push(ext_loader);
        info!(
            "{} programs attached to dispatcher",
            self.programs.get(&request.iface).unwrap().len()
        );
        Ok(())
    }
}
