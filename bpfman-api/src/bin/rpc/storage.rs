// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    fs::{create_dir_all, remove_dir_all, remove_file},
    os::unix::fs::chown,
    path::Path,
    sync::Arc,
};

use anyhow::{Context, Result};
use async_trait::async_trait;
use aya::maps::MapData;
use bpfman::{
    types::ListFilter,
    utils::{SOCK_MODE, create_bpffs, set_dir_permissions, set_file_permissions},
};
use bpfman_csi::v1::{
    GetPluginCapabilitiesRequest, GetPluginCapabilitiesResponse, GetPluginInfoRequest,
    GetPluginInfoResponse, NodeExpandVolumeRequest, NodeExpandVolumeResponse,
    NodeGetCapabilitiesRequest, NodeGetCapabilitiesResponse, NodeGetInfoRequest,
    NodeGetInfoResponse, NodeGetVolumeStatsRequest, NodeGetVolumeStatsResponse,
    NodePublishVolumeRequest, NodePublishVolumeResponse, NodeServiceCapability,
    NodeStageVolumeRequest, NodeStageVolumeResponse, NodeUnpublishVolumeRequest,
    NodeUnpublishVolumeResponse, NodeUnstageVolumeRequest, NodeUnstageVolumeResponse, ProbeRequest,
    ProbeResponse,
    identity_server::{Identity, IdentityServer},
    node_server::{Node, NodeServer},
    node_service_capability, volume_capability,
};
use log::{debug, error, info, warn};
use nix::mount::{MsFlags, mount, umount};
use tokio::{
    net::UnixListener,
    sync::{Mutex, broadcast},
};
use tokio_stream::wrappers::UnixListenerStream;
use tonic::{Request, Response, Status, transport::Server};

use crate::AsyncBpfman;

const DRIVER_NAME: &str = "csi.bpfman.io";
const MAPS_KEY: &str = "csi.bpfman.io/maps";
const PROGRAM_KEY: &str = "csi.bpfman.io/program";
const OPERATOR_PROGRAM_KEY: &str = "bpfman.io/ProgramName";
const RTPATH_BPFMAN_CSI_SOCKET: &str = "/run/bpfman/csi/csi.sock";
const RTDIR_BPFMAN_CSI_FS: &str = "/run/bpfman/csi/fs";
// Node Publish Volume Error code constant mirrored from: https://github.com/container-storage-interface/spec/blob/master/spec.md#nodepublishvolume-errors
const NPV_NOT_FOUND: i32 = 5;
const OWNER_READ_WRITE: u32 = 0o0750;

pub struct StorageManager {
    csi_identity: CsiIdentity,
    csi_node: CsiNode,
}

struct CsiIdentity {
    name: String,
    version: String,
}

struct CsiNode {
    node_id: String,
    db_lock: Arc<Mutex<AsyncBpfman>>,
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
        let fs_group = volume_cap.as_ref().and_then(|volume_capability| {
            volume_capability
                .access_type
                .as_ref()
                .and_then(|at| match at {
                    volume_capability::AccessType::Mount(v) => Some(&v.volume_mount_group),
                    _ => None,
                })
        });

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
                read_only: {read_only}, \n
                fs_group: {fs_group:?}"
        );

        let bpfman_lock = self.db_lock.lock().await;
        match (
            volume_context.get(MAPS_KEY),
            volume_context.get(PROGRAM_KEY),
        ) {
            (Some(m), Some(program_name)) => {
                let maps: Vec<&str> = m.split(',').collect();

                // Find the Program with the specified *Program CRD name
                let prog_data = bpfman_lock
                    .list_programs(ListFilter::default())
                    .await
                    .map_err(|e| Status::aborted(format!("failed list programs: {e}")))?
                    .into_iter()
                    .find(|p| {
                        p.get_data()
                            .get_metadata()
                            .expect("unable to get program metadata")
                            .get(OPERATOR_PROGRAM_KEY)
                            == Some(program_name)
                    })
                    .ok_or(Status::new(
                        NPV_NOT_FOUND.into(),
                        format!("Bpfman Program {program_name} not found"),
                    ))?;

                let core_map_path = prog_data
                    .get_data()
                    .get_map_pin_path()
                    .map_err(|e| Status::aborted(format!("failed to get map_pin_path: {e}")))?
                    .expect("map pin path should be set since the program is already loaded")
                    .to_owned();

                // Create the Target Path if it doesn't exist
                let target = Path::new(target_path);
                if !target.exists() {
                    create_dir_all(target).map_err(|e| {
                        Status::new(
                            NPV_NOT_FOUND.into(),
                            format!("failed creating target path {target_path:?}: {e}"),
                        )
                    })?;
                    set_dir_permissions(target_path, OWNER_READ_WRITE);
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

                // Allow unprivileged container to access the bpffs
                if let Some(fs_group) = fs_group {
                    debug!("Setting GID of bpffs {} to {fs_group}", path.display());
                    chown(path, None, fs_group.parse().ok())?;
                };

                // Load the desired maps from the fs and re-pin to new fs.
                maps.iter().try_for_each(|m| {
                    debug!("Loading map {m} from {core_map_path:?}");
                    let map = MapData::from_pin(core_map_path.join(m)).map_err(|e| {
                        Status::new(
                            NPV_NOT_FOUND.into(),
                            format!("map {m} not found in {program_name}'s pinned maps: {e}"),
                        )
                    })?;
                    debug!("Re-pinning map {m} to {path:?}");
                    let map_path = path.join(m);
                    map.pin(&map_path).map_err(|e| {
                        Status::new(
                            NPV_NOT_FOUND.into(),
                            format!(
                                "failed re-pinning map {m} for {program_name}'s bpf-fs: {e:#?}"
                            ),
                        )
                    })?;

                    // Ensure unprivileged container access to bpffs pins
                    if let Some(fs_group) = fs_group {
                        debug!(
                            "Setting GID and permissions of map {} to {fs_group} and 0660",
                            map_path.display()
                        );
                        chown(&map_path, None, fs_group.parse().ok())?;
                        set_file_permissions(&map_path, 0o0660)
                    };
                    Ok::<(), Status>(())
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
                        r#type: node_service_capability::rpc::Type::VolumeMountGroup.into(),
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
    pub fn new(db_lock: Arc<Mutex<AsyncBpfman>>) -> Self {
        const VERSION: &str = env!("CARGO_PKG_VERSION");
        let node_id = std::env::var("KUBE_NODE_NAME")
            .expect("cannot start bpfman csi driver if KUBE_NODE_NAME not set");

        let csi_identity = CsiIdentity {
            name: DRIVER_NAME.to_string(),
            version: VERSION.to_string(),
        };

        let csi_node = CsiNode { node_id, db_lock };

        Self {
            csi_node,
            csi_identity,
        }
    }

    pub async fn run(self, mut shutdown_channel: broadcast::Receiver<()>) {
        create_dir_all(RTDIR_BPFMAN_CSI_FS)
            .context("unable to create CSI socket directory")
            .expect("cannot create csi filesystem");

        let path: &Path = Path::new(RTPATH_BPFMAN_CSI_SOCKET);
        // Listen on Unix socket
        if path.exists() {
            // Attempt to remove the socket, since bind fails if it exists
            remove_file(path).expect("Panicked cleaning up stale csi socket");
        }

        let uds = UnixListener::bind(path)
            .unwrap_or_else(|_| panic!("failed to bind {RTPATH_BPFMAN_CSI_SOCKET}"));
        let uds_stream = UnixListenerStream::new(uds);
        set_file_permissions(Path::new(RTPATH_BPFMAN_CSI_SOCKET), SOCK_MODE);

        let node_service = NodeServer::new(self.csi_node);
        let identity_service = IdentityServer::new(self.csi_identity);
        let serve = Server::builder()
            .add_service(node_service)
            .add_service(identity_service)
            .serve_with_incoming_shutdown(uds_stream, async move {
                match shutdown_channel.recv().await {
                    Ok(()) => debug!("Unix Socket: Received shutdown signal"),
                    Err(e) => error!("Error receiving shutdown signal {:?}", e),
                };
            });

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
