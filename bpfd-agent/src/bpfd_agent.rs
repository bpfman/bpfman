use std::sync::Arc;

use anyhow::Result;
use bpfd_api::v1::{
    load_request::AttachType, loader_client::LoaderClient, LoadRequest, NetworkMultiAttach,
    ProgramType, UnloadRequest,
};
use bpfd_k8s_api::v1alpha1::{EbpfProgram, EbpfProgramStatus};
use kube::{
    api::{Api, Patch, PatchParams},
    runtime::controller::Action,
    Client, Resource, ResourceExt,
};
use serde_json::json;
use thiserror::Error;
use tokio::time::Duration;
use tonic::transport::Channel;
use tracing::*;

use crate::finalizer;

#[derive(Debug, Error)]
pub enum Error {
    #[error("Failed to reconcile EbpfProgram: {0}")]
    EbpfProgramReconcileFailed(#[source] kube::Error),
    #[error("Failed to parse EbpfProgram: {0}")]
    EbpfProgramParseFailed(#[source] bpfd_api::ParseError),
    #[error("Failed to load EbpfProgram: {0}")]
    EbpfProgramLoadFailed(#[source] tonic::Status),
    #[error("MissingObjectKey: {0}")]
    MissingObjectKey(&'static str),
}

// Context for our reconciler
#[derive(Clone)]
pub struct Context {
    /// Kubernetes client
    pub client: Client,
    /// BPFD client,
    pub bpfd_client: LoaderClient<Channel>,
    // /// Prometheus metrics
    // metrics: Metrics,
}

/// Action to be taken upon an EbpfProgram resource during reconciliation
enum EbpfProgramAction {
    /// Create the EbpfProgram
    Create,
    /// Delete the EbpfProgram
    Delete,
    /// This EbpfProgram resource is in desired state and requires no actions to be taken
    NoOp,
}

/// Controller triggers this whenever an EbpfProgram is managed
pub async fn reconcile(ebpf_program: Arc<EbpfProgram>, ctx: Arc<Context>) -> Result<Action, Error> {
    let program_name = ebpf_program
        .metadata
        .name
        .as_ref()
        .ok_or(Error::MissingObjectKey(".metadata.name"))?;

    let client = ctx.client.clone();
    let ns = ebpf_program
        .metadata
        .namespace
        .as_ref()
        .ok_or(Error::MissingObjectKey(".metadata.namespace"))?;

    // Make ebpfProgram client
    let ebpf_programs_api: Api<EbpfProgram> = Api::namespaced(client.clone(), ns);

    // Performs action as decided by the `determine_action` function.
    return match determine_action(&ebpf_program) {
        EbpfProgramAction::Create => {
            debug!("Created EbpfProgram: {}", program_name);

            // load the program
            let (uuid, attach_point) = load_ebpfprogram(ebpf_program.clone(), ctx).await?;

            debug!(
                "Loaded program via bpfd, uuid: {} attach_point name {}",
                uuid, attach_point
            );
            // Apply the finalizer first. If that fails, the `?` operator invokes automatic conversion
            // of `kube::Error` to the `Error` defined in this crate.
            finalizer::add(client.clone(), program_name, ns)
                .await
                .map_err(Error::EbpfProgramReconcileFailed)?;

            // always overwrite the annotations with a fresh UUID
            let annotation = Patch::Apply(json!({
                "apiVersion": "bpfd.io/v1alpha1",
                "kind": "EbpfProgram",
                "metadata": {
                    "annotations": {
                        "bpfd.ebpfprogram.io/uuid": uuid,
                        "bpfd.ebpfprogram.io/attach_point": attach_point
                    }
                }
            }));

            // always overwrite status object if load was successful
            let new_status = Patch::Apply(json!({
                "apiVersion": "bpfd.io/v1alpha1",
                "kind": "EbpfProgram",
                "status": EbpfProgramStatus {
                    sync_status: Some("Loaded".to_string())
                }
            }));

            let ps = PatchParams::apply("ebpfprograms.bpfd.io").force();

            ebpf_programs_api
                .patch_status(program_name, &ps, &new_status)
                .await
                .map_err(Error::EbpfProgramReconcileFailed)?;

            ebpf_programs_api
                .patch(program_name, &ps, &annotation)
                .await
                .map_err(Error::EbpfProgramReconcileFailed)?;

            Ok(Action::requeue(Duration::from_secs(10)))
        }

        EbpfProgramAction::Delete => {
            debug!("Deleted EbpfProgram: {}", program_name);

            let annotations = ebpf_program
                .metadata
                .annotations
                .as_ref()
                .ok_or(Error::MissingObjectKey(".metadata.annotations"))?;

            let uuid = annotations.get("bpfd.ebpfprogram.io/uuid");
            let interface = annotations.get("bpfd.ebpfprogram.io/attach_point");
            unload_ebpfprogram(uuid.unwrap(), interface.unwrap(), ctx).await?;

            finalizer::delete(client.clone(), &ebpf_program.name_any(), ns)
                .await
                .map_err(Error::EbpfProgramReconcileFailed)?;
            Ok(Action::await_change()) // Makes no sense to delete after a successful delete, as the resource is gone
        }
        // The resource is already in desired state, do nothing and re-check after 10 seconds
        EbpfProgramAction::NoOp => Ok(Action::requeue(Duration::from_secs(10))),
    };
}

/// The controller triggers this on reconcile errors
pub fn error_policy(_ebpf_program: Arc<EbpfProgram>, _error: &Error, _ctx: Arc<Context>) -> Action {
    Action::requeue(Duration::from_secs(10))
}

/// Resources arrives into reconciliation queue in a certain state. This function looks at
/// the state of given `EbpfProgram` resource and decides which actions needs to be performed.
/// The finite set of possible actions is represented by the `Action` enum.
///
/// # Arguments
/// - `EbpfProgram`: A reference to `EbpfProgram` being reconciled to decide next action upon.
fn determine_action(ebpf_program: &EbpfProgram) -> EbpfProgramAction {
    if ebpf_program.meta().deletion_timestamp.is_some() {
        EbpfProgramAction::Delete
    } else if ebpf_program
        .meta()
        .finalizers
        .as_ref()
        .map_or(true, |finalizers| finalizers.is_empty())
    {
        EbpfProgramAction::Create
    } else {
        EbpfProgramAction::NoOp
    }
}

// load_ebpfprogram is used to communicate with the bpfd daemon also running
// As part of the same daemonset. If successful it returns the UUID of the program
// which will be written in an annotation.
pub async fn load_ebpfprogram(
    ebpf_program: Arc<EbpfProgram>,
    ctx: Arc<Context>,
) -> Result<(String, String), Error> {
    let program_type = ProgramType::try_from(ebpf_program.spec.program_type.clone())
        .map_err(Error::EbpfProgramParseFailed)?;

    let program_name = ebpf_program.spec.name.clone();
    let section_name_flag = ebpf_program.spec.sectionname.clone();
    let priority_flag = ebpf_program.spec.priority;

    // A valid program can only have one attach point, CRD verification
    // should be used to ensure this. Here just choose whicherver
    let attach_point = if ebpf_program.spec.interface.is_some() {
        ebpf_program.spec.interface.clone().unwrap()
    } else {
        ebpf_program.spec.cgroup.clone().unwrap()
    };

    let mut from_image_flag = false;

    let bytecode_location = if ebpf_program.spec.path.is_none() {
        from_image_flag = true;
        ebpf_program.spec.image.clone().unwrap()
    } else {
        ebpf_program.spec.path.clone().unwrap()
    }
    .to_string();
    debug!("Program Location was {}", bytecode_location);

    // TODO add proceed-on to CRD
    let proc_on = Vec::new();

    let request = tonic::Request::new(LoadRequest {
        path: bytecode_location,
        from_image: from_image_flag,
        section_name: section_name_flag,
        program_type: program_type as i32,
        attach_type: Some(AttachType::NetworkMultiAttach(NetworkMultiAttach {
            iface: attach_point,
            priority: priority_flag,
            // Not supported via the kube API yet
            proceed_on: proc_on,
        })),
    });

    debug!("sending request to bpfd {:?}", request);

    let response = ctx
        .bpfd_client
        .clone()
        .load(request)
        .await
        .map_err(Error::EbpfProgramLoadFailed)?
        .into_inner();

    info!(
        "Loaded program: {} with UUID: {}",
        program_name, response.id
    );

    Ok((response.id, ebpf_program.spec.interface.clone().unwrap()))
}

// unload_ebpfprogram is used to communicate with the bpfd daemon also deployed
// as part of the same daemonset. If successful it parses the UUID of the program
// from the ebpfprogram's annotations and removes it from the node.
pub async fn unload_ebpfprogram(
    uuid: &str,
    attach_point: &str,
    ctx: Arc<Context>,
) -> Result<(), Error> {
    let request = tonic::Request::new(UnloadRequest {
        iface: attach_point.to_string(),
        id: uuid.to_string(),
    });
    ctx.bpfd_client
        .clone()
        .unload(request)
        .await
        .map_err(Error::EbpfProgramLoadFailed)?
        .into_inner();
    Ok(())
}
