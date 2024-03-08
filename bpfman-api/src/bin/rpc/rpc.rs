// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
use bpfman::{
    config::Config,
    types::{
        FentryProgram, FexitProgram, KprobeProgram, ListFilter, Location, Program, ProgramData,
        TcProceedOn, TcProgram, TracepointProgram, UprobeProgram, XdpProceedOn, XdpProgram,
    },
    BpfManager,
};
use bpfman_api::v1::{
    attach_info::Info, bpfman_server::Bpfman, bytecode_location::Location as RpcLocation,
    list_response::ListResult, FentryAttachInfo, FexitAttachInfo, GetRequest, GetResponse,
    KprobeAttachInfo, ListRequest, ListResponse, LoadRequest, LoadResponse, PullBytecodeRequest,
    PullBytecodeResponse, TcAttachInfo, TracepointAttachInfo, UnloadRequest, UnloadResponse,
    UprobeAttachInfo, XdpAttachInfo,
};
use tonic::{Request, Response, Status};

pub struct BpfmanLoader {
    config: Config,
}

impl BpfmanLoader {
    pub(crate) fn new(config: Config) -> BpfmanLoader {
        BpfmanLoader { config }
    }
}

#[tonic::async_trait]
impl Bpfman for BpfmanLoader {
    async fn load(&self, request: Request<LoadRequest>) -> Result<Response<LoadResponse>, Status> {
        let mut bpf_manager = BpfManager::new(self.config.clone());
        let request = request.into_inner();

        let bytecode_source = match request
            .bytecode
            .ok_or(Status::aborted("missing bytecode info"))?
            .location
            .ok_or(Status::aborted("missing location"))?
        {
            RpcLocation::Image(i) => Location::Image(i.into()),
            RpcLocation::File(p) => Location::File(p),
        };

        let data = ProgramData::new_pre_load(
            &bpf_manager.root_db,
            bytecode_source,
            request.name,
            request.metadata,
            request.global_data,
            request.map_owner_id,
        )
        .map_err(|e| Status::aborted(format!("failed to create ProgramData: {e}")))?;

        let program = match request
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
            Info::TracepointAttachInfo(TracepointAttachInfo { tracepoint }) => Program::Tracepoint(
                TracepointProgram::new(data, tracepoint)
                    .map_err(|e| Status::aborted(format!("failed to create tcprogram: {e}")))?,
            ),
            Info::KprobeAttachInfo(KprobeAttachInfo {
                fn_name,
                offset,
                retprobe,
                container_pid,
            }) => Program::Kprobe(
                KprobeProgram::new(data, fn_name, offset, retprobe, container_pid)
                    .map_err(|e| Status::aborted(format!("failed to create kprobeprogram: {e}")))?,
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
                    .map_err(|e| Status::aborted(format!("failed to create uprobeprogram: {e}")))?,
            ),
            Info::FentryAttachInfo(FentryAttachInfo { fn_name }) => Program::Fentry(
                FentryProgram::new(data, fn_name)
                    .map_err(|e| Status::aborted(format!("failed to create fentryprogram: {e}")))?,
            ),
            Info::FexitAttachInfo(FexitAttachInfo { fn_name }) => Program::Fexit(
                FexitProgram::new(data, fn_name)
                    .map_err(|e| Status::aborted(format!("failed to create fexitprogram: {e}")))?,
            ),
        };

        let program = bpf_manager
            .add_program(program)
            .await
            .map_err(|e| Status::aborted(format!("{e}")))?;

        let reply_entry =
            LoadResponse {
                info: Some((&program).try_into().map_err(|e| {
                    Status::aborted(format!("convert Program to GRPC program: {e}"))
                })?),
                kernel_info: Some((&program).try_into().map_err(|e| {
                    Status::aborted(format!("convert Program to GRPC kernel program info: {e}"))
                })?),
            };

        Ok(Response::new(reply_entry))
    }

    async fn unload(
        &self,
        request: Request<UnloadRequest>,
    ) -> Result<Response<UnloadResponse>, Status> {
        let mut bpf_manager = BpfManager::new(self.config.clone());

        let reply = UnloadResponse {};
        let request = request.into_inner();

        bpf_manager
            .remove_program(request.id)
            .await
            .map_err(|e| Status::aborted(format!("{e}")))?;

        Ok(Response::new(reply))
    }

    async fn get(&self, request: Request<GetRequest>) -> Result<Response<GetResponse>, Status> {
        let mut bpf_manager = BpfManager::new(self.config.clone());
        let request = request.into_inner();
        let id = request.id;

        let program = bpf_manager
            .get_program(id)
            .map_err(|e| Status::aborted(format!("{e}")))?;

        let reply_entry =
            GetResponse {
                info: if let Program::Unsupported(_) = program {
                    None
                } else {
                    Some((&program).try_into().map_err(|e| {
                        Status::aborted(format!("failed to get program metadata: {e}"))
                    })?)
                },
                kernel_info: Some((&program).try_into().map_err(|e| {
                    Status::aborted(format!("convert Program to GRPC kernel program info: {e}"))
                })?),
            };
        Ok(Response::new(reply_entry))
    }

    async fn list(&self, request: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        let mut bpf_manager = BpfManager::new(self.config.clone());

        let mut reply = ListResponse { results: vec![] };

        let filter = ListFilter::new(
            request.get_ref().program_type,
            request.get_ref().match_metadata.clone(),
            request.get_ref().bpfman_programs_only(),
        );

        // Await the response
        for r in bpf_manager.list_programs(filter) {
            // Populate the response with the Program Info and the Kernel Info.
            let reply_entry = ListResult {
                info: if let Program::Unsupported(_) = r {
                    None
                } else {
                    Some((&r).try_into().map_err(|e| {
                        Status::aborted(format!("failed to get program metadata: {e}"))
                    })?)
                },
                kernel_info: Some((&r).try_into().map_err(|e| {
                    Status::aborted(format!("convert Program to GRPC kernel program info: {e}"))
                })?),
            };
            reply.results.push(reply_entry)
        }
        Ok(Response::new(reply))
    }

    async fn pull_bytecode(
        &self,
        request: tonic::Request<PullBytecodeRequest>,
    ) -> std::result::Result<tonic::Response<PullBytecodeResponse>, tonic::Status> {
        let mut bpf_manager = BpfManager::new(self.config.clone());

        let request = request.into_inner();
        let image = match request.image {
            Some(i) => i.into(),
            None => return Err(Status::aborted("Empty pull_bytecode request received")),
        };

        bpf_manager
            .pull_bytecode(image)
            .await
            .map_err(|e| Status::aborted(format!("{e}")))?;

        let reply = PullBytecodeResponse {};
        Ok(Response::new(reply))
    }
}
