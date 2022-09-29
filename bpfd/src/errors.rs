// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::io;

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
    #[error("No programs loaded to requested interface")]
    NoProgramsLoaded,
    #[error("Invalid ID")]
    InvalidID,
    #[error("Not authorized")]
    NotAuthorized,
    #[error("Invalid Interface")]
    InvalidInterface,
    #[error("Unable to pin link")]
    UnableToPin,
    #[error("Unable to cleanup")]
    UnableToCleanup {
        #[from]
        io_error: io::Error,
    },
    #[error("Unsupported ProgramType")]
    UnsuportedProgramType,
    #[error("{0} is not a valid program type")]
    InvalidProgramType(String),
}
