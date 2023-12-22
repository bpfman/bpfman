// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use oci_distribution::{
    client::{ClientConfig, ImageData},
    errors::OciDistributionError,
    manifest::OciImageManifest,
    secrets::RegistryAuth,
    Client as OciClient, Reference,
};
use tokio::runtime::Runtime;

pub struct Client {
    inner: OciClient,
    rt: Runtime,
}

impl Client {
    pub fn new(config: ClientConfig) -> Result<Self, anyhow::Error> {
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()?;
        let inner = OciClient::new(config);
        Ok(Self { inner, rt })
    }

    pub fn pull_manifest_and_config(
        &mut self,
        image: &Reference,
        auth: &RegistryAuth,
    ) -> Result<(OciImageManifest, String, String), OciDistributionError> {
        self.rt
            .block_on(async { self.inner.pull_manifest_and_config(image, auth).await })
    }

    pub fn pull(
        &mut self,
        image: &Reference,
        auth: &RegistryAuth,
        accepted_media_types: Vec<&str>,
    ) -> Result<ImageData, OciDistributionError> {
        self.rt
            .block_on(async { self.inner.pull(image, auth, accepted_media_types).await })
    }
}
