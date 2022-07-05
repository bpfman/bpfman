// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use thiserror::Error;

#[derive(Debug, Error)]
pub enum BpfdError {
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
    #[error("Map not found")]
    MapNotFound,
    #[error("Map not loaded")]
    MapNotLoaded,
    #[error("Map not deleted")]
    MapNotDeleted,
    #[error("Not authorized")]
    NotAuthorized,
    #[error("No programs left for interface. Failed to Remove Dispatcher")]
    RemoveDispatcher
}
