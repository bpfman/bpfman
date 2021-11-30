use aya::programs::{tc, SchedClassifier, TcAttachType, Xdp, XdpFlags};
use aya::{include_bytes_aligned, Bpf};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use tonic::{transport::Server, Request, Response, Status};
use uuid::Uuid;

use bpfd_api::loader_server::{Loader, LoaderServer};
use bpfd_api::{LoadRequest, LoadResponse, ProgramType, UnloadRequest, UnloadResponse};
use logs::info;

pub mod bpfd_api {
    tonic::include_proto!("bpfd");
}

#[derive(Debug, Default)]
pub struct BpfProgram {
    section_name: String,
    iface: String,
}

#[derive(Debug, Default)]
pub struct BpfdLoader {
    xdp_root: &'static [u8],
    tc_root: &'static [u8],
    ifaces: Arc<Mutex<HashMap<String, bool>>>,
    programs: Arc<Mutex<HashMap<Uuid, BpfProgram>>>,
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

            // add the qdisc
            if let Ok(()) = tc::qdisc_add_clsact(&inner.iface) {
                info!("tc qdisc already attached");
            }

            let mut tc_root = Bpf::load(self.tc_root).unwrap();
            let tc_root_prog: &mut SchedClassifier = tc_root
                .program_mut("dispatcher")
                .unwrap()
                .try_into()
                .unwrap();
            tc_root_prog.load().unwrap();
            tc_root_prog
                .attach(&inner.iface, TcAttachType::Ingress)
                .unwrap();
            tc_root_prog
                .attach(&inner.iface, TcAttachType::Egress)
                .unwrap();
            ifaces.insert(inner.iface.clone(), true);
        }

        let mut prog = Bpf::load_file(inner.path).unwrap();
        match ProgramType::from_i32(inner.program_type) {
            Some(ProgramType::Xdp) => {
                let xdp_prog: &mut Xdp = prog
                    .program_mut(&inner.section_name)
                    .unwrap()
                    .try_into()
                    .unwrap();

                // TODO: Do the fancy freplace thing
            }
            Some(ProgramType::TcIngress) => {
                let tc_prog: &mut SchedClassifier = prog
                    .program_mut(&inner.section_name)
                    .unwrap()
                    .try_into()
                    .unwrap();

                // TODO: Do the fancy freplace thing
            }
            Some(ProgramType::TcEgress) => {
                let tc_prog: &mut SchedClassifier = prog
                    .program_mut(&inner.section_name)
                    .unwrap()
                    .try_into()
                    .unwrap();

                // TODO: Do the fancy freplace thing
            }
            None => panic!("unidentified program type"),
        }
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
    let addr = "[::1]:50051".parse()?;
    let ifaces: Arc<Mutex<HashMap<String, bool>>> = Arc::new(Mutex::new(HashMap::new()));
    let programs: Arc<Mutex<HashMap<Uuid, BpfProgram>>> = Arc::new(Mutex::new(HashMap::new()));

    let xdp_root = include_bytes_aligned!("../bpf/.output/xdp_dispatcher.bpf.o");
    let tc_root = include_bytes_aligned!("../bpf/.output/tc_dispatcher.bpf.o");

    let loader = BpfdLoader {
        xdp_root,
        tc_root,
        ifaces,
        programs,
    };

    Server::builder()
        .add_service(LoaderServer::new(loader))
        .serve(addr)
        .await?;

    Ok(())
}
