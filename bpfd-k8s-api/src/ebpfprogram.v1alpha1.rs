use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// Generate the Kubernetes wrapper struct `Ebpfprogram` from our Spec and Status struct
///
/// This provides a hook for generating the CRD yaml (in crdgen.rs)
#[derive(CustomResource, Deserialize, Serialize, Clone, Debug, JsonSchema)]
#[kube(kind = "EbpfProgram", group = "bpfd.io", version = "v1alpha1", namespaced)]
#[kube(status = "EbpfProgramStatus", shortname = "bpfprog")]
pub struct EbpfProgramSpec {
    #[serde(rename = "type")]
    pub program_type: String,
    pub name: String,
    pub interface: Option<String>,
    pub cgroup: Option<String>,
    pub path: Option<String>, 
    pub image: Option<String>, 
    pub sectionname: String, 
    pub priority: i32,
}

/// The status object of `EbpfProgram`
#[derive(Deserialize, Serialize, Clone, Debug, JsonSchema)]
pub struct EbpfProgramStatus {
    /// Field to denote if the last sync was successful or failed.
    /// Will be empty if there is no status.
    #[serde(rename = "syncStatus")]
    //#[serde(default, skip_serializing_if = "Option::is_none")]
    pub sync_status: Option<String>,
}
