// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub(crate) mod cosign;
pub mod image_manager;

use lazy_static::lazy_static;
use thiserror::Error;
use tokio::runtime::{Builder as RuntimeBuilder, Handle, Runtime};

#[derive(Debug, Error)]
pub enum ImageError {
    #[error("Failed to Parse bytecode Image URL: {0}")]
    InvalidImageUrl(#[source] oci_distribution::ParseError),
    #[error("Failed to pull bytecode Image manifest: {0}")]
    ImageManifestPullFailure(#[source] oci_distribution::errors::OciDistributionError),
    #[error("Failed to pull bytecode Image: {0}")]
    BytecodeImagePullFailure(#[source] oci_distribution::errors::OciDistributionError),
    #[error("Failed to extract bytecode from Image: {0}")]
    BytecodeImageExtractFailure(String),
    #[error(transparent)]
    ByteCodeImageProcessFailure(#[from] anyhow::Error),
    #[error("BytecodeImage not found: {0}")]
    ByteCodeImageNotfound(String),
    #[error("{0}: {1}")]
    DatabaseError(String, String),
    #[error("{0}: {1}")]
    BytecodeImageParseFailure(String, String),
    #[error(transparent)]
    JoinError(#[from] tokio::task::JoinError),
}

lazy_static! {
    pub(crate) static ref RUNTIME: BlockingWrapper = BlockingWrapper::new();
}

/// A wrapper to handle blocking execution safely in both sync and async contexts.
pub(crate) struct BlockingWrapper {
    handle: Option<Handle>,
    runtime: Option<Runtime>,
}

impl BlockingWrapper {
    /// Creates a new `RuntimeBlocker`, detecting the current runtime if available.
    fn new() -> Self {
        match Handle::try_current() {
            Ok(handle) => Self {
                handle: Some(handle),
                runtime: None,
            },
            Err(_) => {
                let runtime = RuntimeBuilder::new_multi_thread()
                    .worker_threads(1)
                    .enable_all()
                    .build()
                    .expect("Failed to create runtime");
                Self {
                    handle: Some(runtime.handle().clone()),
                    runtime: Some(runtime),
                }
            }
        }
    }

    /// Runs an async function synchronously, choosing the best blocking strategy.
    fn block<F, T>(&self, future: F) -> T
    where
        F: std::future::Future<Output = T>,
    {
        if let Some(handle) = &self.handle {
            // Inside an async runtime, use `block_in_place` to avoid blocking the executor.
            return tokio::task::block_in_place(|| handle.block_on(future));
        }
        if let Some(runtime) = &self.runtime {
            // Inside a runtime, use `block_on` normally.
            return runtime.block_on(future);
        }
        panic!("No runtime available");
    }
}
