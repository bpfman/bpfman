use aya::programs::{tc, Extension, LinkRef, SchedClassifier, TcAttachType, Xdp, XdpFlags};
use aya::{include_bytes_aligned, Bpf, BpfLoader};
use std::collections::HashMap;
use std::ffi::CString;

use std::sync::{Arc, Mutex};
use std::{fs, io};
use tonic::{transport::Server, Request, Response, Status};
use uuid::Uuid;

use bpfd_api::loader_server::{Loader, LoaderServer};
use bpfd_api::{LoadRequest, LoadResponse, ProgramType, UnloadRequest, UnloadResponse};
use log::info;

pub mod bpfd_api {
    tonic::include_proto!("bpfd");
}

#[derive(Debug, Default)]
pub struct BpfProgram {
    section_name: String,
}

#[derive(Debug)]
pub struct BpfdLoader {
    xdp_root: &'static [u8],
    tc_root: &'static [u8],
    xdp_root_pin_path: &'static str,
    tc_root_pin_path: &'static str,
    ifaces: Arc<Mutex<HashMap<String, bool>>>,
    programs: Arc<Mutex<HashMap<String, Vec<BpfProgram>>>>,
}

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
        let tc_ingress_root_prog_path =
            format!("{}/{}/{}", self.tc_root_pin_path, "ingress", root_prog_name);
        let tc_egress_root_prog_path =
            format!("{}/{}/{}", self.tc_root_pin_path, "egress", root_prog_name);

        fs::create_dir_all(xdp_root_prog_path.as_str()).unwrap();
        fs::create_dir_all(tc_ingress_root_prog_path.as_str()).unwrap();
        fs::create_dir_all(tc_egress_root_prog_path.as_str()).unwrap();

        if ifaces.get(&inner.iface).is_none() {
            let mut xdp_root = Bpf::load(self.xdp_root).unwrap();
            let xdp_root_prog: &mut Xdp = xdp_root
                .program_mut("dispatcher")
                .unwrap()
                .try_into()
                .unwrap();
            xdp_root_prog.load().unwrap();
            xdp_root_prog
                .attach(&inner.iface, XdpFlags::default())
                .unwrap();
            xdp_root
                .program_mut("dispatcher")
                .unwrap()
                .pin(format!("{}/root", xdp_root_prog_path.as_str()))
                .unwrap();

            // add the qdisc
            if let Ok(()) = tc::qdisc_add_clsact(&inner.iface) {
                info!("tc qdisc already attached");
            }

            let mut tc_ingress_root = Bpf::load(self.tc_root).unwrap();
            let tc_ingress_root_prog: &mut SchedClassifier = tc_ingress_root
                .program_mut("dispatcher")
                .unwrap()
                .try_into()
                .unwrap();
            tc_ingress_root_prog.load().unwrap();
            tc_ingress_root_prog
                .attach(&inner.iface, TcAttachType::Ingress)
                .unwrap();
            tc_ingress_root
                .program_mut("dispatcher")
                .unwrap()
                .pin(format!("{}/root", tc_ingress_root_prog_path.as_str()))
                .unwrap();

            let mut tc_egress_root = Bpf::load(self.tc_root).unwrap();
            let tc_egress_root_prog: &mut SchedClassifier = tc_egress_root
                .program_mut("dispatcher")
                .unwrap()
                .try_into()
                .unwrap();
            tc_egress_root_prog.load().unwrap();
            tc_egress_root_prog
                .attach(&inner.iface, TcAttachType::Ingress)
                .unwrap();
            tc_egress_root
                .program_mut("dispatcher")
                .unwrap()
                .pin(format!("{}/root", tc_egress_root_prog_path.as_str()))
                .unwrap();

            ifaces.insert(inner.iface.clone(), true);
        }
      
        let mut bpf = BpfLoader::new()
            .programs_as_extensions()
            .load_file(inner.path)
            .unwrap();

        let root_prog_path = match ProgramType::from_i32(inner.program_type) {
            Some(ProgramType::Xdp) => xdp_root_prog_path.as_str(),
            Some(ProgramType::TcIngress) => tc_ingress_root_prog_path.as_str(),
            Some(ProgramType::TcEgress) => tc_egress_root_prog_path.as_str(),
            None => panic!("unidentified program type"),
        };
        let ext: &mut Extension = bpf
            .program_mut(&inner.section_name)
            .unwrap()
            .try_into()
            .unwrap();

        let mut programs = self.programs.lock().unwrap();
        let next_available_id = if let Some(prog) = programs.get(&inner.iface) {
            prog.len()
        } else {
            programs.insert(inner.iface.clone(), vec![]);
            0
        };
        let target_fn = format!("prog{}", next_available_id);
        let root_prog = LinkRef::from_pinned_path(format!("{}/root", root_prog_path)).unwrap();
        ext.load(root_prog.fd().unwrap(), target_fn.to_string())
            .unwrap();
        ext.attach().unwrap();

        bpf.program_mut(&inner.section_name)
            .unwrap()
            .pin(format!("{}/{}", root_prog_path, target_fn))
            .unwrap();

        programs.get_mut(&inner.iface).unwrap().push(BpfProgram {
            section_name: inner.section_name.clone(),
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

    let xdp_root = include_bytes_aligned!("../bpf/.output/xdp_dispatcher.bpf.o");
    let tc_root = include_bytes_aligned!("../bpf/.output/tc_dispatcher.bpf.o");

    let xdp_root_pin_path = "/sys/fs/bpf/xdp";
    let tc_root_pin_path = "/sys/fs/bpf/tc";

    let loader = BpfdLoader {
        xdp_root,
        tc_root,
        xdp_root_pin_path,
        tc_root_pin_path,
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
