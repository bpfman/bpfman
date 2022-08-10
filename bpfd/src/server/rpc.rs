// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::sync::{Arc, Mutex};

use bpfd_api::v1::{
    list_response::ListResult, loader_server::Loader, GetMapRequest, GetMapResponse, ListRequest,
    ListResponse, LoadRequest, LoadResponse, UnloadRequest, UnloadResponse,
};
use log::info;
use tokio::sync::{mpsc, mpsc::Sender, oneshot};
use tonic::{Request, Response, Status};
use uuid::Uuid;
use x509_certificate::X509Certificate;

use crate::server::{bpf::InterfaceInfo, errors::BpfdError, pull_bytecode::pull_bytecode};

#[derive(Debug, Default)]
struct User {
    username: String,
}

static DEFAULT_USER: User = User {
    username: String::new(),
};

/// This function will get called on each inbound request.
/// It extracts the username from the client certificate and adds it to the request
pub(crate) fn intercept(mut req: Request<()>) -> Result<Request<()>, Status> {
    let certs = req
        .peer_certs()
        .ok_or_else(|| Status::unauthenticated("no certificate provided"))?;

    if certs.len() != 1 {
        return Err(Status::unauthenticated(
            "expected only one client certificate",
        ));
    }

    let cert = X509Certificate::from_der(certs[0].get_ref()).unwrap();
    let username = cert
        .subject_common_name()
        .ok_or_else(|| Status::unauthenticated("CN is empty"))?;

    req.extensions_mut().insert(User { username });
    Ok(req)
}

#[derive(Debug)]
pub struct BpfdLoader {
    tx: Arc<Mutex<Sender<Command>>>,
}

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

impl BpfdLoader {
    pub(crate) fn new(tx: mpsc::Sender<Command>) -> BpfdLoader {
        let tx = Arc::new(Mutex::new(tx));
        BpfdLoader { tx }
    }
}

#[tonic::async_trait]
impl Loader for BpfdLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
        let mut reply = LoadResponse { id: String::new() };
        let username = request
            .extensions()
            .get::<User>()
            .unwrap_or(&DEFAULT_USER)
            .username
            .to_string();
        let mut request = request.into_inner();

        if request.from_image {
            // Pull image from Repo if not locally here, dump bytecode
            let internal_program_overrides = pull_bytecode(&request.path).await;
            match internal_program_overrides {
                Ok(internal_program_overrides) => {
                    request.path = internal_program_overrides.path;
                    request.section_name = internal_program_overrides.image_meta.section_name;
                }
                Err(e) => return Err(Status::aborted(format!("{}", e))),
            };
        }

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Load {
            iface: request.iface,
            responder: resp_tx,
            path: request.path,
            priority: request.priority,
            section_name: request.section_name,
            proceed_on: request.proceed_on,
            username,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(id) => {
                reply.id = id.unwrap().to_string();
                Ok(Response::new(reply))
            }
            Err(e) => {
                info!("RPC load error: {}", e);
                Err(Status::aborted(format!("{}", e)))
            }
        }
    }

    async fn unload(
        &self,
        request: Request<UnloadRequest>,
    ) -> Result<Response<UnloadResponse>, Status> {
        let reply = UnloadResponse {};
        let username = request
            .extensions()
            .get::<User>()
            .unwrap_or(&DEFAULT_USER)
            .username
            .to_string();
        let request = request.into_inner();
        let id = request
            .id
            .parse()
            .map_err(|_| Status::invalid_argument("invalid id"))?;

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Unload {
            id,
            iface: request.iface,
            username,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(_) => Ok(Response::new(reply)),
            Err(e) => {
                info!("RPC unload error: {}", e);
                Err(Status::aborted(format!("{}", e)))
            }
        }
    }

    async fn list(&self, request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        let mut reply = ListResponse {
            xdp_mode: String::new(),
            results: vec![],
        };
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::List {
            iface: request.iface,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => {
                let results = res.unwrap();
                reply.xdp_mode = results.xdp_mode;
                for r in results.programs {
                    reply.results.push(ListResult {
                        id: r.id,
                        name: r.name,
                        path: r.path,
                        position: r.position as u32,
                        priority: r.priority,
                        proceed_on: r.proceed_on,
                    })
                }
                Ok(Response::new(reply))
            }
            Err(e) => {
                info!("RPC list error: {}", e);
                Err(Status::aborted(format!("{}", e)))
            }
        }
    }

    async fn get_map(
        &self,
        request: Request<GetMapRequest>,
    ) -> Result<Response<GetMapResponse>, Status> {
        let reply = GetMapResponse {};
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::GetMap {
            iface: request.iface,
            id: request.id,
            map_name: request.map_name,
            socket_path: request.socket_path,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(_) => Ok(Response::new(reply)),
            Err(e) => {
                info!("RPC get_map error: {}", e);
                Err(Status::aborted(format!("{}", e)))
            }
        }
    }
}

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
pub(crate) enum Command {
    Load {
        iface: String,
        path: String,
        priority: i32,
        section_name: String,
        proceed_on: Vec<i32>,
        username: String,
        responder: Responder<Result<Uuid, BpfdError>>,
    },
    Unload {
        id: Uuid,
        iface: String,
        username: String,
        responder: Responder<Result<(), BpfdError>>,
    },
    List {
        iface: String,
        responder: Responder<Result<InterfaceInfo, BpfdError>>,
    },
    GetMap {
        iface: String,
        id: String,
        map_name: String,
        socket_path: String,
        responder: Responder<Result<(), BpfdError>>,
    },
}
