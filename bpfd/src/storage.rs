// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, fs::remove_file, path::Path};

use async_trait::async_trait;
use aya::maps::MapData;
use bpfd_api::util::directories::STPATH_BPFD_CSI_SOCKET;
use bpfd_csi::v1::{
    identity_server::{Identity, IdentityServer},
    node_server::{Node, NodeServer},
    node_service_capability, GetPluginCapabilitiesRequest, GetPluginCapabilitiesResponse,
    GetPluginInfoRequest, GetPluginInfoResponse, NodeExpandVolumeRequest, NodeExpandVolumeResponse,
    NodeGetCapabilitiesRequest, NodeGetCapabilitiesResponse, NodeGetInfoRequest,
    NodeGetInfoResponse, NodeGetVolumeStatsRequest, NodeGetVolumeStatsResponse,
    NodePublishVolumeRequest, NodePublishVolumeResponse, NodeServiceCapability,
    NodeStageVolumeRequest, NodeStageVolumeResponse, NodeUnpublishVolumeRequest,
    NodeUnpublishVolumeResponse, NodeUnstageVolumeRequest, NodeUnstageVolumeResponse, ProbeRequest,
    ProbeResponse,
};
use log::info;
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::{transport::Server, Request, Response, Status};

use crate::{
    serve::shutdown_handler,
    utils::{create_bpffs, set_file_permissions, SOCK_MODE},
};

const DRIVER_NAME: &str = "csi.bpfd.dev";

pub(crate) struct StorageManager {
    csi_identity: CsiIdentity,
    csi_node: CsiNode,
}

struct CsiIdentity {
    name: String,
    version: String,
}

struct CsiNode {
    node_id: String,
}

#[async_trait]
impl Identity for CsiIdentity {
    async fn get_plugin_info(
        &self,
        _request: Request<GetPluginInfoRequest>,
    ) -> Result<Response<GetPluginInfoResponse>, Status> {
        return Ok(Response::new(GetPluginInfoResponse {
            name: self.name.clone(),
            vendor_version: self.version.clone(),
            manifest: HashMap::new(),
        }));
    }

    async fn probe(
        &self,
        _request: Request<ProbeRequest>,
    ) -> Result<Response<ProbeResponse>, Status> {
        return Ok(Response::new(ProbeResponse {
            ..Default::default()
        }));
    }

    // Actual caps are defined in the CSIDriver K8s resource.
    async fn get_plugin_capabilities(
        &self,
        _request: Request<GetPluginCapabilitiesRequest>,
    ) -> Result<Response<GetPluginCapabilitiesResponse>, Status> {
        return Ok(Response::new(GetPluginCapabilitiesResponse {
            ..Default::default()
        }));
    }
}

#[async_trait]
impl Node for CsiNode {
    async fn node_stage_volume(
        &self,
        _request: Request<NodeStageVolumeRequest>,
    ) -> std::result::Result<Response<NodeStageVolumeResponse>, tonic::Status> {
        return Err(Status::unimplemented(""));
    }
    async fn node_unstage_volume(
        &self,
        _request: Request<NodeUnstageVolumeRequest>,
    ) -> std::result::Result<Response<NodeUnstageVolumeResponse>, tonic::Status> {
        return Err(Status::unimplemented(""));
    }
    async fn node_publish_volume(
        &self,
        request: Request<NodePublishVolumeRequest>,
    ) -> std::result::Result<Response<NodePublishVolumeResponse>, tonic::Status> {
        let req = request.get_ref();
        let volume_cap = &req.volume_capability;
        let volume_id = &req.volume_id;
        let target_path = &req.target_path;
        let volume_context = &req.volume_context;
        let read_only = &req.readonly;

        info!(
            "Received publish volume request with :\n\
        volume_caps: {volume_cap:#?},\n\
        volume_id: {volume_id},\n\
        target_path: {target_path},\n\
        volume_context: {volume_context:#?},\n\
        read_only: {read_only}\n\
        Sleeping for 60 seconds"
        );

        tokio::time::sleep(std::time::Duration::from_secs(60)).await;

        Ok(Response::new(NodePublishVolumeResponse {}))
    }
    async fn node_unpublish_volume(
        &self,
        request: Request<NodeUnpublishVolumeRequest>,
    ) -> std::result::Result<Response<NodeUnpublishVolumeResponse>, tonic::Status> {
        let req = request.get_ref();
        let volume_id = &req.volume_id;
        let target_path = &req.target_path;
        info!(
            "Received unpublish volume request with :\n\
        volume_id: {volume_id},\n\
        target_path: {target_path}"
        );

        Ok(Response::new(NodeUnpublishVolumeResponse {}))
    }
    async fn node_get_volume_stats(
        &self,
        _request: Request<NodeGetVolumeStatsRequest>,
    ) -> std::result::Result<Response<NodeGetVolumeStatsResponse>, tonic::Status> {
        return Err(Status::unimplemented(""));
    }
    async fn node_expand_volume(
        &self,
        _request: Request<NodeExpandVolumeRequest>,
    ) -> std::result::Result<Response<NodeExpandVolumeResponse>, tonic::Status> {
        return Err(Status::unimplemented(""));
    }
    async fn node_get_capabilities(
        &self,
        _request: Request<NodeGetCapabilitiesRequest>,
    ) -> std::result::Result<Response<NodeGetCapabilitiesResponse>, tonic::Status> {
        return Ok(Response::new(NodeGetCapabilitiesResponse {
            capabilities: vec![NodeServiceCapability {
                r#type: Some(node_service_capability::Type::Rpc(
                    node_service_capability::Rpc {
                        r#type: node_service_capability::rpc::Type::Unknown.into(),
                    },
                )),
            }],
        }));
    }
    // see https://github.com/container-storage-interface/spec/blob/master/spec.md#nodegetinfo
    // for more information.
    async fn node_get_info(
        &self,
        _request: Request<NodeGetInfoRequest>,
    ) -> std::result::Result<Response<NodeGetInfoResponse>, tonic::Status> {
        return Ok(Response::new(NodeGetInfoResponse {
            node_id: self.node_id.clone(),

            max_volumes_per_node: 0,
            accessible_topology: None,
        }));
    }
}

impl StorageManager {
    pub fn new() -> Self {
        const VERSION: &str = env!("CARGO_PKG_VERSION");
        let node_id = std::env::var("KUBE_NODE_NAME")
            .expect("cannot start bpfd csi driver if KUBE_NODE_NAME not set");

        let csi_identity = CsiIdentity {
            name: DRIVER_NAME.to_string(),
            version: VERSION.to_string(),
        };
        let csi_node = CsiNode { node_id };

        Self {
            csi_node,
            csi_identity,
        }
    }

    pub async fn run(self) {
        let path: &Path = Path::new(STPATH_BPFD_CSI_SOCKET);
        // Listen on Unix socket
        if path.exists() {
            // Attempt to remove the socket, since bind fails if it exists
            remove_file(path).expect("Panicked cleaning up stale csi socket");
        }

        let uds = UnixListener::bind(path)
            .unwrap_or_else(|_| panic!("failed to bind {STPATH_BPFD_CSI_SOCKET}"));
        let uds_stream = UnixListenerStream::new(uds);
        set_file_permissions(STPATH_BPFD_CSI_SOCKET, SOCK_MODE).await;

        let node_service = NodeServer::new(self.csi_node);
        let identity_service = IdentityServer::new(self.csi_identity);
        let serve = Server::builder()
            .add_service(node_service)
            .add_service(identity_service)
            .serve_with_incoming_shutdown(uds_stream, shutdown_handler());

        tokio::spawn(async move {
            info!("CSI Plugin Listening on {}", path.display());
            if let Err(e) = serve.await {
                eprintln!("Error = {e:?}");
            }
            info!("Shutdown CSI Plugin Handler {}", path.display());
        });
    }

    #[allow(dead_code)] // TODO: Remove this when the storage manager is fully implemented
    fn create_bpffs(&self, path: &Path) -> anyhow::Result<()> {
        create_bpffs(
            path.as_os_str()
                .to_str()
                .expect("unable to convert path to str"),
        )
    }

    #[allow(dead_code)] // TODO: Remove this when the storage manager is fully implemented
    fn pin_map_to_bpffs(
        &self,
        source_object: &mut MapData,
        dest_bpffs: &Path,
    ) -> anyhow::Result<()> {
        source_object
            .pin(dest_bpffs)
            .map_err(|e| anyhow::anyhow!("unable to pin map to bpffs: {}", e))?;
        Ok(())
    }
}
