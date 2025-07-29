// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub(crate) mod cosign;
pub mod image_manager;

use std::sync::LazyLock;

use thiserror::Error;

#[derive(Debug, Error)]
pub enum ImageError {
    #[error("Failed to Parse bytecode Image URL: {0}")]
    InvalidImageUrl(#[source] oci_client::ParseError),
    #[error("Failed to pull bytecode Image manifest: {0}")]
    ImageManifestPullFailure(#[source] oci_client::errors::OciDistributionError),
    #[error("Failed to pull bytecode Image: {0}")]
    BytecodeImagePullFailure(#[source] oci_client::errors::OciDistributionError),
    #[error("Failed to extract bytecode from Image: {0}")]
    BytecodeImageExtractFailure(String),
    #[error(transparent)]
    ByteCodeImageProcessFailure(#[from] anyhow::Error),
    #[error("BytecodeImage not found: {0}")]
    ByteCodeImageNotfound(String),
    #[error("BytecodeImage compromised: {0}")]
    ByteCodeImageCompromised(String),
    #[error("{0}: {1}")]
    DatabaseError(String, String),
    #[error("Failed reading from database at: {0}")]
    DatabaseReadError(String),
    #[error("{0}: {1}")]
    BytecodeImageParseFailure(String, String),
    #[error(transparent)]
    JoinError(#[from] tokio::task::JoinError),
}

pub(crate) fn rt() -> Result<tokio::runtime::Handle, std::io::Error> {
    static RT: LazyLock<tokio::runtime::Runtime> = LazyLock::new(|| {
        tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
            .expect("Failed to create tokio runtime")
    });

    Ok(RT.handle().clone())
}
