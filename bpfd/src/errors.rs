// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use thiserror::Error;
use tokio::sync::oneshot;

#[derive(Debug, Error)]
pub enum BpfdError {
    #[error("An error occurred. {0}")]
    Error(String),
    #[error(transparent)]
    BpfProgramError(#[from] aya::programs::ProgramError),
    #[error(transparent)]
    BpfLoadError(#[from] aya::BpfError),
    #[error("Unable to find a valid program with section name {0}")]
    SectionNameNotValid(String),
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
    BpfBytecodeError(#[from] anyhow::Error),
    #[error("Bytecode image has section name: {image_prog_name} isn't equal to the provided section name {provided_prog_name}")]
    BytecodeMetaDataMismatch {
        image_prog_name: String,
        provided_prog_name: String,
    },
    #[error("Unable to delete program {0}")]
    BpfdProgramDeleteError(#[source] anyhow::Error),
    #[error(transparent)]
    RpcError(#[from] oneshot::error::RecvError),
    #[error("Failed to pin map {0}")]
    UnableToPinMap(#[source] aya::pin::PinError),
}
