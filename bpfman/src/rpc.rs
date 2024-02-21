// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use bpfman_api::{
    v1::{
        attach_info::Info, bpfman_server::Bpfman, bytecode_location::Location,
        list_response::ListResult, GetRequest, GetResponse, KprobeAttachInfo, ListRequest,
        ListResponse, LoadRequest, LoadResponse, PullBytecodeRequest, PullBytecodeResponse,
        TcAttachInfo, TracepointAttachInfo, UnloadRequest, UnloadResponse, UprobeAttachInfo,
        XdpAttachInfo,
    },
    TcProceedOn, XdpProceedOn,
};
use log::warn;
use tokio::sync::{mpsc, mpsc::Sender, oneshot};
use tonic::{Request, Response, Status};

use crate::command::{
    Command, GetArgs, KprobeProgram, ListFilter, LoadArgs, Program, ProgramData, PullBytecodeArgs,
    TcProgram, TracepointProgram, UnloadArgs, UprobeProgram, XdpProgram,
};

#[derive(Debug)]
pub struct BpfmanLoader {
    tx: Sender<Command>,
}

impl BpfmanLoader {
    pub(crate) fn new(tx: mpsc::Sender<Command>) -> BpfmanLoader {
        BpfmanLoader { tx }
    }
}

#[tonic::async_trait]
impl Bpfman for BpfmanLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
        let request = request.into_inner();

        let (resp_tx, resp_rx) = oneshot::channel();

        let bytecode_source = match request
            .bytecode
            .ok_or(Status::aborted("missing bytecode info"))?
            .location
            .ok_or(Status::aborted("missing location"))?
        {
            Location::Image(i) => crate::command::Location::Image(i.into()),
            Location::File(p) => crate::command::Location::File(p),
        };

        let data = ProgramData::new_pre_load(
            bytecode_source,
            request.name,
            request.metadata,
            request.global_data,
            request.map_owner_id,
        )
        .map_err(|e| Status::aborted(format!("failed to create ProgramData: {e}")))?;

        let load_args = LoadArgs {
            program: match request
                .attach
                .ok_or(Status::aborted("missing attach info"))?
                .info
                .ok_or(Status::aborted("missing info"))?
            {
                Info::XdpAttachInfo(XdpAttachInfo {
                    priority,
                    iface,
                    position: _,
                    proceed_on,
                }) => Program::Xdp(
                    XdpProgram::new(
                        data,
                        priority,
                        iface,
                        XdpProceedOn::from_int32s(proceed_on)
                            .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                    )
                    .map_err(|e| Status::aborted(format!("failed to create xdpprogram: {e}")))?,
                ),
                Info::TcAttachInfo(TcAttachInfo {
                    priority,
                    iface,
                    position: _,
                    direction,
                    proceed_on,
                }) => {
                    let direction = direction
                        .try_into()
                        .map_err(|_| Status::aborted("direction is not a string"))?;
                    Program::Tc(
                        TcProgram::new(
                            data,
                            priority,
                            iface,
                            TcProceedOn::from_int32s(proceed_on)
                                .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                            direction,
                        )
                        .map_err(|e| Status::aborted(format!("failed to create tcprogram: {e}")))?,
                    )
                }
                Info::TracepointAttachInfo(TracepointAttachInfo { tracepoint }) => {
                    Program::Tracepoint(
                        TracepointProgram::new(data, tracepoint).map_err(|e| {
                            Status::aborted(format!("failed to create tcprogram: {e}"))
                        })?,
                    )
                }
                Info::KprobeAttachInfo(KprobeAttachInfo {
                    fn_name,
                    offset,
                    retprobe,
                    container_pid,
                }) => Program::Kprobe(
                    KprobeProgram::new(data, fn_name, offset, retprobe, container_pid).map_err(
                        |e| Status::aborted(format!("failed to create kprobeprogram: {e}")),
                    )?,
                ),
                Info::UprobeAttachInfo(UprobeAttachInfo {
                    fn_name,
                    offset,
                    target,
                    retprobe,
                    pid,
                    container_pid,
                }) => Program::Uprobe(
                    UprobeProgram::new(data, fn_name, offset, target, retprobe, pid, container_pid)
                        .map_err(|e| {
                            Status::aborted(format!("failed to create uprobeprogram: {e}"))
                        })?,
                ),
            },
            responder: resp_tx,
        };

        // Send the LOAD request
        // TODO: This channel can be removed. It was in place because main bpfman thread
        //       had the privileges and RPC thread did not. But now that we are unwinding
        //       that, this can call bpfman directly.
        self.tx.send(Command::Load(load_args)).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(program) => {
                    let reply_entry = LoadResponse {
                        info: Some((&program).try_into().map_err(|e| {
                            Status::aborted(format!("convert Program to GRPC program: {e}"))
                        })?),
                        kernel_info: Some((&program).try_into().map_err(|e| {
                            Status::aborted(format!(
                                "convert Program to GRPC kernel program info: {e}"
                            ))
                        })?),
                    };
                    Ok(Response::new(reply_entry))
                }
                Err(e) => {
                    warn!("BPFMAN load error: {:#?}", e);
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
                    warn!("BPFMAN unload error: {}", e);
                    Err(Status::aborted(format!("{e}")))
                }
            },
            Err(e) => {
                warn!("RPC unload error: {}", e);
                Err(Status::aborted(format!("{e}")))
            }
        }
    }

    async fn get(&self, request: Request<GetRequest>) -> Result<Response<GetResponse>, Status> {
        let request = request.into_inner();
        let id = request.id;

        let (resp_tx, resp_rx) = oneshot::channel();
        let cmd = Command::Get(GetArgs {
            id,
            responder: resp_tx,
        });

        // Send the GET request
        self.tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(res) => match res {
                Ok(program) => {
                    let reply_entry = GetResponse {
                        info: if let Program::Unsupported(_) = program {
                            None
                        } else {
                            Some((&program).try_into().map_err(|e| {
                                Status::aborted(format!("failed to get program metadata: {e}"))
                            })?)
                        },
                        kernel_info: match (&program).try_into() {
                            Ok(i) => {
                                if let Program::Unsupported(_) = program {
                                    program.delete().map_err(|e| {
                                        Status::aborted(format!(
                                            "failed to get program metadata: {e}"
                                        ))
                                    })?;
                                };
                                Ok(Some(i))
                            }
                            Err(e) => Err(Status::aborted(format!(
                                "convert Program to GRPC kernel program info: {e}"
                            ))),
                        }?,
                    };
                    Ok(Response::new(reply_entry))
                }
                Err(e) => {
                    warn!("BPFMAN get error: {}", e);
                    Err(Status::aborted(format!("{e}")))
                }
            },
            Err(e) => {
                warn!("RPC get error: {}", e);
                Err(Status::aborted(format!("{e}")))
            }
        }
    }

    async fn list(&self, request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        let mut reply = ListResponse { results: vec![] };

        let (resp_tx, resp_rx) = oneshot::channel();

        let cmd = Command::List {
            responder: resp_tx,
            filter: ListFilter::new(
                request.get_ref().program_type,
                request.get_ref().match_metadata.clone(),
                request.get_ref().bpfman_programs_only(),
            ),
        };

        // Send the List request
        self.tx.send(cmd).await.unwrap();

        // Await the response
        match resp_rx.await {
            Ok(results) => {
                for r in results {
                    // Populate the response with the Program Info and the Kernel Info.
                    let reply_entry = ListResult {
                        info: if let Program::Unsupported(_) = r {
                            None
                        } else {
                            Some((&r).try_into().map_err(|e| {
                                Status::aborted(format!("failed to get program metadata: {e}"))
                            })?)
                        },
                        kernel_info: match (&r).try_into() {
                            Ok(i) => {
                                if let Program::Unsupported(_) = r {
                                    r.delete().map_err(|e| {
                                        Status::aborted(format!(
                                            "failed to get program metadata: {e}"
                                        ))
                                    })?;
                                };
                                Ok(Some(i))
                            }
                            Err(e) => Err(Status::aborted(format!(
                                "convert Program to GRPC kernel program info: {e}"
                            ))),
                        }?,
                    };
                    reply.results.push(reply_entry)
                }
                Ok(Response::new(reply))
            }
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
                    warn!("BPFMAN pull_bytecode error: {:#?}", e);
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
    use std::{collections::HashMap, time::SystemTime};

    use bpfman_api::v1::{
        bytecode_location::Location, AttachInfo, BytecodeLocation, LoadRequest, XdpAttachInfo,
    };
    use tokio::sync::mpsc::Receiver;

    use super::*;

    #[tokio::test]
    async fn test_load_with_valid_id() {
        let (tx, rx) = mpsc::channel(32);
        let loader = BpfmanLoader::new(tx.clone());

        let attach_info = AttachInfo {
            info: Some(Info::XdpAttachInfo(XdpAttachInfo {
                iface: "eth0".to_string(),
                priority: 50,
                position: 0,
                proceed_on: vec![2, 31],
            })),
        };
        let request = LoadRequest {
            bytecode: Some(BytecodeLocation {
                location: Some(Location::Image(bpfman_api::v1::BytecodeImage {
                    url: "quay.io/bpfman-bytecode/xdp:latest".to_string(),
                    ..Default::default()
                })),
            }),
            attach: Some(attach_info),
            ..Default::default()
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
        let loader = BpfmanLoader::new(tx.clone());

        let request = PullBytecodeRequest {
            image: Some(bpfman_api::v1::BytecodeImage {
                url: String::from("quay.io/bpfman-bytecode/xdp_pass:latest"),
                image_pull_policy: bpfman_api::ImagePullPolicy::Always.into(),
                username: Some(String::from("someone")),
                password: Some(String::from("secret")),
            }),
        };

        tokio::spawn(async move { mock_serve(rx).await });

        let res = loader.pull_bytecode(Request::new(request)).await;
        assert!(res.is_ok());
    }

    async fn mock_serve(mut rx: Receiver<Command>) {
        let mut data = ProgramData::new_pre_load(
            crate::command::Location::File("/tmp/fake".to_string()),
            "xdp_pass".to_string(),
            HashMap::new(),
            HashMap::new(),
            None,
        )
        .unwrap();

        // Set kernel info
        data.set_id(0).unwrap();
        data.set_kernel_name("").unwrap();
        data.set_kernel_program_type(0).unwrap();
        data.set_kernel_loaded_at(SystemTime::now()).unwrap();
        data.set_kernel_tag(0).unwrap();
        data.set_kernel_gpl_compatible(true).unwrap();
        data.set_kernel_map_ids(vec![]).unwrap();
        data.set_kernel_btf_id(0).unwrap();
        data.set_kernel_bytes_xlated(0).unwrap();
        data.set_kernel_jited(false).unwrap();
        data.set_kernel_bytes_jited(0).unwrap();
        data.set_kernel_bytes_memlock(0).unwrap();
        data.set_kernel_verified_insns(0).unwrap();

        let program = Program::Xdp(
            XdpProgram::new(data, 0, "eth0".to_string(), XdpProceedOn::default()).unwrap(),
        );

        while let Some(cmd) = rx.recv().await {
            match cmd {
                Command::Load(args) => args.responder.send(Ok(program.clone())).unwrap(),
                Command::Unload(args) => args.responder.send(Ok(())).unwrap(),
                Command::List { responder, .. } => responder.send(vec![]).unwrap(),
                Command::Get(args) => args.responder.send(Ok(program.clone())).unwrap(),
                Command::PullBytecode(args) => args.responder.send(Ok(())).unwrap(),
            }
        }
    }
}
