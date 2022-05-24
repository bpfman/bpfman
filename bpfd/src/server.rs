use aya::{
    include_bytes_aligned,
    programs::{
        extension::ExtensionLink, xdp::XdpLink, Extension, OwnedLink, ProgramFd, Xdp, XdpFlags,
    },
    Bpf, BpfLoader,
};
use log::info;
use std::collections::HashMap;
use thiserror::Error;
use tokio::sync::{mpsc, mpsc::Sender, oneshot};

use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};
use std::sync::{Arc, Mutex};
use tonic::{transport::Server, Request, Response, Status};
use uuid::Uuid;

use bpfd_common::*;

use bpfd_api::{
    loader_server::{Loader, LoaderServer},
    LoadRequest, LoadResponse, UnloadRequest, UnloadResponse,
};

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
struct ExtensionProgram {
    loader: Bpf,
    link: Option<OwnedLink<ExtensionLink>>,
}

struct DispatcherProgram {
    _loader: Bpf,
    link: Option<OwnedLink<XdpLink>>,
}

struct BpfManager {
    dispatcher_bytes: &'static [u8],
    dispatchers: HashMap<String, DispatcherProgram>,
    programs: HashMap<String, Vec<ExtensionProgram>>,
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

        let mut old_links = vec![];
        if self.programs.contains_key(&request.iface) {
            info!("attempting to attach existing programs");
            for (i, v) in self
                .programs
                .get_mut(&request.iface)
                .unwrap()
                .iter_mut()
                .enumerate()
            {
                let ext: &mut Extension =
                    (*v).loader.programs_mut().next().unwrap().1.try_into()?;
                let target_fn = format!("prog{}", i);
                let old_link = v.link.take().unwrap();
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                old_links.push(old_link);
                v.link = Some(ext.forget_link(new_link_id)?);
                info!("old ext attached to new dispatcher");
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
        let ext_link = ext.attach()?;
        info!("ext attached");
        let owned_ext_link = ext.forget_link(ext_link)?;

        if let Some(mut d) = self.dispatchers.remove(&request.iface) {
            info!("dispatcher replace");
            info!("{:?}", dispatcher.fd().unwrap());
            let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
            let owned_link = dispatcher.forget_link(link)?;
            self.dispatchers.insert(
                request.iface.clone(),
                DispatcherProgram {
                    _loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        } else {
            info!("attach dispatcher");
            let link = dispatcher
                .attach(&request.iface, XdpFlags::default())
                .unwrap();
            let owned_link = dispatcher.forget_link(link)?;
            info!("dispatcher attached");
            self.dispatchers.insert(
                request.iface.clone(),
                DispatcherProgram {
                    _loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        }

        let programs = self.programs.get_mut(&request.iface).unwrap();
        programs.push(ExtensionProgram {
            loader: ext_loader,
            link: Some(owned_ext_link),
        });
        info!(
            "{} programs attached to dispatcher",
            self.programs.get(&request.iface).unwrap().len()
        );
        Ok(())
    }
}
