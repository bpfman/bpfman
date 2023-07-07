// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::sync::{Arc, Mutex};

use bpfd_api::{
    v1::{
        list_response::{list_result, list_result::AttachInfo, ListResult},
        load_request,
        load_request_common::Location,
        loader_server::Loader,
        ListRequest, ListResponse, LoadRequest, LoadResponse, NoAttachInfo, NoLocation,
        TcAttachInfo, TracepointAttachInfo, UnloadRequest, UnloadResponse, UprobeAttachInfo,
        XdpAttachInfo,
    },
    TcProceedOn, XdpProceedOn,
};
use log::{debug, warn};
use tokio::sync::{mpsc, mpsc::Sender, oneshot};
use tonic::{Request, Response, Status};
use uuid::Uuid;

use crate::{
    command::{Command, LoadTCArgs, LoadTracepointArgs, LoadUprobeArgs, LoadXDPArgs, UnloadArgs},
    oci_utils::BytecodeImage,
};

#[derive(Debug, Default)]
struct User {
    username: String,
}

static DEFAULT_USER: User = User {
    username: String::new(),
};

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

        let id = match common.id {
            Some(id) => Some(Uuid::parse_str(&id).map_err(|_| Status::aborted("invalid UUID"))?),
            None => None,
        };

        let cmd = match request.attach_info.unwrap() {
            load_request::AttachInfo::XdpAttachInfo(attach) => Command::LoadXDP(LoadXDPArgs {
                responder: resp_tx,
                id,
                global_data: common.global_data,
                location: bytecode_source,
                iface: attach.iface,
                priority: attach.priority,
                proceed_on: XdpProceedOn::from_int32s(attach.proceed_on)
                    .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                section_name: common.section_name,
                username,
            }),
            load_request::AttachInfo::TcAttachInfo(attach) => {
                let direction = attach
                    .direction
                    .try_into()
                    .map_err(|_| Status::aborted("direction is not a string"))?;
                Command::LoadTC(LoadTCArgs {
                    responder: resp_tx,
                    location: bytecode_source,
                    id,
                    global_data: common.global_data,
                    iface: attach.iface,
                    priority: attach.priority,
                    direction,
                    proceed_on: TcProceedOn::from_int32s(attach.proceed_on)
                        .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                    section_name: common.section_name,
                    username,
                })
            }
            load_request::AttachInfo::TracepointAttachInfo(attach) => {
                Command::LoadTracepoint(LoadTracepointArgs {
                    responder: resp_tx,
                    id,
                    global_data: common.global_data,
                    location: bytecode_source,
                    tracepoint: attach.tracepoint,
                    section_name: common.section_name,
                    username,
                })
            }
            load_request::AttachInfo::UprobeAttachInfo(attach) => {
                Command::LoadUprobe(LoadUprobeArgs {
                    responder: resp_tx,
                    id,
                    global_data: common.global_data,
                    location: bytecode_source,
                    fn_name: attach.fn_name,
                    offset: attach.offset,
                    target: attach.target,
                    pid: attach.pid,
                    _namespace: attach.namespace,
                    section_name: common.section_name,
                    username,
                })
            }
        };

        let tx = self.tx.lock().unwrap().clone();
        // Send the GET request
        tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(id) => {
                    reply.id = id.to_string();
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
        let cmd = Command::Unload(UnloadArgs {
            id,
            username,
            responder: resp_tx,
        });

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

    async fn list(&self, request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
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
                        debug!("RESULTS {:?}", r.clone());
                        let loc;
                        let attach_info;
                        let name;
                        // With bpfd "multi-attach" programs are loaded as
                        // extensions to a dispatcher. The kernel see's these
                        // programs as type "BPF_PROG_TYPE_EXT" rather than
                        // their actual type.  Therefore we check here if the
                        // program is owned by bpfd and use our stored "real"
                        // type for the filtering. Otherwise we use what's stored
                        // in the kernel.
                        let program_type = match r.program_type {
                            Some(t) => t,
                            None => r.kernel_info.program_type,
                        };

                        // initial prog type filtering
                        if let Some(p) = request.get_ref().program_type {
                            if program_type != p {
                                continue;
                            }
                        }

                        // If there's a bpfd ID we know the program has an
                        // attach info and location
                        let id = match r.id {
                            Some(i) => {
                                // populate bpfd attach info
                                attach_info = match r
                                    .attach_info
                                    .expect("program should have attach info")
                                {
                                    crate::command::AttachInfo::Xdp(info) => {
                                        Some(AttachInfo::XdpAttachInfo(XdpAttachInfo {
                                            priority: info.priority,
                                            iface: info.iface,
                                            position: info.position,
                                            proceed_on: info.proceed_on.as_action_vec(),
                                        }))
                                    }
                                    crate::command::AttachInfo::Tc(info) => {
                                        Some(AttachInfo::TcAttachInfo(TcAttachInfo {
                                            priority: info.priority,
                                            iface: info.iface,
                                            position: info.position,
                                            direction: info.direction.to_string(),
                                            proceed_on: info.proceed_on.as_action_vec(),
                                        }))
                                    }
                                    crate::command::AttachInfo::Tracepoint(info) => Some(
                                        AttachInfo::TracepointAttachInfo(TracepointAttachInfo {
                                            tracepoint: info.tracepoint,
                                        }),
                                    ),
                                    crate::command::AttachInfo::Uprobe(info) => {
                                        Some(AttachInfo::UprobeAttachInfo(UprobeAttachInfo {
                                            fn_name: info.fn_name,
                                            offset: info.offset,
                                            target: info.target,
                                            pid: info.pid,
                                            namespace: info.namespace,
                                        }))
                                    }
                                };

                                // populate bpfd location
                                loc = match r.location.expect("program should have location info") {
                                    crate::command::Location::Image(m) => {
                                        Some(list_result::Location::Image(
                                            bpfd_api::v1::BytecodeImage {
                                                url: m.get_url().to_string(),
                                                image_pull_policy: m.get_pull_policy() as i32,
                                                // Never dump Plaintext Credentials
                                                username: "".to_string(),
                                                password: "".to_string(),
                                            },
                                        ))
                                    }
                                    crate::command::Location::File(m) => {
                                        Some(list_result::Location::File(m))
                                    }
                                };

                                // Program names are sometimes abbreviated to a 16 byte length
                                // by the program. If the program is owned by bpfd override the name
                                //  with the full one stored by bpfd.
                                name = r.name.expect("program should have a name tracked by bpfd");
                                // return bpfd UUID
                                Some(i.to_string())
                            }
                            None => {
                                attach_info = Some(AttachInfo::None(NoAttachInfo {}));
                                loc = Some(list_result::Location::NoLocation(NoLocation {}));
                                name = r.kernel_info.name;
                                // skip programs not owned by bpfd
                                if request.get_ref().bpfd_programs_only() {
                                    continue;
                                }
                                None
                            }
                        };

                        debug!("Pushing list result for {:?}", id);
                        reply.results.push(ListResult {
                            id,
                            name,
                            attach_info,
                            location: loc,
                            program_type,
                            bpf_id: r.kernel_info.id,
                            loaded_at: r.kernel_info.loaded_at,
                            tag: r.kernel_info.tag,
                            gpl_compatible: r.kernel_info.gpl_compatible,
                            map_ids: r.kernel_info.map_ids,
                            btf_id: r.kernel_info.btf_id,
                            bytes_xlated: r.kernel_info.bytes_xlated,
                            jited: r.kernel_info.jited,
                            bytes_jited: r.kernel_info.bytes_jited,
                            bytes_memlock: r.kernel_info.bytes_memlock,
                            verified_insns: r.kernel_info.verified_insns,
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

#[cfg(test)]
mod test {
    use bpfd_api::v1::{
        load_request::AttachInfo, load_request_common::Location, LoadRequest, LoadRequestCommon,
        XdpAttachInfo,
    };
    use tokio::sync::mpsc::Receiver;

    use super::*;

    #[tokio::test]
    async fn test_load_with_valid_id() {
        let (tx, rx) = mpsc::channel(32);
        let loader = BpfdLoader::new(tx.clone());

        let request = LoadRequest {
            common: Some(LoadRequestCommon {
                id: Some("4eee7d98-ffb5-49aa-bab8-b6d5d39c638e".to_string()),
                location: Some(Location::Image(bpfd_api::v1::BytecodeImage {
                    url: "quay.io/bpfd-bytecode/xdp:latest".to_string(),
                    ..Default::default()
                })),
                ..Default::default()
            }),
            attach_info: Some(AttachInfo::XdpAttachInfo(XdpAttachInfo {
                iface: "eth0".to_string(),
                priority: 50,
                position: 0,
                proceed_on: vec![2, 31],
            })),
        };

        tokio::spawn(async move {
            mock_serve(rx).await;
        });

        let res = loader.load(Request::new(request)).await;
        assert!(res.is_ok());
    }

    #[tokio::test]
    async fn test_load_with_invalid_id() {
        let (tx, rx) = mpsc::channel(32);
        let loader = BpfdLoader::new(tx.clone());

        let request = LoadRequest {
            common: Some(LoadRequestCommon {
                id: Some("notauuid".to_string()),
                location: Some(Location::Image(bpfd_api::v1::BytecodeImage {
                    url: "quay.io/bpfd-bytecode/xdp:latest".to_string(),
                    ..Default::default()
                })),
                ..Default::default()
            }),
            attach_info: Some(AttachInfo::XdpAttachInfo(XdpAttachInfo {
                iface: "eth0".to_string(),
                priority: 50,
                position: 0,
                proceed_on: vec![2, 31],
            })),
        };

        tokio::spawn(async move {
            mock_serve(rx).await;
        });

        let res = loader.load(Request::new(request)).await;
        assert!(res.is_err());
    }

    async fn mock_serve(mut rx: Receiver<Command>) {
        while let Some(cmd) = rx.recv().await {
            match cmd {
                Command::LoadXDP(args) => args.responder.send(Ok(Uuid::new_v4())).unwrap(),
                Command::LoadTC(args) => args.responder.send(Ok(Uuid::new_v4())).unwrap(),
                Command::LoadTracepoint(args) => args.responder.send(Ok(Uuid::new_v4())).unwrap(),
                Command::LoadUprobe(args) => args.responder.send(Ok(Uuid::new_v4())).unwrap(),
                Command::Unload(args) => args.responder.send(Ok(())).unwrap(),
                Command::List { responder, .. } => responder.send(Ok(vec![])).unwrap(),
            }
        }
    }
}
