use bpfd_k8s_api::v1alpha1::EbpfProgram;
use kube::{
    api::{Patch, PatchParams},
    Api, Client, Error,
};
use serde_json::{json, Value};

/// Adds a finalizer record into an `EbpfProgram` kind of resource. If the finalizer already exists,
/// this action has no effect.
///
/// # Arguments:
/// - `client` - Kubernetes client to modify the `EbpfProgram` resource with.
/// - `name` - Name of the `EbpfProgram` resource to modify. Existence is not verified
/// - `namespace` - Namespace where the `EbpfProgram` resource with given `name` resides.
///
/// Note: Does not check for resource's existence for simplicity.
pub async fn add(client: Client, name: &str, namespace: &str) -> Result<EbpfProgram, Error> {
    let api: Api<EbpfProgram> = Api::namespaced(client, namespace);
    let finalizer: Value = json!({
        "metadata": {
            "finalizers": ["ebpfprogram.bpfd.io/finalizer"]
        }
    });

    let patch: Patch<&Value> = Patch::Merge(&finalizer);
    api.patch(name, &PatchParams::default(), &patch).await
}

/// Removes all finalizers from an `EbpfProgram` resource. If there are no finalizers already, this
/// action has no effect.
///
/// # Arguments:
/// - `client` - Kubernetes client to modify the `EbpfProgram` resource with.
/// - `name` - Name of the `Echo` resource to modify. Existence is not verified
/// - `namespace` - Namespace where the `EbpfProgram` resource with given `name` resides.
///
/// Note: Does not check for resource's existence for simplicity.
pub async fn delete(client: Client, name: &str, namespace: &str) -> Result<EbpfProgram, Error> {
    let api: Api<EbpfProgram> = Api::namespaced(client, namespace);
    let finalizer: Value = json!({
        "metadata": {
            "finalizers": null
        }
    });

    let patch: Patch<&Value> = Patch::Merge(&finalizer);
    api.patch(name, &PatchParams::default(), &patch).await
}
