use tokio::sync::{mpsc, mpsc::Sender, oneshot};

use std::sync::{Arc, Mutex};
use tonic::{Request, Response, Status};
use uuid::Uuid;

use bpfd_api::{
    list_response::ListResult, loader_server::Loader, GetMapRequest, GetMapResponse, ListRequest,
    ListResponse, LoadRequest, LoadResponse, UnloadRequest, UnloadResponse,
};

use crate::{bpf::ProgramInfo, errors::BpfdError};

pub mod bpfd_api {
    tonic::include_proto!("bpfd");
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
        let mut reply = bpfd_api::LoadResponse { id: String::new() };
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Load {
            iface: request.iface,
            responder: resp_tx,
            path: request.path,
            priority: request.priority,
            section_name: request.section_name,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        let res = resp_rx.await.unwrap();
        match res {
            Ok(id) => {
                reply.id = id.to_string();
                Ok(Response::new(reply))
            },
            Err(e) => Err(Status::aborted(format!("{}", e))),
        }
    }

    async fn unload(
        &self,
        request: Request<UnloadRequest>,
    ) -> Result<Response<UnloadResponse>, Status> {
        let reply = bpfd_api::UnloadResponse {};
        let request = request.into_inner();
        let id = request
            .id
            .parse()
            .map_err(|_| Status::invalid_argument("invalid id"))?;

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Unload {
            id,
            iface: request.iface,
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

    async fn list(&self, request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        let mut reply = ListResponse { results: vec![] };
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
        let res = resp_rx.await.unwrap();
        match res {
            Ok(results) => {
                for r in results {
                    reply.results.push(ListResult {
                        id: r.id,
                        name: r.name,
                        path: r.path,
                        position: r.position as u32,
                        priority: r.priority,
                    })
                }
                Ok(Response::new(reply))
            }
            Err(e) => Err(Status::aborted(format!("{}", e))),
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
        let res = resp_rx.await.unwrap();
        match res {
            Ok(_) => Ok(Response::new(reply)),
            Err(e) => Err(Status::aborted(format!("{}", e))),
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
        responder: Responder<Result<Uuid, BpfdError>>,
    },
    Unload {
        id: Uuid,
        iface: String,
        responder: Responder<Result<(), BpfdError>>,
    },
    List {
        iface: String,
        responder: Responder<Result<Vec<ProgramInfo>, BpfdError>>,
    },
    GetMap {
        iface: String,
        id: String,
        map_name: String,
        socket_path: String,
        responder: Responder<Result<(), BpfdError>>,
    },
}
