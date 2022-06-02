use thiserror::Error;

#[derive(Debug, Error)]
pub enum BpfdError {
    #[error(transparent)]
    BpfProgramError(#[from] aya::programs::ProgramError),
    #[error(transparent)]
    BpfLoadError(#[from] aya::BpfError),
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
}
