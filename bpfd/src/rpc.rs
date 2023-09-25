// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::collections::HashMap;

use bpfd_api::{
    v1::{
        bpfd_server::Bpfd,
        list_response::{list_result, list_result::AttachInfo, ListResult},
        load_request,
        load_request_common::Location,
        KprobeAttachInfo, ListRequest, ListResponse, LoadRequest, LoadResponse,
        PullBytecodeRequest, PullBytecodeResponse, TcAttachInfo, TracepointAttachInfo,
        UnloadRequest, UnloadResponse, UprobeAttachInfo, XdpAttachInfo,
    },
    TcProceedOn, XdpProceedOn,
};
use log::warn;
use tokio::sync::{mpsc, mpsc::Sender, oneshot};
use tonic::{Request, Response, Status};

use crate::command::{
    Command, KprobeProgram, LoadArgs, Program, ProgramData, PullBytecodeArgs, TcProgram,
    TracepointProgram, UnloadArgs, UprobeProgram, XdpProgram,
};

#[derive(Debug)]
pub struct BpfdLoader {
    tx: Sender<Command>,
}

impl BpfdLoader {
    pub(crate) fn new(tx: mpsc::Sender<Command>) -> BpfdLoader {
        BpfdLoader { tx }
    }
}

#[tonic::async_trait]
impl Bpfd for BpfdLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
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
            Location::Image(i) => crate::command::Location::Image(i.into()),
            Location::File(p) => crate::command::Location::File(p),
        };

        let data = ProgramData::new(
            bytecode_source,
            common.name,
            common.metadata,
            common.global_data,
            common.map_owner_id,
        );

        let load_args = LoadArgs {
            program: match request.attach_info.unwrap() {
                load_request::AttachInfo::XdpAttachInfo(attach) => Program::Xdp(XdpProgram::new(
                    data,
                    attach.priority,
                    attach.iface,
                    XdpProceedOn::from_int32s(attach.proceed_on)
                        .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                )),
                load_request::AttachInfo::TcAttachInfo(attach) => {
                    let direction = attach
                        .direction
                        .try_into()
                        .map_err(|_| Status::aborted("direction is not a string"))?;
                    Program::Tc(TcProgram::new(
                        data,
                        attach.priority,
                        attach.iface,
                        TcProceedOn::from_int32s(attach.proceed_on)
                            .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                        direction,
                    ))
                }
                load_request::AttachInfo::TracepointAttachInfo(attach) => {
                    Program::Tracepoint(TracepointProgram::new(data, attach.tracepoint))
                }
                load_request::AttachInfo::KprobeAttachInfo(attach) => {
                    Program::Kprobe(KprobeProgram::new(
                        data,
                        attach.fn_name,
                        attach.offset,
                        attach.retprobe,
                        attach.namespace,
                    ))
                }
                load_request::AttachInfo::UprobeAttachInfo(attach) => {
                    Program::Uprobe(UprobeProgram::new(
                        data,
                        attach.fn_name,
                        attach.offset,
                        attach.target,
                        attach.retprobe,
                        attach.pid,
                        attach.namespace,
                    ))
                }
            },
            responder: resp_tx,
        };

        // Send the GET request
        self.tx.send(Command::Load(load_args)).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(id) => Ok(Response::new(LoadResponse { id })),
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
        let request = request.into_inner();
        let id = request.id;

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Unload(UnloadArgs {
            id,
            responder: resp_tx,
        });

        // Send the GET request
        self.tx.send(cmd).await.unwrap();

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

        // Send the GET request
        self.tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(results) => {
                    for r in results {
                        let program_type = r.kind() as u32;

                        // initial prog type filtering
                        if let Some(p) = request.get_ref().program_type {
                            if program_type != p {
                                continue;
                            }
                        }

                        let kernel_info = r
                            .kernel_info()
                            .expect("kernel info should be set for all loaded programs");

                        let mut reply_entry = ListResult {
                            id: kernel_info.id,
                            name: r.name().to_owned(),
                            attach_info: None,
                            location: None,
                            program_type,
                            metadata: HashMap::new(),
                            global_data: HashMap::new(),
                            map_owner_id: None,
                            map_pin_path: String::new(),
                            map_used_by: Vec::new(),
                            loaded_at: kernel_info.loaded_at.clone(),
                            tag: kernel_info.tag.clone(),
                            gpl_compatible: kernel_info.gpl_compatible,
                            map_ids: kernel_info.map_ids.clone(),
                            btf_id: kernel_info.btf_id,
                            bytes_xlated: kernel_info.bytes_xlated,
                            jited: kernel_info.jited,
                            bytes_jited: kernel_info.bytes_jited,
                            bytes_memlock: kernel_info.bytes_memlock,
                            verified_insns: kernel_info.verified_insns,
                        };

                        match r.data() {
                            // program is of type Unsupported
                            Err(_) => {
                                if !request.get_ref().bpfd_programs_only() {
                                    reply.results.push(reply_entry);
                                }
                                continue;
                            }
                            // Bpfd Program
                            Ok(data) => {
                                // prog metadata filtering
                                let mut meta_match = true;
                                for (key, value) in &request.get_ref().match_metadata {
                                    if let Some(v) = data.metadata().get(key) {
                                        if *value != *v {
                                            meta_match = false;
                                            break;
                                        }
                                    } else {
                                        meta_match = false;
                                        break;
                                    }
                                }

                                if !meta_match {
                                    continue;
                                }
                                // populate id
                                reply_entry.id = data
                                    .id()
                                    .expect("Every Bpf Program should have a kernel id");

                                reply_entry.map_pin_path = data
                                    .map_pin_path()
                                    .map_or(String::new(), |v| v.to_str().unwrap().to_string());

                                reply_entry.global_data = data.global_data().to_owned();

                                reply_entry.metadata = data.metadata().to_owned();

                                reply_entry.map_used_by = data
                                    .maps_used_by()
                                    .map_or(vec![], |m| m.iter().map(|m| m.to_string()).collect());

                                reply_entry.map_owner_id = data.map_owner_id();

                                // populate bpfd location
                                reply_entry.location = match r.location() {
                                    Some(l) => match l {
                                        crate::command::Location::Image(m) => {
                                            Some(list_result::Location::Image(
                                                bpfd_api::v1::BytecodeImage {
                                                    url: m.get_url().to_string(),
                                                    image_pull_policy: m
                                                        .get_pull_policy()
                                                        .to_owned()
                                                        as i32,
                                                    // Never dump Plaintext Credentials
                                                    username: String::new(),
                                                    password: String::new(),
                                                },
                                            ))
                                        }
                                        crate::command::Location::File(m) => {
                                            Some(list_result::Location::File(m.to_string()))
                                        }
                                    },
                                    None => {
                                        // skip programs not owned by bpfd
                                        if request.get_ref().bpfd_programs_only() {
                                            continue;
                                        }
                                        None
                                    }
                                };

                                reply_entry.attach_info = match r.clone() {
                                    Program::Xdp(p) => {
                                        Some(AttachInfo::XdpAttachInfo(XdpAttachInfo {
                                            priority: p.priority,
                                            iface: p.iface,
                                            position: p.current_position.unwrap_or(0) as i32,
                                            proceed_on: p.proceed_on.as_action_vec(),
                                        }))
                                    }
                                    Program::Tc(p) => {
                                        Some(AttachInfo::TcAttachInfo(TcAttachInfo {
                                            priority: p.priority,
                                            iface: p.iface,
                                            position: p.current_position.unwrap_or(0) as i32,
                                            direction: p.direction.to_string(),
                                            proceed_on: p.proceed_on.as_action_vec(),
                                        }))
                                    }
                                    Program::Tracepoint(p) => Some(
                                        AttachInfo::TracepointAttachInfo(TracepointAttachInfo {
                                            tracepoint: p.tracepoint,
                                        }),
                                    ),
                                    Program::Kprobe(p) => {
                                        Some(AttachInfo::KprobeAttachInfo(KprobeAttachInfo {
                                            fn_name: p.fn_name,
                                            offset: p.offset,
                                            retprobe: p.retprobe,
                                            namespace: p.namespace,
                                        }))
                                    }
                                    Program::Uprobe(p) => {
                                        Some(AttachInfo::UprobeAttachInfo(UprobeAttachInfo {
                                            fn_name: p.fn_name,
                                            offset: p.offset,
                                            target: p.target,
                                            retprobe: p.retprobe,
                                            pid: p.pid,
                                            namespace: p.namespace,
                                        }))
                                    }
                                    Program::Unsupported(_) => None,
                                };

                                reply.results.push(reply_entry)
                            }
                        }
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

    async fn pull_bytecode(
        &self,
        request: tonic::Request<PullBytecodeRequest>,
    ) -> std::result::Result<tonic::Response<PullBytecodeResponse>, tonic::Status> {
        let request = request.into_inner();
        let image = match request.image {
            Some(i) => i.into(),
            None => return Err(Status::aborted("Empty pull_bytecode request received")),
        };
        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::PullBytecode(PullBytecodeArgs {
            image,
            responder: resp_tx,
        });

        self.tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(_) => {
                    let reply = PullBytecodeResponse {};
                    Ok(Response::new(reply))
                }
                Err(e) => {
                    warn!("BPFD pull_bytecode error: {:#?}", e);
                    Err(Status::aborted(format!("{e}")))
                }
            },

            Err(e) => {
                warn!("RPC pull_bytecode error: {:#?}", e);
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
    async fn test_pull_bytecode() {
        let (tx, rx) = mpsc::channel(32);
        let loader = BpfdLoader::new(tx.clone());

        let request = PullBytecodeRequest {
            image: Some(bpfd_api::v1::BytecodeImage {
                url: String::from("quay.io/bpfd-bytecode/xdp_pass:latest"),
                image_pull_policy: bpfd_api::ImagePullPolicy::Always.into(),
                username: String::from("someone"),
                password: String::from("secret"),
            }),
        };

        tokio::spawn(async move { mock_serve(rx).await });

        let res = loader.pull_bytecode(Request::new(request)).await;
        assert!(res.is_ok());
    }

    async fn mock_serve(mut rx: Receiver<Command>) {
        while let Some(cmd) = rx.recv().await {
            match cmd {
                Command::Load(args) => args.responder.send(Ok(9999)).unwrap(),
                Command::Unload(args) => args.responder.send(Ok(())).unwrap(),
                Command::List { responder, .. } => responder.send(Ok(vec![])).unwrap(),
                Command::PullBytecode(args) => args.responder.send(Ok(())).unwrap(),
            }
        }
    }
}
