// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    fs::{create_dir_all, remove_dir_all, remove_file},
    path::Path,
};

use anyhow::{Context, Result};
use async_trait::async_trait;
use aya::maps::MapData;
use bpfman_api::util::directories::{RTDIR_BPFMAN_CSI_FS, RTPATH_BPFMAN_CSI_SOCKET};
use bpfman_csi::v1::{
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
use log::{debug, info, warn};
use nix::mount::{mount, umount, MsFlags};
use tokio::{
    net::UnixListener,
    sync::{mpsc, mpsc::Sender, oneshot},
};
use tokio_stream::wrappers::UnixListenerStream;
use tonic::{transport::Server, Request, Response, Status};

use crate::{
    command::Command,
    serve::shutdown_handler,
    utils::{create_bpffs, set_dir_permissions, set_file_permissions, SOCK_MODE},
};

const DRIVER_NAME: &str = "csi.bpfman.io";
const MAPS_KEY: &str = "csi.bpfman.io/maps";
const PROGRAM_KEY: &str = "csi.bpfman.io/program";
const OPERATOR_PROGRAM_KEY: &str = "bpfman.io/ProgramName";
// Node Publish Volume Error code constant mirrored from: https://github.com/container-storage-interface/spec/blob/master/spec.md#nodepublishvolume-errors
const NPV_NOT_FOUND: i32 = 5;
const OWNER_READ_WRITE: u32 = 0o0750;

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
    tx: Sender<Command>,
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
        // TODO (astoycos) support readonly bpf pins.
        let read_only = &req.readonly;

        debug!(
            "Received publish volume request with :\n\
                volume_caps: {volume_cap:#?},\n\
                volume_id: {volume_id},\n\
                target_path: {target_path},\n\
                volume_context: {volume_context:#?},\n\
                read_only: {read_only}"
        );

        match (
            volume_context.get(MAPS_KEY),
            volume_context.get(PROGRAM_KEY),
        ) {
            (Some(m), Some(program_name)) => {
                let maps: Vec<&str> = m.split(',').collect();

                // Get the program information from the BpfManager
                let (resp_tx, resp_rx) = oneshot::channel();
                let cmd = Command::List { responder: resp_tx };

                // Send the LIST request
                self.tx.send(cmd).await.unwrap();

                // Await the response
                let core_map_path = match resp_rx.await {
                    Ok(res) => match res {
                        // Find the Program with the specified *Program CRD name
                        Ok(results) => {
                            let prog_data = results
                                .into_iter()
                                .find(|p| {
                                    if let Ok(data) = p.data() {
                                        data.metadata().get(OPERATOR_PROGRAM_KEY)
                                            == Some(program_name)
                                    } else {
                                        false
                                    }
                                })
                                .ok_or(Status::new(
                                    NPV_NOT_FOUND.into(),
                                    format!("Bpfman Program {program_name} not found"),
                                ))?;
                            Ok(prog_data
                                .data().expect("program data is valid because we just checked it")
                                .map_pin_path()
                                .expect("map pin path should be set since the program is already loaded")
                                .to_owned())
                        }
                        Err(_) => Err(Status::new(
                            NPV_NOT_FOUND.into(),
                            format!("Bpfman Program {program_name} not found"),
                        )),
                    },
                    Err(_) => Err(Status::new(
                        NPV_NOT_FOUND.into(),
                        format!("Unable to list bpfman programs {program_name} not found"),
                    )),
                }?;

                // Create the Target Path if it doesn't exist
                let target = Path::new(target_path);
                if !target.exists() {
                    create_dir_all(target).map_err(|e| {
                        Status::new(
                            NPV_NOT_FOUND.into(),
                            format!("failed creating target path {target_path:?}: {e}"),
                        )
                    })?;
                    set_dir_permissions(target_path, OWNER_READ_WRITE).await;
                }

                // Make a new bpf fs specifically for the pod.
                let path = &Path::new(RTDIR_BPFMAN_CSI_FS).join(volume_id);

                // Volume_id is unique to the instance of the pod, if it get's restarted it will
                // be new.
                create_dir_all(path)?;

                create_bpffs(
                    path.as_os_str()
                        .to_str()
                        .expect("unable to convert path to str"),
                )
                .map_err(|e| {
                    Status::new(
                        NPV_NOT_FOUND.into(),
                        format!("failed creating bpf-fs for pod {volume_id}: {e}"),
                    )
                })?;

                // Load the desired maps from the fs and re-pin to new fs.
                // TODO(astoycos) all aya calls should be completed by main bpfManager thread.
                maps.iter().try_for_each(|m| {
                    debug!("Loading map {m} from {core_map_path:?}");
                    let map = MapData::from_pin(core_map_path.join(m)).map_err(|e| {
                        Status::new(
                            NPV_NOT_FOUND.into(),
                            format!("map {m} not found in {program_name}'s pinned maps: {e}"),
                        )
                    })?;
                    debug!("Re-pinning map {m} to {path:?}");
                    map.pin(path.join(m)).map_err(|e| {
                        Status::new(
                            NPV_NOT_FOUND.into(),
                            format!(
                                "failed re-pinning map {m} for {program_name}'s bpf-fs: {e:#?}"
                            ),
                        )
                    })
                })?;

                // mount the bpffs into the container
                mount_fs_in_container(path.to_str().unwrap(), target_path).map_err(|e| {
                    Status::new(
                        NPV_NOT_FOUND.into(),
                        format!("failed mounting bpffs {path:?} to container {target_path}: {e}"),
                    )
                })?;

                Ok(Response::new(NodePublishVolumeResponse {}))
            }
            (_, Some(program)) => {
                let err_msg = format!("No {MAPS_KEY} set in volume context from {program}");
                warn!("{}", err_msg);

                Err(Status::new(NPV_NOT_FOUND.into(), err_msg))
            }
            (Some(m), _) => {
                let err_msg =
                    format!("No {PROGRAM_KEY} set in volume context in for requested maps {m}");
                warn!("{}", err_msg);

                Err(Status::new(NPV_NOT_FOUND.into(), err_msg))
            }
            (_, _) => {
                let err_msg = format!("No {MAPS_KEY} or {PROGRAM_KEY} set in volume context");
                warn!("{}", err_msg);

                Err(Status::new(NPV_NOT_FOUND.into(), err_msg))
            }
        }
    }
    async fn node_unpublish_volume(
        &self,
        request: Request<NodeUnpublishVolumeRequest>,
    ) -> std::result::Result<Response<NodeUnpublishVolumeResponse>, tonic::Status> {
        let req = request.get_ref();
        let volume_id = &req.volume_id;
        let target_path = &req.target_path;
        debug!(
            "Received unpublish volume request with :\n\
                volume_id: {volume_id},\n\
                target_path: {target_path}"
        );

        // unmount bpffs from the container
        unmount(target_path).map_err(|e| {
            Status::new(
                5.into(),
                format!("Failed to unmount bpffs from container at {target_path}: {e}"),
            )
        })?;

        // unmount the bpffs
        let path = &Path::new(RTDIR_BPFMAN_CSI_FS).join(volume_id);
        unmount(path.to_str().unwrap()).map_err(|e| {
            Status::new(
                5.into(),
                format!("Failed to unmount bpffs at {path:?}: {e}"),
            )
        })?;

        let path = &Path::new(path);
        if path.exists() {
            remove_dir_all(path).map_err(|e| {
                Status::new(5.into(), format!("Failed to remove bpffs at {path:?}: {e}"))
            })?;
        }

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
    pub fn new(tx: mpsc::Sender<Command>) -> Self {
        const VERSION: &str = env!("CARGO_PKG_VERSION");
        let node_id = std::env::var("KUBE_NODE_NAME")
            .expect("cannot start bpfman csi driver if KUBE_NODE_NAME not set");

        let csi_identity = CsiIdentity {
            name: DRIVER_NAME.to_string(),
            version: VERSION.to_string(),
        };

        let csi_node = CsiNode { node_id, tx };

        Self {
            csi_node,
            csi_identity,
        }
    }

    pub async fn run(self) {
        let path: &Path = Path::new(RTPATH_BPFMAN_CSI_SOCKET);
        // Listen on Unix socket
        if path.exists() {
            // Attempt to remove the socket, since bind fails if it exists
            remove_file(path).expect("Panicked cleaning up stale csi socket");
        }

        let uds = UnixListener::bind(path)
            .unwrap_or_else(|_| panic!("failed to bind {RTPATH_BPFMAN_CSI_SOCKET}"));
        let uds_stream = UnixListenerStream::new(uds);
        set_file_permissions(RTPATH_BPFMAN_CSI_SOCKET, SOCK_MODE).await;

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
    fn pin_map_to_bpffs(&self, source_object: &MapData, dest_bpffs: &Path) -> anyhow::Result<()> {
        source_object
            .pin(dest_bpffs)
            .map_err(|e| anyhow::anyhow!("unable to pin map to bpffs: {}", e))?;
        Ok(())
    }
}

pub(crate) fn unmount(directory: &str) -> anyhow::Result<()> {
    debug!("Unmounting fs at {directory}");
    umount(directory).with_context(|| format!("unable to unmount fs at {directory}"))
}

pub(crate) fn mount_fs_in_container(path: &str, target_path: &str) -> anyhow::Result<()> {
    debug!("Mounting {path} at {target_path}");
    let flags = MsFlags::MS_BIND;
    mount::<str, str, str, str>(Some(path), target_path, None, flags, None)
        .with_context(|| format!("unable to mount bpffs {path} in container at {target_path}"))
}
