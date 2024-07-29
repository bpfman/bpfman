// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use thiserror::Error;
use tokio::sync::oneshot;
use url::ParseError as urlParseError;

use crate::oci_utils::ImageError;

#[derive(Debug, Error)]
pub enum BpfmanError {
    #[error("An error occurred. {0}")]
    Error(String),
    #[error(transparent)]
    BpfIOError(#[from] std::io::Error),
    #[error(transparent)]
    BpfProgramError(#[from] aya::programs::ProgramError),
    #[error(transparent)]
    BpfLoadError(#[from] aya::EbpfError),
    #[error("Unable to find a valid program with function name {0}")]
    BpfFunctionNameNotValid(String),
    #[error("No room to attach program. Please remove one and try again.")]
    TooManyPrograms,
    #[error("Invalid Interface")]
    InvalidInterface,
    #[error("Failed to pin link {0}")]
    UnableToPinLink(#[source] aya::pin::PinError),
    #[error("Failed to pin program {0}")]
    UnableToPinProgram(#[source] aya::pin::PinError),
    #[error("{0} is not a valid attach point for this program")]
    InvalidAttach(String),
    #[error("dispatcher is not loaded")]
    NotLoaded,
    #[error("dispatcher not required")]
    DispatcherNotRequired,
    #[error(transparent)]
    BpfBytecodeError(#[from] ImageError),
    #[error("Bytecode image bpf function name: {image_prog_name} isn't equal to the provided bpf function name {provided_prog_name}")]
    BytecodeMetaDataMismatch {
        image_prog_name: String,
        provided_prog_name: String,
    },
    #[error("Program {expected_prog_name} not found in bytecode image {bytecode_image} with program names {program_names:?}")]
    ProgramNotFoundInBytecode {
        bytecode_image: String,
        expected_prog_name: String,
        program_names: Vec<String>,
    },
    #[error("Unable to delete program {0}")]
    BpfmanProgramDeleteError(#[source] anyhow::Error),
    #[error(transparent)]
    RpcRecvError(#[from] oneshot::error::RecvError),
    // Use anyhow::Error here since the real error contains a generic <T> reflecting
    // the failed sent item's type
    #[error(transparent)]
    RpcSendError(#[from] anyhow::Error),
    #[error("Failed to pin map {0}")]
    UnableToPinMap(#[source] aya::pin::PinError),
    #[error("Unable to attach {program_type} in container with pid {container_pid}")]
    ContainerAttachError {
        program_type: String,
        container_pid: i32,
    },
    #[error("{0}: {1}")]
    DatabaseError(String, String),
    #[error("Internal error occurred. {0}")]
    InternalError(String),
    #[error(transparent)]
    BtfError(#[from] aya::BtfError),
    #[error("Failed to acquire database lock, please try again later")]
    DatabaseLockError,
}

#[derive(Error, Debug)]
pub enum ParseError {
    #[error("{program} is not a valid program type")]
    InvalidProgramType { program: String },
    #[error("{map} is not a valid map type")]
    InvalidMapType { map: String },
    #[error("{proceedon} is not a valid proceed-on value")]
    InvalidProceedOn { proceedon: String },
    #[error("not a valid direction: {direction}")]
    InvalidDirection { direction: String },
    #[error("Failed to Parse bytecode location: {0}")]
    BytecodeLocationParseFailure(#[source] urlParseError),
    #[error("Invalid bytecode location: {location}")]
    InvalidBytecodeLocation { location: String },
    #[error("Invalid bytecode image pull policy: {pull_policy}")]
    InvalidBytecodeImagePullPolicy { pull_policy: String },
    #[error("{probe} is not a valid probe type")]
    InvalidProbeType { probe: String },
    #[error("Invalid XdpMode: {mode}")]
    InvalidXdpMode { mode: String },
    #[error("Error parsing config file: {0}")]
    ConfigParseError(#[from] toml::de::Error),
}
