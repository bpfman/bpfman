use std::{path::PathBuf, sync::Arc};

use anyhow::{anyhow, bail, Context as _};
// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use bpfman::types::{
    FentryProgram, FexitProgram, KprobeProgram, ListFilter, Location, Program, ProgramData,
    TcProceedOn, TcProgram, TcxProgram, TracepointProgram, UprobeProgram, XdpProceedOn, XdpProgram,
};
use bpfman_api::v1::{
    attach_info::Info, bpfman_server::Bpfman, bytecode_location::Location as RpcLocation,
    list_response::ListResult, FentryAttachInfo, FexitAttachInfo, GetRequest, GetResponse,
    KprobeAttachInfo, ListRequest, ListResponse, LoadRequest, LoadResponse, PullBytecodeRequest,
    PullBytecodeResponse, TcAttachInfo, TcxAttachInfo, TracepointAttachInfo, UnloadRequest,
    UnloadResponse, UprobeAttachInfo, XdpAttachInfo,
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

        let data = ProgramData::new(
            bytecode_source,
            request.name,
            request.metadata,
            request.global_data,
            request.map_owner_id,
        )
        .context("failed to create ProgramData")?;

        let program = match request
            .attach
            .ok_or(anyhow!("missing attach info"))?
            .info
            .ok_or(anyhow!("missing info"))?
        {
            Info::XdpAttachInfo(XdpAttachInfo {
                priority,
                iface,
                position: _,
                proceed_on,
                netns,
            }) => Program::Xdp(XdpProgram::new(
                data,
                priority,
                iface,
                XdpProceedOn::from_int32s(proceed_on)?,
                netns.map(PathBuf::from),
            )?),
            Info::TcAttachInfo(TcAttachInfo {
                priority,
                iface,
                position: _,
                direction,
                proceed_on,
                netns,
            }) => {
                let direction = direction.try_into()?;
                Program::Tc(TcProgram::new(
                    data,
                    priority,
                    iface,
                    TcProceedOn::from_int32s(proceed_on)?,
                    direction,
                    netns.map(PathBuf::from),
                )?)
            }
            Info::TcxAttachInfo(TcxAttachInfo {
                priority,
                iface,
                position: _,
                direction,
                netns,
            }) => {
                let direction = direction.try_into()?;
                Program::Tcx(TcxProgram::new(
                    data,
                    priority,
                    iface,
                    direction,
                    netns.map(PathBuf::from),
                )?)
            }
            Info::TracepointAttachInfo(TracepointAttachInfo { tracepoint }) => {
                Program::Tracepoint(TracepointProgram::new(data, tracepoint)?)
            }
            Info::KprobeAttachInfo(KprobeAttachInfo {
                fn_name,
                offset,
                retprobe,
                container_pid,
            }) => Program::Kprobe(KprobeProgram::new(
                data,
                fn_name,
                offset,
                retprobe,
                container_pid,
            )?),
            Info::UprobeAttachInfo(UprobeAttachInfo {
                fn_name,
                offset,
                target,
                retprobe,
                pid,
                container_pid,
            }) => Program::Uprobe(UprobeProgram::new(
                data,
                fn_name,
                offset,
                target,
                retprobe,
                pid,
                container_pid,
            )?),
            Info::FentryAttachInfo(FentryAttachInfo { fn_name }) => {
                Program::Fentry(FentryProgram::new(data, fn_name)?)
            }
            Info::FexitAttachInfo(FexitAttachInfo { fn_name }) => {
                Program::Fexit(FexitProgram::new(data, fn_name)?)
            }
        };

        let bpfman_lock = self.lock.lock().await;
        let program = bpfman_lock.add_program(program).await?;

        let reply_entry = LoadResponse {
            info: Some((&program).try_into()?),
            kernel_info: Some((&program).try_into()?),
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
