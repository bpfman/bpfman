// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::path::Path;

use async_trait::async_trait;
use aya::maps::MapData;
use bpfd_csi::v1::{
    controller_server::{Controller, ControllerServer},
    node_server::{Node, NodeServer},
    ControllerExpandVolumeRequest, ControllerExpandVolumeResponse,
    ControllerGetCapabilitiesRequest, ControllerGetCapabilitiesResponse,
    ControllerGetVolumeRequest, ControllerGetVolumeResponse, ControllerModifyVolumeRequest,
    ControllerModifyVolumeResponse, ControllerPublishVolumeRequest,
    ControllerPublishVolumeResponse, ControllerUnpublishVolumeRequest,
    ControllerUnpublishVolumeResponse, CreateSnapshotRequest, CreateSnapshotResponse,
    CreateVolumeRequest, CreateVolumeResponse, DeleteSnapshotRequest, DeleteSnapshotResponse,
    DeleteVolumeRequest, DeleteVolumeResponse, GetCapacityRequest, GetCapacityResponse,
    ListSnapshotsRequest, ListSnapshotsResponse, ListVolumesRequest, ListVolumesResponse,
    NodeExpandVolumeRequest, NodeExpandVolumeResponse, NodeGetCapabilitiesRequest,
    NodeGetCapabilitiesResponse, NodeGetInfoRequest, NodeGetInfoResponse,
    NodeGetVolumeStatsRequest, NodeGetVolumeStatsResponse, NodePublishVolumeRequest,
    NodePublishVolumeResponse, NodeStageVolumeRequest, NodeStageVolumeResponse,
    NodeUnpublishVolumeRequest, NodeUnpublishVolumeResponse, NodeUnstageVolumeRequest,
    NodeUnstageVolumeResponse, ValidateVolumeCapabilitiesRequest,
    ValidateVolumeCapabilitiesResponse,
};
use log::info;
use tonic::{transport::Server, Request, Response, Status};

use crate::{serve::shutdown_handler, utils::create_bpffs};
pub(crate) struct StorageManager {
    csi_controller: CsiController,
    csi_node: CsiNode,
}

struct CsiController {}

#[async_trait]
impl Controller for CsiController {
    async fn create_volume(
        &self,
        _request: Request<CreateVolumeRequest>,
    ) -> std::result::Result<Response<CreateVolumeResponse>, Status> {
        todo!()
    }

    async fn delete_volume(
        &self,
        _request: Request<DeleteVolumeRequest>,
    ) -> std::result::Result<Response<DeleteVolumeResponse>, Status> {
        todo!()
    }
    async fn controller_publish_volume(
        &self,
        _request: Request<ControllerPublishVolumeRequest>,
    ) -> std::result::Result<Response<ControllerPublishVolumeResponse>, Status> {
        todo!()
    }
    async fn controller_unpublish_volume(
        &self,
        _request: Request<ControllerUnpublishVolumeRequest>,
    ) -> std::result::Result<Response<ControllerUnpublishVolumeResponse>, Status> {
        todo!()
    }
    async fn validate_volume_capabilities(
        &self,
        _request: Request<ValidateVolumeCapabilitiesRequest>,
    ) -> std::result::Result<Response<ValidateVolumeCapabilitiesResponse>, Status> {
        todo!()
    }
    async fn list_volumes(
        &self,
        _request: Request<ListVolumesRequest>,
    ) -> std::result::Result<Response<ListVolumesResponse>, Status> {
        todo!()
    }
    async fn get_capacity(
        &self,
        _request: Request<GetCapacityRequest>,
    ) -> std::result::Result<Response<GetCapacityResponse>, Status> {
        todo!()
    }
    async fn controller_get_capabilities(
        &self,
        _request: Request<ControllerGetCapabilitiesRequest>,
    ) -> std::result::Result<Response<ControllerGetCapabilitiesResponse>, Status> {
        todo!()
    }
    async fn create_snapshot(
        &self,
        _request: Request<CreateSnapshotRequest>,
    ) -> std::result::Result<Response<CreateSnapshotResponse>, Status> {
        todo!()
    }
    async fn delete_snapshot(
        &self,
        _request: Request<DeleteSnapshotRequest>,
    ) -> std::result::Result<Response<DeleteSnapshotResponse>, Status> {
        todo!()
    }
    async fn list_snapshots(
        &self,
        _request: Request<ListSnapshotsRequest>,
    ) -> std::result::Result<Response<ListSnapshotsResponse>, Status> {
        todo!()
    }
    async fn controller_expand_volume(
        &self,
        _request: Request<ControllerExpandVolumeRequest>,
    ) -> std::result::Result<Response<ControllerExpandVolumeResponse>, Status> {
        todo!()
    }
    async fn controller_get_volume(
        &self,
        _request: Request<ControllerGetVolumeRequest>,
    ) -> std::result::Result<Response<ControllerGetVolumeResponse>, Status> {
        todo!()
    }
    async fn controller_modify_volume(
        &self,
        _request: Request<ControllerModifyVolumeRequest>,
    ) -> std::result::Result<Response<ControllerModifyVolumeResponse>, Status> {
        todo!()
    }
}

struct CsiNode {}

#[async_trait]
impl Node for CsiNode {
    async fn node_stage_volume(
        &self,
        _request: Request<NodeStageVolumeRequest>,
    ) -> std::result::Result<Response<NodeStageVolumeResponse>, tonic::Status> {
        todo!()
    }
    async fn node_unstage_volume(
        &self,
        _request: Request<NodeUnstageVolumeRequest>,
    ) -> std::result::Result<Response<NodeUnstageVolumeResponse>, tonic::Status> {
        todo!()
    }
    async fn node_publish_volume(
        &self,
        _request: Request<NodePublishVolumeRequest>,
    ) -> std::result::Result<Response<NodePublishVolumeResponse>, tonic::Status> {
        todo!()
    }
    async fn node_unpublish_volume(
        &self,
        _request: Request<NodeUnpublishVolumeRequest>,
    ) -> std::result::Result<Response<NodeUnpublishVolumeResponse>, tonic::Status> {
        todo!()
    }
    async fn node_get_volume_stats(
        &self,
        _request: Request<NodeGetVolumeStatsRequest>,
    ) -> std::result::Result<Response<NodeGetVolumeStatsResponse>, tonic::Status> {
        todo!()
    }
    async fn node_expand_volume(
        &self,
        _request: Request<NodeExpandVolumeRequest>,
    ) -> std::result::Result<Response<NodeExpandVolumeResponse>, tonic::Status> {
        todo!()
    }
    async fn node_get_capabilities(
        &self,
        _request: Request<NodeGetCapabilitiesRequest>,
    ) -> std::result::Result<Response<NodeGetCapabilitiesResponse>, tonic::Status> {
        todo!()
    }
    async fn node_get_info(
        &self,
        _request: Request<NodeGetInfoRequest>,
    ) -> std::result::Result<Response<NodeGetInfoResponse>, tonic::Status> {
        todo!()
    }
}

impl StorageManager {
    pub fn new() -> Self {
        let csi_controller = CsiController {};
        let csi_node = CsiNode {};
        Self {
            csi_controller,
            csi_node,
        }
    }

    pub async fn run(self) {
        let addr = "[::1]:50052".parse().unwrap();
        let controller_service = ControllerServer::new(self.csi_controller);
        let node_service = NodeServer::new(self.csi_node);
        let serve = Server::builder()
            .add_service(controller_service)
            .add_service(node_service)
            .serve_with_shutdown(addr, shutdown_handler());

        tokio::spawn(async move {
            info!("CSI Plugin Listening on {addr}");
            if let Err(e) = serve.await {
                eprintln!("Error = {e:?}");
            }
            info!("Shutdown CSI Plugin {}", addr);
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
