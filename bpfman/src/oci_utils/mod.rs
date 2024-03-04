// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub(crate) mod cosign;
pub mod image_manager;

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
    #[error("{0}: {1}")]
    DatabaseError(String, String),
}
