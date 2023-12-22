// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::str::FromStr;

use anyhow::{anyhow, bail};
use log::{debug, info, warn};
use sigstore::{
    cosign::{
        verification_constraint::VerificationConstraintVec, verify_constraints, ClientBuilder,
        CosignCapabilities,
    },
    errors::SigstoreError::RegistryPullManifestError,
    registry::{Auth, ClientConfig, ClientProtocol, OciReference},
    tuf::SigstoreRepository,
};
use tokio::{runtime::Runtime, task::spawn_blocking};

/// A blocking wrapper around the sigstore cosign client.
pub struct CosignVerifier {
    client: sigstore::cosign::Client,
    rt: Runtime,
}

impl CosignVerifier {
    pub(crate) fn new() -> Result<Self, anyhow::Error> {
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()?;

        let client = rt.block_on(async {
            // We must use spawn_blocking here.
            // See: https://docs.rs/sigstore/0.7.2/sigstore/oauth/openidflow/index.html
            let repo: sigstore::errors::Result<SigstoreRepository> = spawn_blocking(|| {
                info!("Starting Cosign Verifier, downloading data from Sigstore TUF repository");
                sigstore::tuf::SigstoreRepository::fetch(None)
            })
            .await
            .map_err(|e| anyhow!("Error fetching sigstore repository: {}", e))?;

            let repo = repo?;

            let oci_config = ClientConfig {
                protocol: ClientProtocol::Https,
                ..Default::default()
            };

            ClientBuilder::default()
                .with_oci_client_config(oci_config)
                .with_rekor_pub_key(repo.rekor_pub_key())
                .with_fulcio_certs(repo.fulcio_certs())
                .enable_registry_caching()
                .build()
                .map_err(|e| anyhow!("Error building cosign client: {}", e))
        })?;

        Ok(Self { client, rt })
    }

    pub(crate) fn verify(
        &mut self,
        image: &str,
        username: Option<&str>,
        password: Option<&str>,
        allow_unsigned: bool,
    ) -> Result<(), anyhow::Error> {
        debug!("CosignVerifier::verify()");
        let image = OciReference::from_str(image)?;
        let auth = if let (Some(username), Some(password)) = (username, password) {
            Auth::Basic(username.to_string(), password.to_string())
        } else {
            Auth::Anonymous
        };

        self.rt.block_on(async {
            debug!("Triangulating image: {}", image);
            let (cosign_signature_image, source_image_digest) =
                self.client.triangulate(&image, &auth).await?;

            debug!("Getting trusted layers");
            match self
                .client
                .trusted_signature_layers(&auth, &source_image_digest, &cosign_signature_image)
                .await
            {
                Ok(trusted_layers) => {
                    debug!("Found trusted layers");
                    debug!("Verifying constraints");
                    info!("The bytecode image: {} is signed", image);
                    // TODO: Add some constraints here
                    let verification_constraints: VerificationConstraintVec = Vec::new();
                    verify_constraints(&trusted_layers, verification_constraints.iter())
                        .map_err(|e| anyhow!("Error verifying constraints: {}", e))?;
                    Ok(())
                }
                Err(e) => match e {
                    RegistryPullManifestError { .. } => {
                        if !allow_unsigned {
                            bail!("Error triangulating image: {}", e);
                        } else {
                            warn!("The bytecode image: {} is unsigned", image);
                            Ok(())
                        }
                    }
                    _ => {
                        bail!("Error triangulating image: {}", e);
                    }
                },
            }
        })
    }
}
