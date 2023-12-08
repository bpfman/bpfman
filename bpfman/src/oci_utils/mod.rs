// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub(crate) mod cosign;
pub(crate) mod image_manager;

pub(crate) use image_manager::ImageManager;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ImageError {
    #[error("Failed to Parse bytecode Image URL: {0}")]
    InvalidImageUrl(#[source] oci_distribution::ParseError),
    #[error("Failed to pull bytecode Image manifest: {0}")]
    ImageManifestPullFailure(#[source] oci_distribution::errors::OciDistributionError),
    #[error("Failed to pull bytecode Image: {0}")]
    BytecodeImagePullFailure(#[source] oci_distribution::errors::OciDistributionError),
    #[error("Failed to extract bytecode from Image")]
    BytecodeImageExtractFailure,
    #[error(transparent)]
    ByteCodeImageProcessFailure(#[from] anyhow::Error),
    #[error("BytecodeImage not found: {0}")]
    ByteCodeImageNotfound(String),
    #[error("Failed to flush database to disk: {0}")]
    DatabaseFlushFailure(#[source] sled::Error),
    #[error("Failed to write to database: {0}")]
    DatabaseWriteFailure(#[source] sled::Error),
    #[error("Failed to read to database: {0}")]
    DatabaseReadFailure(#[source] sled::Error),
    #[error("Failed to parse data from database: {0}")]
    DatabaseParseFailure(#[source] anyhow::Error),
    #[error("Database key {0} does not exist")]
    DatabaseKeyDoesNotExist(String),
}
