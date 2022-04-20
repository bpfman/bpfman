use aya::programs::{Extension, Xdp, XdpFlags, ProgramFd};
use aya::{include_bytes_aligned, BpfLoader, Bpf};
use thiserror::Error;
use tokio::sync::mpsc::Sender;
use tokio::sync::{oneshot, mpsc};
use std::collections::HashMap;

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
        BpfdLoader {
            tx,
        }
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

        // Send the GET request
        tx.send(cmd).await.unwrap();
    
        // Await the response
        let res = resp_rx.await;
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
        let reply = bpfd_api::UnloadResponse {};
        Ok(Response::new(reply))
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
    ArgumentNotProvided(String)
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    env_logger::init();

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
    let xdp_root = include_bytes_aligned!("../../target/bpfel-unknown-none/debug/xdp-dispatcher");
    let mut bpf_manager = BpfManager::new(xdp_root);
    

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

struct BpfManager {
    xdp_root: &'static [u8],
    ifaces : HashMap<String, Bpf>,
    programs : HashMap<String, Vec<Bpf>>,
}

impl BpfManager {
    fn new(xdp_root: &'static [u8]) -> Self { 
        Self { xdp_root, ifaces: HashMap::new(), programs : HashMap::new() } }

    fn new_dispatcher(&mut self, request: LoadRequest) -> Result<(), BpfdError> {
        if request.iface.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("iface".to_string()))
        }
        if request.path.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("path".to_string()))
        }

        // let ifindex = ifindex_from_ifname(&request.iface).unwrap();
    
        let next_available_id = if let Some(prog) = self.programs.get(&request.iface) {
            prog.len()
        } else {
            self.programs.insert(request.iface.clone(), vec![]);
            0
        };
    
        let config = XdpDispatcherConfig {
            num_progs_enabled: next_available_id as u8 + 1,
            chain_call_actions: [DEFAULT_ACTIONS_MAP; 10],
            run_prios: [DEFAULT_PRIORITY; 10],
        };
    
      if self.ifaces.get(&request.iface).is_some() {
        unimplemented!("dispatcher replacement")
      }  

        let mut xdp_root = BpfLoader::new()
            .set_global("CONFIG", &config)
            .load(self.xdp_root)
            .unwrap();
        let xdp_root_prog: &mut Xdp = xdp_root
            .program_mut("dispatcher")
            .unwrap()
            .try_into()
            .unwrap();
        xdp_root_prog.load().unwrap();
        xdp_root_prog
            .attach(&request.iface, XdpFlags::default())
            .unwrap();
   
        let mut bpf = BpfLoader::new()
            .extension(&request.section_name)
            .load_file(request.path)
            .unwrap();
    
        let ext: &mut Extension = bpf
            .program_mut(&request.section_name)
            .unwrap()
            .try_into()
            .unwrap();
    
        let target_fn = format!("prog{}", next_available_id);
    
        ext.load(xdp_root_prog.fd().unwrap(), &target_fn).unwrap();
        ext.attach().unwrap();

        self.ifaces.insert(request.iface.clone(), xdp_root);
        self.programs.get_mut(&request.iface).unwrap().push(bpf);

        Ok(())
    }
}