use aya::programs::{Extension, ProgramInfo, Xdp, XdpFlags};
use aya::{include_bytes_aligned, BpfLoader};
use std::collections::HashMap;
use std::ffi::CString;

use std::sync::{Arc, Mutex};
use std::{fs, io, mem};
use tonic::{transport::Server, Request, Response, Status};
use uuid::Uuid;

use bpfd_common::*;

use bpfd_api::loader_server::{Loader, LoaderServer};
use bpfd_api::{LoadRequest, LoadResponse, ProgramType, UnloadRequest, UnloadResponse};

pub mod bpfd_api {
    tonic::include_proto!("bpfd");
}

#[derive(Debug, Default)]
pub struct BpfProgram {
    _section_name: String,
}

#[derive(Debug)]
pub struct BpfdLoader {
    xdp_root: &'static [u8],
    xdp_root_pin_path: &'static str,
    ifaces: Arc<Mutex<HashMap<String, bool>>>,
    programs: Arc<Mutex<HashMap<String, Vec<BpfProgram>>>>,
}

const DEFAULT_ACTIONS_MAP: u32 = 1 << 2;
const DEFAULT_PRIORITY: u32 = 50;

#[tonic::async_trait]
impl Loader for BpfdLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
        let id = Uuid::new_v4();
        let reply = bpfd_api::LoadResponse { id: id.to_string() };
        let mut ifaces = self.ifaces.lock().unwrap();
        let inner = request.into_inner();
        if inner.iface.is_empty() {
            return Err(Status::invalid_argument("iface not provided"));
        }
        if inner.path.is_empty() {
            return Err(Status::invalid_argument("path not provided"));
        }
        let ifindex = ifindex_from_ifname(&inner.iface).unwrap();
        let root_prog_name = format!("dispatch-{}", ifindex);
        let xdp_root_prog_path = format!("{}/{}", self.xdp_root_pin_path, root_prog_name);
        fs::create_dir_all(xdp_root_prog_path.as_str()).unwrap();

        let mut programs = self.programs.lock().unwrap();
        let next_available_id = if let Some(prog) = programs.get(&inner.iface) {
            prog.len()
        } else {
            programs.insert(inner.iface.clone(), vec![]);
            0
        };

        let config = XdpDispatcherConfig {
            num_progs_enabled: next_available_id as u8 + 1,
            chain_call_actions: [DEFAULT_ACTIONS_MAP; 10],
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        if ifaces.get(&inner.iface).is_none() {
            let mut xdp_root = BpfLoader::new()
                .set_global("CONFIG", &config)
                .load(self.xdp_root)
                .unwrap();
            // Will log using the default logger, which is TermLogger in this case
            let xdp_root_prog: &mut Xdp = xdp_root
                .program_mut("dispatcher")
                .unwrap()
                .try_into()
                .unwrap();
            xdp_root_prog.load().unwrap();
            let link = xdp_root_prog
                .attach(&inner.iface, XdpFlags::default())
                .unwrap();
            // forget the link so the root prog isn't detached on drop.
            mem::forget(link);
            xdp_root
                .program_mut("dispatcher")
                .unwrap()
                .pin(format!("{}/root", xdp_root_prog_path.as_str()))
                .unwrap();
            ifaces.insert(inner.iface.clone(), true);
        }

        let mut bpf = BpfLoader::new()
            .extension(&inner.section_name)
            .load_file(inner.path)
            .unwrap();

        let root_prog_path = match ProgramType::from_i32(inner.program_type) {
            Some(ProgramType::Xdp) => xdp_root_prog_path.as_str(),
            Some(ProgramType::TcIngress) => todo!("tc support"),
            Some(ProgramType::TcEgress) => todo!("tc support"),
            None => panic!("unidentified program type"),
        };
        let ext: &mut Extension = bpf
            .program_mut(&inner.section_name)
            .unwrap()
            .try_into()
            .unwrap();

        let target_fn = format!("prog{}", next_available_id);
        let root_prog = ProgramInfo::from_pinned(format!("{}/root", root_prog_path)).unwrap();
        ext.load(root_prog.fd().unwrap(), &target_fn).unwrap();
        let link = ext.attach().unwrap();
        // forget the link so the root prog isn't detached on drop.
        mem::forget(link);

        bpf.program_mut(&inner.section_name)
            .unwrap()
            .pin(format!("{}/{}", root_prog_path, target_fn))
            .unwrap();

        programs.get_mut(&inner.iface).unwrap().push(BpfProgram {
            _section_name: inner.section_name.clone(),
        });

        Ok(Response::new(reply))
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

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    env_logger::init();

    let addr = "[::1]:50051".parse()?;
    let ifaces: Arc<Mutex<HashMap<String, bool>>> = Arc::new(Mutex::new(HashMap::new()));
    let programs: Arc<Mutex<HashMap<String, Vec<BpfProgram>>>> =
        Arc::new(Mutex::new(HashMap::new()));

    let xdp_root = include_bytes_aligned!("../../target/bpfel-unknown-none/release/xdp-dispatcher");
    let xdp_root_pin_path = "/sys/fs/bpf/xdp";

    let loader = BpfdLoader {
        xdp_root,
        xdp_root_pin_path,
        ifaces,
        programs,
    };

    Server::builder()
        .add_service(LoaderServer::new(loader))
        .serve(addr)
        .await?;

    Ok(())
}

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
