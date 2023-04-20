// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::sync::{Arc, Mutex};

use bpfd_api::{
    v1::{
        list_response::{list_result, list_result::AttachInfo, ListResult},
        load_request,
        load_request_common::Location,
        loader_server::Loader,
        ListRequest, ListResponse, LoadRequest, LoadResponse, TcAttachInfo, TracepointAttachInfo,
        UnloadRequest, UnloadResponse, XdpAttachInfo,
    },
    TcProceedOn, XdpProceedOn,
};
use log::warn;
use tokio::sync::{mpsc, mpsc::Sender, oneshot};
use tonic::{Request, Response, Status};
use x509_certificate::X509Certificate;

use crate::{oci_utils::BytecodeImage, Command};

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
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();

        if request.common.is_none() {
            return Err(Status::aborted("missing common program info"));
        }
        let common = request.common.unwrap();

        if request.attach_info.is_none() {
            return Err(Status::aborted("missing attach info"));
        }
        let bytecode_source = match common.location.unwrap() {
            Location::Image(i) => crate::command::Location::Image(BytecodeImage::new(
                i.url,
                i.image_pull_policy,
                match i.username.as_ref() {
                    "" => None,
                    u => Some(u.to_string()),
                },
                match i.password.as_ref() {
                    "" => None,
                    p => Some(p.to_string()),
                },
            )),
            Location::File(p) => crate::command::Location::File(p),
        };

        let cmd = match request.attach_info.unwrap() {
            load_request::AttachInfo::XdpAttachInfo(attach) => Command::LoadXDP {
                responder: resp_tx,
                id: common.id,
                global_data: common.global_data,
                location: bytecode_source,
                iface: attach.iface,
                priority: attach.priority,
                proceed_on: XdpProceedOn::from_int32s(attach.proceed_on)
                    .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                section_name: common.section_name,
                username,
            },
            load_request::AttachInfo::TcAttachInfo(attach) => {
                let direction = attach
                    .direction
                    .try_into()
                    .map_err(|_| Status::aborted("direction is not a string"))?;
                Command::LoadTC {
                    responder: resp_tx,
                    location: bytecode_source,
                    id: common.id,
                    global_data: common.global_data,
                    iface: attach.iface,
                    priority: attach.priority,
                    direction,
                    proceed_on: TcProceedOn::from_int32s(attach.proceed_on)
                        .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                    section_name: common.section_name,
                    username,
                }
            }
            load_request::AttachInfo::TracepointAttachInfo(attach) => Command::LoadTracepoint {
                responder: resp_tx,
                id: common.id,
                global_data: common.global_data,
                location: bytecode_source,
                tracepoint: attach.tracepoint,
                section_name: common.section_name,
                username,
            },
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(id) => {
                    reply.id = id;
                    Ok(Response::new(reply))
                }
                Err(e) => {
                    warn!("BPFD load error: {:#?}", e);
                    Err(Status::aborted(format!("{e}")))
                }
            },

            Err(e) => {
                warn!("RPC load error: {:#?}", e);
                Err(Status::aborted(format!("{e}")))
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
            username,
            responder: resp_tx,
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(_) => Ok(Response::new(reply)),
                Err(e) => {
                    warn!("BPFD unload error: {}", e);
                    Err(Status::aborted(format!("{e}")))
                }
            },
            Err(e) => {
                warn!("RPC unload error: {}", e);
                Err(Status::aborted(format!("{e}")))
            }
        }
    }

    async fn list(&self, _request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        let mut reply = ListResponse { results: vec![] };

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::List { responder: resp_tx };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(results) => {
                    for r in results {
                        let attach_info = match r.attach_info {
                            crate::command::AttachInfo::Xdp(info) => {
                                AttachInfo::XdpAttachInfo(XdpAttachInfo {
                                    priority: info.priority,
                                    iface: info.iface,
                                    position: info.position,
                                    proceed_on: info.proceed_on.as_action_vec(),
                                })
                            }
                            crate::command::AttachInfo::Tc(info) => {
                                AttachInfo::TcAttachInfo(TcAttachInfo {
                                    priority: info.priority,
                                    iface: info.iface,
                                    position: info.position,
                                    direction: info.direction.to_string(),
                                    proceed_on: info.proceed_on.as_action_vec(),
                                })
                            }
                            crate::command::AttachInfo::Tracepoint(info) => {
                                AttachInfo::TracepointAttachInfo(TracepointAttachInfo {
                                    tracepoint: info.tracepoint,
                                })
                            }
                        };

                        let loc = match r.location {
                            crate::command::Location::Image(m) => {
                                Some(list_result::Location::Image(bpfd_api::v1::BytecodeImage {
                                    url: m.get_url().to_string(),
                                    image_pull_policy: m.get_pull_policy() as i32,
                                    // Never dump Plaintext Credentials
                                    username: "".to_string(),
                                    password: "".to_string(),
                                }))
                            }
                            crate::command::Location::File(m) => {
                                Some(list_result::Location::File(m))
                            }
                        };

                        reply.results.push(ListResult {
                            id: r.id,
                            section_name: Some(r.name),
                            attach_info: Some(attach_info),
                            location: loc,
                            program_type: r.program_type,
                        })
                    }
                    Ok(Response::new(reply))
                }
                Err(e) => {
                    warn!("BPFD list error: {}", e);
                    Err(Status::aborted(format!("{e}")))
                }
            },
            Err(e) => {
                warn!("RPC list error: {}", e);
                Err(Status::aborted(format!("{e}")))
            }
        }
    }
}
