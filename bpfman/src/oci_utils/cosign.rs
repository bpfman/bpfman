// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{path::PathBuf, str::FromStr};

use anyhow::{anyhow, bail};
use log::{debug, info, warn};
use sigstore::{
    cosign::{
        verification_constraint::VerificationConstraintVec, verify_constraints, ClientBuilder,
        CosignCapabilities,
    },
    errors::SigstoreError::RegistryPullManifestError,
    registry::{Auth, ClientConfig, ClientProtocol, OciReference},
};

pub struct CosignVerifier {
    pub client: sigstore::cosign::Client,
    pub allow_unsigned: bool,
}

#[cfg(test)]
fn get_tuf_path() -> Option<PathBuf> {
    None
}

#[cfg(not(test))]
fn get_tuf_path() -> Option<PathBuf> {
    Some(PathBuf::from(crate::directories::RTDIR_TUF))
}

impl CosignVerifier {
    pub(crate) fn new(allow_unsigned: bool) -> Result<Self, anyhow::Error> {
        info!("Starting Cosign Verifier, downloading data from Sigstore TUF repository");
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|e| anyhow!("Error building tokio runtime: {}", e))?;
        let oci_config = ClientConfig {
            protocol: ClientProtocol::Https,
            ..Default::default()
        };

        // The cosign is a static ref which needs to live for the rest of the program's
        // lifecycle so therefore the repo ALSO needs to be static, requiring us
        // to leak it here.
        let repo: &dyn sigstore::trust::TrustRoot =
            Box::leak(rt.block_on(fetch_sigstore_tuf_data())?);

        let cosign_client = ClientBuilder::default()
            .with_oci_client_config(oci_config)
            .with_trust_repository(repo)?
            .enable_registry_caching()
            .build()?;

        Ok(Self {
            client: cosign_client,
            allow_unsigned,
        })
    }

    pub(crate) fn verify(
        &mut self,
        image: &str,
        username: Option<&str>,
        password: Option<&str>,
    ) -> Result<(), anyhow::Error> {
        debug!("CosignVerifier::verify()");
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|e| anyhow!("Error building tokio runtime: {}", e))?;
        let image = OciReference::from_str(image)?;
        let auth = if let (Some(username), Some(password)) = (username, password) {
            Auth::Basic(username.to_string(), password.to_string())
        } else {
            Auth::Anonymous
        };

        debug!("Triangulating image: {}", image);
        let (cosign_signature_image, source_image_digest) =
            rt.block_on(self.client.triangulate(&image, &auth))?;

        debug!("Getting trusted layers");
        match rt.block_on(self.client.trusted_signature_layers(
            &auth,
            &source_image_digest,
            &cosign_signature_image,
        )) {
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
                    if !self.allow_unsigned {
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
    }
}

async fn fetch_sigstore_tuf_data() -> anyhow::Result<Box<dyn sigstore::trust::TrustRoot>> {
    let tuf = sigstore::trust::sigstore::SigstoreTrustRoot::new(get_tuf_path().as_deref())
        .await
        .map_err(|e| {
            anyhow!(
                "Error spawning blocking task to build sigstore repo inside of tokio: {}",
                e
            )
        })?;

    Ok(Box::new(tuf))
}
