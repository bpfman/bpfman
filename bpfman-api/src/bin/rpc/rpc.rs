// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{path::PathBuf, sync::Arc};

use anyhow::{anyhow, bail};
use bpfman::types::{
    AttachInfo, FentryProgram, FexitProgram, KprobeProgram, ListFilter, Location, Program,
    ProgramData, TcProceedOn, TcProgram, TcxProgram, TracepointProgram, UprobeProgram,
    XdpProceedOn, XdpProgram,
};
use bpfman_api::v1::{
    AttachRequest, AttachResponse, BpfmanProgramType, DetachRequest, DetachResponse, GetRequest,
    GetResponse, ListRequest, ListResponse, LoadRequest, LoadResponse, LoadResponseInfo,
    ProgSpecificInfo, PullBytecodeRequest, PullBytecodeResponse, UnloadRequest, UnloadResponse,
    attach_info::Info, bpfman_server::Bpfman, bytecode_location::Location as RpcLocation,
    list_response::ListResult,
};
use log::error;
use tokio::sync::Mutex;
use tonic::{Request, Response, Status};

use crate::AsyncBpfman;

pub struct BpfmanLoader {
    lock: Arc<Mutex<AsyncBpfman>>,
}

impl BpfmanLoader {
    pub(crate) fn new(lock: Arc<Mutex<AsyncBpfman>>) -> BpfmanLoader {
        BpfmanLoader { lock }
    }
}

impl BpfmanLoader {
    async fn do_load(&self, request: Request<LoadRequest>) -> anyhow::Result<LoadResponse> {
        let request = request.into_inner();

        let bytecode_source = match request
            .bytecode
            .ok_or(anyhow!("missing bytecode info"))?
            .location
            .ok_or(anyhow!("missing location"))?
        {
            RpcLocation::Image(i) => Location::Image(i.into()),
            RpcLocation::File(p) => Location::File(p),
        };

        let programs : Vec<Result<Program, anyhow::Error>> = request.info.iter().map(|info| {
            let data = ProgramData::new(
                bytecode_source.clone(),
                info.name.clone(),
                request.metadata.clone(),
                request.global_data.clone(),
                request.map_owner_id,
            )?;

            let program = match info.program_type() {
                BpfmanProgramType::Xdp => Program::Xdp(XdpProgram::new(data)?),
                BpfmanProgramType::Tc => Program::Tc(TcProgram::new(data)?),
                BpfmanProgramType::Tcx => Program::Tcx(TcxProgram::new(data)?),
                BpfmanProgramType::Tracepoint => Program::Tracepoint(TracepointProgram::new(data)?),
                BpfmanProgramType::Kprobe => Program::Kprobe(KprobeProgram::new(data)?),
                BpfmanProgramType::Uprobe => Program::Uprobe(UprobeProgram::new(data)?),
                BpfmanProgramType::Fentry => {
                    if let Some(ProgSpecificInfo {
                        info: Some(bpfman_api::v1::prog_specific_info::Info::FentryLoadInfo(fentry)),
                    }) = &info.info
                    {
                        Program::Fentry(FentryProgram::new(data, fentry.fn_name.clone())?)
                    } else {
                        bail!("missing FentryInfo");
                    }
                }
                BpfmanProgramType::Fexit => {
                    if let Some(ProgSpecificInfo {
                        info: Some(bpfman_api::v1::prog_specific_info::Info::FexitLoadInfo(fexit)),
                    }) = &info.info
                    {
                        Program::Fexit(FexitProgram::new(data, fexit.fn_name.clone())?)
                    } else {
                        bail!("missing FexitInfo");
                    }
                }
            };
            Ok(program)
        }).collect();

        // Check if any of the programs failed to be created
        for p in programs.iter() {
            if let Err(e) = p {
                bail!("{e}");
            }
        }

        let bpfman_lock = self.lock.lock().await;
        let add_prog_result = bpfman_lock
            .add_programs(programs.into_iter().map(|p| p.unwrap()).collect())
            .await?;

        let mut load_program_info = vec![];
        for p in add_prog_result.iter() {
            let info = p.try_into()?;
            let kernel_info = p.try_into()?;
            load_program_info.push(LoadResponseInfo {
                info: Some(info),
                kernel_info: Some(kernel_info),
            });
        }
        let reply_entry = LoadResponse {
            programs: load_program_info,
        };
        Ok(reply_entry)
    }

    async fn do_unload(&self, request: Request<UnloadRequest>) -> anyhow::Result<UnloadResponse> {
        let reply = UnloadResponse {};
        let request = request.into_inner();
        let bpfman_lock = self.lock.lock().await;
        bpfman_lock.remove_program(request.id).await?;
        Ok(reply)
    }

    async fn do_get(&self, request: Request<GetRequest>) -> anyhow::Result<GetResponse> {
        let request = request.into_inner();
        let id = request.id;
        let bpfman_lock = self.lock.lock().await;
        let program = bpfman_lock.get_program(id).await?;

        let reply_entry = GetResponse {
            info: if let Program::Unsupported(_) = program {
                None
            } else {
                Some((&program).try_into()?)
            },
            kernel_info: Some((&program).try_into()?),
        };
        Ok(reply_entry)
    }

    async fn do_list(&self, request: Request<ListRequest>) -> anyhow::Result<ListResponse> {
        let mut reply = ListResponse { results: vec![] };

        let filter = ListFilter::new(
            request.get_ref().program_type,
            request.get_ref().match_metadata.clone(),
            request.get_ref().bpfman_programs_only(),
        );

        let bpfman_lock = self.lock.lock().await;

        // Await the response
        for r in bpfman_lock
            .list_programs(filter)
            .await
            .map_err(|e| Status::aborted(format!("failed to list programs: {e}")))?
        {
            // Populate the response with the Program Info and the Kernel Info.
            let reply_entry = ListResult {
                info: if let Program::Unsupported(_) = r {
                    None
                } else {
                    Some((&r).try_into()?)
                },
                kernel_info: Some((&r).try_into()?),
            };
            reply.results.push(reply_entry)
        }
        Ok(reply)
    }

    async fn do_pull_bytecode(
        &self,
        request: tonic::Request<PullBytecodeRequest>,
    ) -> anyhow::Result<PullBytecodeResponse> {
        let request = request.into_inner();
        let image = match request.image {
            Some(i) => i.into(),
            None => bail!("Empty pull_bytecode request received"),
        };
        let bpfman_lock = self.lock.lock().await;
        bpfman_lock.pull_bytecode(image).await?;

        let reply = PullBytecodeResponse {};
        Ok(reply)
    }

    async fn do_attach(
        &self,
        request: tonic::Request<AttachRequest>,
    ) -> anyhow::Result<AttachResponse> {
        let request = request.into_inner();

        let attach_info = if let Some(info) = request.attach {
            match info.info {
                Some(Info::XdpAttachInfo(i)) => AttachInfo::Xdp {
                    priority: i.priority,
                    iface: i.iface,
                    proceed_on: XdpProceedOn::from_int32s(i.proceed_on)
                        .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                    netns: i.netns.map(PathBuf::from),
                    metadata: i.metadata,
                },
                Some(Info::TcAttachInfo(i)) => AttachInfo::Tc {
                    priority: i.priority,
                    iface: i.iface,
                    direction: i.direction,
                    proceed_on: TcProceedOn::from_int32s(i.proceed_on)
                        .map_err(|_| Status::aborted("failed to parse proceed_on"))?,
                    netns: i.netns.map(PathBuf::from),
                    metadata: i.metadata,
                },
                Some(Info::TcxAttachInfo(i)) => AttachInfo::Tcx {
                    priority: i.priority,
                    iface: i.iface,
                    direction: i.direction,
                    netns: i.netns.map(PathBuf::from),
                    metadata: i.metadata,
                },
                Some(Info::TracepointAttachInfo(i)) => AttachInfo::Tracepoint {
                    tracepoint: i.tracepoint,
                    metadata: i.metadata,
                },
                Some(Info::KprobeAttachInfo(i)) => AttachInfo::Kprobe {
                    fn_name: i.fn_name,
                    offset: i.offset,
                    container_pid: i.container_pid,
                    metadata: i.metadata,
                },
                Some(Info::UprobeAttachInfo(i)) => AttachInfo::Uprobe {
                    fn_name: i.fn_name,
                    offset: i.offset,
                    target: i.target,
                    pid: i.pid,
                    container_pid: i.container_pid,
                    metadata: i.metadata,
                },
                Some(Info::FentryAttachInfo(i)) => AttachInfo::Fentry {
                    metadata: i.metadata,
                },
                Some(Info::FexitAttachInfo(i)) => AttachInfo::Fexit {
                    metadata: i.metadata,
                },
                None => bail!("missing attach_info"),
            }
        } else {
            bail!("missing attach_info");
        };

        let bpfman_lock = self.lock.lock().await;
        let link_id = bpfman_lock.attach(request.id, attach_info).await?;

        Ok(AttachResponse { link_id })
    }

    async fn do_detach(&self, request: Request<DetachRequest>) -> anyhow::Result<DetachResponse> {
        let request = request.into_inner();
        let bpfman_lock = self.lock.lock().await;
        bpfman_lock.detach(request.link_id).await?;

        Ok(DetachResponse {})
    }
}

#[tonic::async_trait]
impl Bpfman for BpfmanLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
        self.do_load(request)
            .await
            .map_err(|e| {
                error!("Error in load: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }

    async fn unload(
        &self,
        request: Request<UnloadRequest>,
    ) -> Result<Response<UnloadResponse>, Status> {
        self.do_unload(request)
            .await
            .map_err(|e| {
                error!("Error in get: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }

    async fn get(&self, request: Request<GetRequest>) -> Result<Response<GetResponse>, Status> {
        self.do_get(request)
            .await
            .map_err(|e| {
                error!("Error in get: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }

    async fn attach(
        &self,
        request: Request<AttachRequest>,
    ) -> Result<Response<AttachResponse>, Status> {
        self.do_attach(request)
            .await
            .map_err(|e| {
                error!("Error in attach: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }

    async fn detach(
        &self,
        request: Request<DetachRequest>,
    ) -> Result<Response<DetachResponse>, Status> {
        self.do_detach(request)
            .await
            .map_err(|e| {
                error!("Error in detach: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }

    async fn list(&self, request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        self.do_list(request)
            .await
            .map_err(|e| {
                error!("Error in list: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }

    async fn pull_bytecode(
        &self,
        request: tonic::Request<PullBytecodeRequest>,
    ) -> std::result::Result<tonic::Response<PullBytecodeResponse>, tonic::Status> {
        self.do_pull_bytecode(request)
            .await
            .map_err(|e| {
                error!("Error in pull: {e}");
                Status::aborted(format!("{e}"))
            })
            .map(Response::new)
    }
}
