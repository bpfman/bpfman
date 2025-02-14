// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{path::PathBuf, str::FromStr, sync::Arc};

use anyhow::{anyhow, bail};
use log::{debug, info, warn};
use sigstore::{
    cosign::{
        verification_constraint::VerificationConstraintVec, verify_constraints, ClientBuilder,
        CosignCapabilities,
    },
    errors::SigstoreError::RegistryPullManifestError,
    registry::{Auth, ClientConfig, ClientProtocol, OciReference},
    trust::{sigstore::SigstoreTrustRoot, ManualTrustRoot, TrustRoot as _},
};

use crate::oci_utils::RUNTIME;

pub struct CosignVerifier {
    repo: Arc<ManualTrustRoot<'static>>,
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

        let repo = RUNTIME.block(fetch_sigstore_tuf_data())?;

        Ok(Self {
            repo,
            allow_unsigned,
        })
    }

    fn client(&self) -> anyhow::Result<sigstore::cosign::Client> {
        let oci_config = ClientConfig {
            protocol: ClientProtocol::Https,
            ..Default::default()
        };
        let cosign_client = ClientBuilder::default()
            .with_oci_client_config(oci_config)
            .with_trust_repository(self.repo.as_ref())?
            .enable_registry_caching()
            .build()?;

        Ok(cosign_client)
    }

    pub(crate) fn verify(
        &mut self,
        image: &str,
        username: Option<&str>,
        password: Option<&str>,
    ) -> Result<(), anyhow::Error> {
        debug!("CosignVerifier::verify()");

        let image = OciReference::from_str(image)?;
        let auth = if let (Some(username), Some(password)) = (username, password) {
            Auth::Basic(username.to_string(), password.to_string())
        } else {
            Auth::Anonymous
        };

        debug!("Triangulating image: {}", image);

        RUNTIME.block(get_image_and_digest(
            &mut self.client()?,
            &image,
            &auth,
            self.allow_unsigned,
        ))?;

        Ok(())
    }
}

async fn get_image_and_digest(
    client: &mut sigstore::cosign::Client,
    image: &OciReference,
    auth: &Auth,
    allow_unsigned: bool,
) -> Result<(OciReference, String), anyhow::Error> {
    let (cosign_signature_image, source_image_digest) = client.triangulate(image, auth).await?;

    debug!("Getting trusted layers");

    match client
        .trusted_signature_layers(auth, &source_image_digest, &cosign_signature_image)
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
            Ok((cosign_signature_image, source_image_digest))
        }
        Err(e) => match e {
            RegistryPullManifestError { .. } => {
                if !allow_unsigned {
                    bail!("Error triangulating image: {}", e);
                } else {
                    warn!("The bytecode image: {} is unsigned", image);
                    Ok((cosign_signature_image, source_image_digest))
                }
            }
            _ => {
                bail!("Error triangulating image: {}", e);
            }
        },
    }
}

async fn fetch_sigstore_tuf_data() -> anyhow::Result<Arc<ManualTrustRoot<'static>>> {
    let repo = SigstoreTrustRoot::new(get_tuf_path().as_deref()).await?;

    let fulcio_certs = repo
        .fulcio_certs()
        .expect("Cannot fetch Fulcio certificates from TUF repository")
        .into_iter()
        .map(|c| c.into_owned())
        .collect();

    let manual_root = ManualTrustRoot {
        fulcio_certs,
        rekor_keys: repo
            .rekor_keys()
            .expect("Cannot fetch Rekor keys from TUF repository")
            .iter()
            .map(|k| k.to_vec())
            .collect(),
        ..Default::default()
    };

    Ok(Arc::new(manual_root))
}
