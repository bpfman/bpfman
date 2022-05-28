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
            id,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        let res = resp_rx.await.unwrap();
        match res {
            Ok(_) => Ok(Response::new(reply)),
            Err(e) => Err(Status::aborted(format!("{}", e))),
        }
    }

    async fn unload(
        &self,
        request: Request<UnloadRequest>,
    ) -> Result<Response<UnloadResponse>, Status> {
        let reply = bpfd_api::UnloadResponse {};
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Unload {
            request,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        let res = resp_rx.await.unwrap();
        match res {
            Ok(_) => Ok(Response::new(reply)),
            Err(e) => Err(Status::aborted(format!("{}", e))),
        }
    }
}

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
enum Command {
    Load {
        request: LoadRequest,
        id: Uuid,
        responder: Responder<Result<(), BpfdError>>,
    },
    Unload {
        request: UnloadRequest,
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
    #[error("No room to attach program. Please remove one and try again.")]
    TooManyPrograms,
    #[error("No programs loaded to requested interface")]
    NoProgramsLoaded,
    #[error("Invalid ID")]
    InvalidID,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    TermLogger::init(
        LevelFilter::Debug,
        ConfigBuilder::new()
            .set_target_level(LevelFilter::Error)
            .set_location_level(LevelFilter::Error)
            .add_filter_ignore("h2".to_string())
            .add_filter_ignore("aya".to_string())
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
        info!("Listening on [::1]:50051");
        if let Err(e) = serve.await {
            eprintln!("Error = {:?}", e);
        }
    });
    let dispatcher_bytes = include_bytes_aligned!("../../bpfd-ebpf/.output/xdp_dispatcher.bpf.o");
    let mut bpf_manager = BpfManager::new(dispatcher_bytes);

    // Start receiving messages
    while let Some(cmd) = rx.recv().await {
        match cmd {
            Command::Load {
                request,
                id,
                responder,
            } => {
                let res = bpf_manager.add_program(request, id);
                // Ignore errors
                let _ = responder.send(res);
            }
            Command::Unload { request, responder } => {
                let res = bpf_manager.remove_program(request);
                // Ignore errors
                let _ = responder.send(res);
            }
        }
    }

    Ok(())
}

#[derive(Debug, Eq, Ord, PartialEq, PartialOrd)]
struct Metadata {
    priority: i32,
    name: String,
    attached: bool,
}

struct ExtensionProgram {
    path: String,
    current_position: Option<usize>,
    loader: Option<Bpf>,
    metadata: Metadata,
    link: Option<OwnedLink<ExtensionLink>>,
}

struct DispatcherProgram {
    loader: Bpf,
    link: Option<OwnedLink<XdpLink>>,
}

struct BpfManager {
    dispatcher_bytes: &'static [u8],
    dispatchers: HashMap<String, DispatcherProgram>,
    programs: HashMap<String, HashMap<Uuid, ExtensionProgram>>,
}

impl BpfManager {
    fn new(dispatcher_bytes: &'static [u8]) -> Self {
        Self {
            dispatcher_bytes,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
        }
    }

    fn add_program(&mut self, request: LoadRequest, id: Uuid) -> Result<(), BpfdError> {
        if request.iface.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("iface".to_string()));
        }
        if request.path.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("path".to_string()));
        }

        let next_available_id = if let Some(prog) = self.programs.get(&request.iface) {
            prog.len()
        } else {
            self.programs.insert(request.iface.clone(), HashMap::new());
            0
        };

        if next_available_id > 9 {
            return Err(BpfdError::TooManyPrograms);
        }

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

        let mut old_links = vec![];
        if self.programs.contains_key(&request.iface) {
            self.programs.get_mut(&request.iface).unwrap().insert(
                id,
                ExtensionProgram {
                    path: request.path,
                    loader: None,
                    current_position: None,
                    metadata: Metadata {
                        priority: request.priority,
                        name: request.section_name,
                        attached: false,
                    },
                    link: None,
                },
            );
            let mut extensions = self
                .programs
                .get_mut(&request.iface)
                .unwrap()
                .values_mut()
                .collect::<Vec<&mut ExtensionProgram>>();
            extensions.sort_by(|a, b| b.metadata.cmp(&a.metadata));
            for (i, v) in extensions.iter_mut().enumerate() {
                if v.metadata.attached {
                    let ext: &mut Extension = v
                        .loader
                        .as_mut()
                        .unwrap()
                        .programs_mut()
                        .next()
                        .unwrap()
                        .1
                        .try_into()?;
                    let target_fn = format!("prog{}", i);
                    let old_link = v.link.take().unwrap();
                    let new_link_id = ext
                        .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                        .unwrap();
                    old_links.push(old_link);
                    v.link = Some(ext.forget_link(new_link_id)?);
                    v.current_position = Some(i);
                    v.metadata.attached = true;
                } else {
                    let mut ext_loader = BpfLoader::new()
                        .extension(&v.metadata.name)
                        .load_file(v.path.clone())?;

                    let ext: &mut Extension = ext_loader
                        .program_mut(&v.metadata.name)
                        .unwrap()
                        .try_into()?;

                    let target_fn = format!("prog{}", i);

                    ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                    let ext_link = ext.attach()?;
                    v.link = Some(ext.forget_link(ext_link)?);
                    v.loader = Some(ext_loader);
                    v.current_position = Some(i);
                    v.metadata.attached = true;
                }
            }
        }

        if let Some(mut d) = self.dispatchers.remove(&request.iface) {
            let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
            let owned_link = dispatcher.forget_link(link)?;
            self.dispatchers.insert(
                request.iface.clone(),
                DispatcherProgram {
                    loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
            // HACK: Close old dispatcher.
            // I'm not sure why this doesn't get cleaned up on drop of `Bpf`...
            // Probably some fancy refcount thing that isn't aware of bpf_link_update.
            // We should offer program.unload() to avoid unsafe + also fix this in Aya.
            let old_dispatcher: &mut Xdp =
                d.loader.program_mut("dispatcher").unwrap().try_into()?;
            if let Some(fd) = old_dispatcher.fd() {
                unsafe { libc::close(fd) };
            }
        } else {
            let link = dispatcher
                .attach(&request.iface, XdpFlags::default())
                .unwrap();
            let owned_link = dispatcher.forget_link(link)?;
            self.dispatchers.insert(
                request.iface.clone(),
                DispatcherProgram {
                    loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        }
        info!(
            "{} programs attached to {}",
            self.programs.get(&request.iface).unwrap().len(),
            &request.iface,
        );
        Ok(())
    }

    fn remove_program(&mut self, request: UnloadRequest) -> Result<(), BpfdError> {
        if request.id.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("id".to_string()));
        }
        if request.iface.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("iface".to_string()));
        }

        let id = request
            .id
            .parse::<Uuid>()
            .map_err(|_| BpfdError::InvalidID)?;

        if let Some(programs) = self.programs.get_mut(&request.iface) {
            // Keep old_program until the dispatcher has been reloaded
            if let Some(mut old_program) = programs.remove(&id) {
                if programs.len() == 0 {
                    if let Some(mut dispatcher) = self.dispatchers.remove(&request.iface) {
                        // HACK: Close old dispatcher.
                        // I'm not sure why this doesn't get cleaned up on drop of `Bpf`...
                        // Probably some fancy refcount thing that isn't aware of bpf_link_update.
                        // We should offer program.unload() to avoid unsafe + also fix this in Aya.
                        let dispatcher_prog: &mut Xdp = dispatcher
                            .loader
                            .program_mut("dispatcher")
                            .unwrap()
                            .try_into()?;
                        if let Some(fd) = dispatcher_prog.fd() {
                            unsafe { libc::close(fd) };
                        }
                    }
                    // HACK: Close old exetnsion.
                    let old_ext: &mut Extension = old_program
                        .loader
                        .as_mut()
                        .unwrap()
                        .program_mut(old_program.metadata.name.as_str())
                        .unwrap()
                        .try_into()?;
                    if let Some(fd) = old_ext.fd() {
                        unsafe { libc::close(fd) };
                    }
                    return Ok(());
                }

                let config = XdpDispatcherConfig {
                    num_progs_enabled: programs.len() as u8,
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

                let mut old_links = vec![];
                let mut extensions = programs
                    .values_mut()
                    .collect::<Vec<&mut ExtensionProgram>>();
                extensions.sort_by(|a, b| b.metadata.cmp(&a.metadata));
                for (i, mut v) in extensions.iter_mut().enumerate() {
                    let ext: &mut Extension = (*v)
                        .loader
                        .as_mut()
                        .unwrap()
                        .programs_mut()
                        .next()
                        .unwrap()
                        .1
                        .try_into()?;
                    let target_fn = format!("prog{}", i);
                    let old_link = v.link.take().unwrap();
                    let new_link_id = ext
                        .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                        .unwrap();
                    old_links.push(old_link);
                    v.link = Some(ext.forget_link(new_link_id)?);
                }

                if let Some(mut d) = self.dispatchers.remove(&request.iface) {
                    let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
                    let owned_link = dispatcher.forget_link(link)?;
                    self.dispatchers.insert(
                        request.iface.clone(),
                        DispatcherProgram {
                            loader: dispatcher_loader,
                            link: Some(owned_link),
                        },
                    );
                    // HACK: Close old dispatcher.
                    // I'm not sure why this doesn't get cleaned up on drop of `Bpf`...
                    // Probably some fancy refcount thing that isn't aware of bpf_link_update.
                    // We should offer program.unload() to avoid unsafe + also fix this in Aya.
                    let old_dispatcher: &mut Xdp =
                        d.loader.program_mut("dispatcher").unwrap().try_into()?;
                    if let Some(fd) = old_dispatcher.fd() {
                        unsafe { libc::close(fd) };
                    }
                }

                // HACK: Close old exetnsion.
                let old_ext: &mut Extension = old_program
                    .loader
                    .as_mut()
                    .unwrap()
                    .program_mut(old_program.metadata.name.as_str())
                    .unwrap()
                    .try_into()?;
                if let Some(fd) = old_ext.fd() {
                    unsafe { libc::close(fd) };
                }
            } else {
                return Err(BpfdError::InvalidID);
            }
        } else {
            return Err(BpfdError::NoProgramsLoaded);
        }
        Ok(())
    }
}
