use bpfd_k8s_api::v1alpha1::EbpfProgram;
use kube::{
    api::{Patch, PatchParams},
    Api, Client, Error,
};
use serde_json::{Value, from_str};

/// Adds a finalizer record into an `EbpfProgram` kind of resource. If the finalizer already exists,
/// this action has no effect.
///
/// # Arguments:
/// - `client` - Kubernetes client to modify the `EbpfProgram` resource with.
/// - `name` - Name of the `EbpfProgram` resource to modify. Existence is not verified
/// - `namespace` - Namespace where the `EbpfProgram` resource with given `name` resides.
///
/// Note: Does not check for resource's existence for simplicity.
pub async fn add(client: Client, name: &str, namespace: &str, finalizer_tag: &str) -> Result<EbpfProgram, Error> {
    let api: Api<EbpfProgram> = Api::namespaced(client, namespace);
    // let finalizer: Value = json!({
    //     "metadata": {
    //         "finalizers": [finalizer_tag]
    //     }
    // });
    //let json = format!(r#"{{"type": "type1", "type2": {}}}"#, var1);
    //let finalizer = serde_json::json!({ "metadata": { "finalizers": [finalizer_tag] } });
    // let finalizer = format!(r#"[
    // { "op": "remove", "path": "/metadata/finalizers/0"}
    // ]"#).unwrap();
    // println!("Patching finalizer");
    // let finalizer = json_patch::PatchOperation::Add(json_patch::AddOperation {
    //     path: "/metadata/finalizers/-".into(),
    //     value: serde_json::json!(finalizer_tag),
    // });
    
    let finalizer = from_str(r#"[
    { "op": "add", "path": "/metadata/finalizers", "value": "temporary-fake"}
    ]"#).unwrap();

    //let patch: Patch<&Value> = Patch::Json(json_patch::Patch(vec![finalizer]));
    let patch: Patch<&Value> = Patch::Json(finalizer);
    api.patch(name, &PatchParams::default(), &patch).await
}

/// Removes a single finalizer from a resource
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
    // let finalizer: Value = json!({
    //     "op": "remove",
    //     "path": "/metadata/finalizers"
    // });

    let finalizer = json_patch::PatchOperation::Remove(json_patch::RemoveOperation {
        path: "/metadata/finalizers/0".into(),
    });

    // let finalizer = from_str(r#"[
    // { "op": "remove", "path": "/metadata/finalizers/0"}
    // ]"#).unwrap();

    let patch: Patch<&Value> = Patch::Json(json_patch::Patch(vec![finalizer]));
    //let patch: Patch<&Value> = Patch::Merge(&finalizer);
    api.patch(name, &PatchParams::default(), &patch).await
}
