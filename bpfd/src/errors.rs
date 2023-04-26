// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use thiserror::Error;

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
    #[error("Invalid ID")]
    InvalidID,
    #[error("Not authorized")]
    NotAuthorized,
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
    #[error("Bytecode image has section name: {image_sec_name} isn't equal to the provided section name {provided_sec_name}")]
    BytecodeMetaDataMismatch {
        image_sec_name: String,
        provided_sec_name: String,
    },
    #[error("Unable to parse passed UUID {0}")]
    PassedUUIDError(#[from] uuid::Error),
    #[error("Passed UUID already in use {0}")]
    PassedUUIDInUse(String),
}
