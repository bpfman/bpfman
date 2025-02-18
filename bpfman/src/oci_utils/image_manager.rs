// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    io::{copy, Read},
};

use anyhow::anyhow;
use flate2::read::GzDecoder;
use log::{debug, error, info, trace};
use object::{Endianness, Object};
use oci_client::{
    client::{ClientConfig, ClientProtocol},
    manifest,
    manifest::OciImageManifest,
    secrets::RegistryAuth,
    Client, Reference,
};
use serde::Deserialize;
use serde_json::Value;
use sha2::{Digest, Sha256};
use sled::Db;
use tar::Archive;

use crate::{
    oci_utils::{cosign::CosignVerifier, ImageError},
    types::ImagePullPolicy,
    utils::{sled_get, sled_insert},
};

const OCI_PROGRAMS_LABEL: &str = "io.ebpf.programs";
const OCI_MAPS_LABEL: &str = "io.ebpf.maps";

#[derive(Debug)]
pub struct ContainerImageMetadata {
    pub programs: HashMap<String, String>,
    pub _maps: HashMap<String, String>,
}

impl From<ContainerImageMetadataV1> for ContainerImageMetadata {
    fn from(value: ContainerImageMetadataV1) -> Self {
        let mut programs = HashMap::new();
        programs.insert(value.bpf_function_name, value.program_type);
        ContainerImageMetadata {
            programs,
            _maps: HashMap::new(),
        }
    }
}

#[derive(Deserialize)]
pub struct ContainerImageMetadataV1 {
    #[serde(rename(deserialize = "io.ebpf.program_name"))]
    pub _name: String,
    #[serde(rename(deserialize = "io.ebpf.bpf_function_name"))]
    pub bpf_function_name: String,
    #[serde(rename(deserialize = "io.ebpf.program_type"))]
    pub program_type: String,
    #[serde(rename(deserialize = "io.ebpf.filename"))]
    pub _filename: String,
}

pub struct ImageManager {
    client: Client,
    cosign_verifier: Option<CosignVerifier>,
}

impl ImageManager {
    pub async fn new(verify_enabled: bool, allow_unsigned: bool) -> Result<Self, anyhow::Error> {
        let cosign_verifier = if verify_enabled {
            Some(CosignVerifier::new(allow_unsigned).await?)
        } else {
            None
        };
        let config = ClientConfig {
            protocol: ClientProtocol::Https,
            ..Default::default()
        };
        let client = Client::new(config);
        Ok(Self {
            client,
            cosign_verifier,
        })
    }

    pub(crate) async fn get_image(
        &mut self,
        root_db: &Db,
        image_url: &str,
        pull_policy: ImagePullPolicy,
        username: Option<String>,
        password: Option<String>,
    ) -> Result<(String, Vec<String>), ImageError> {
        // The reference created here is created using the krustlet oci-client
        // crate. It currently contains many defaults more of which can be seen
        // here: https://github.com/oras-project/oci-client/blob/main/src/reference.rs#L58
        let image: Reference = image_url.parse().map_err(ImageError::InvalidImageUrl)?;

        if let Some(cosign_verifier) = &mut self.cosign_verifier {
            cosign_verifier
                .verify(image_url, username.as_deref(), password.as_deref())
                .await?;
        } else {
            info!("Cosign verification is disabled, so skipping verification");
        }

        let image_content_key = get_image_content_key(&image);

        let exists: bool = root_db
            .contains_key(image_content_key.to_string() + "manifest.json")
            .map_err(|e| {
                ImageError::DatabaseError("failed to read db".to_string(), e.to_string())
            })?;

        let image_meta = match pull_policy {
            ImagePullPolicy::Always => {
                self.pull_image(root_db, image, &image_content_key, username, password)
                    .await?
            }
            ImagePullPolicy::IfNotPresent => {
                if exists {
                    self.load_image_meta(root_db, &image_content_key)?
                } else {
                    self.pull_image(root_db, image, &image_content_key, username, password)
                        .await?
                }
            }
            ImagePullPolicy::Never => {
                if exists {
                    self.load_image_meta(root_db, &image_content_key)?
                } else {
                    Err(ImageError::ByteCodeImageNotfound(image.to_string()))?
                }
            }
        };

        Ok((
            image_content_key.to_string(),
            image_meta.programs.into_keys().collect(),
        ))
    }

    fn get_auth_for_registry(
        &self,
        _registry: &str,
        username: Option<String>,
        password: Option<String>,
    ) -> RegistryAuth {
        match (username, password) {
            (Some(username), Some(password)) => RegistryAuth::Basic(username, password),
            _ => RegistryAuth::Anonymous,
        }
    }

    pub async fn pull_image(
        &mut self,
        root_db: &Db,
        image: Reference,
        base_key: &str,
        username: Option<String>,
        password: Option<String>,
    ) -> Result<ContainerImageMetadata, ImageError> {
        debug!(
            "Pulling bytecode from image path: {}/{}:{}",
            image.registry(),
            image.repository(),
            image.tag().unwrap_or("latest")
        );

        let auth = self.get_auth_for_registry(image.registry(), username, password);

        let (image_manifest, _, config_contents) = self
            .client
            .pull_manifest_and_config(&image.clone(), &auth)
            .await
            .map_err(ImageError::ImageManifestPullFailure)?;

        trace!("Raw container image manifest {}", image_manifest);

        let config_sha = &image_manifest
            .config
            .digest
            .split(':')
            .collect::<Vec<&str>>()[1];

        let image_config: Value = serde_json::from_str(&config_contents)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        trace!("Raw container image config {}", image_config);

        let labels_map = image_config["config"]["Labels"].as_object().ok_or(
            ImageError::ByteCodeImageProcessFailure(anyhow!("Labels not found")),
        )?;

        // The values of the Labels `io.ebpf.maps` and `io.ebpf.programs` are in JSON format try and
        // parse those, if that fails fallback to the V1 version of the metadata spec,
        // if that fails error out.
        let image_labels = match (
            labels_map.get(OCI_MAPS_LABEL),
            labels_map.get(OCI_PROGRAMS_LABEL),
        ) {
            (Some(maps), Some(programs)) => {
                let tmp_map = serde_label(maps, "map".to_string())?;
                let tmp_program = serde_label(programs, "program".to_string())?;

                ContainerImageMetadata {
                    _maps: tmp_map,
                    programs: tmp_program,
                }
            }
            _ => {
                // Try to deserialize from older version of metadata
                match serde_json::from_str::<ContainerImageMetadataV1>(
                    &image_config["config"]["Labels"].to_string(),
                ) {
                    Ok(labels) => labels.into(),
                    Err(e) => return Err(ImageError::ByteCodeImageProcessFailure(e.into())),
                }
            }
        };

        let image_content = self
            .client
            .pull(
                &image,
                &auth,
                vec![
                    manifest::IMAGE_LAYER_GZIP_MEDIA_TYPE,
                    manifest::IMAGE_DOCKER_LAYER_GZIP_MEDIA_TYPE,
                ],
            )
            .await
            .map_err(ImageError::BytecodeImagePullFailure)?
            .layers
            .into_iter()
            .next()
            .map(|layer| layer.data)
            .ok_or(ImageError::BytecodeImageExtractFailure(
                "No data in bytecode image layer".to_string(),
            ))?;

        // Make sure endian target matches that of the system before storing
        let unzipped_content = get_bytecode_from_gzip(image_content.clone());
        let obj_endianness = object::read::File::parse(unzipped_content.as_slice())
            .map_err(|e| ImageError::BytecodeImageExtractFailure(e.to_string()))?
            .endianness();
        let host_endianness = Endianness::default();

        if host_endianness != obj_endianness {
            return Err(ImageError::BytecodeImageExtractFailure(
                format!("image bytecode endianness: {obj_endianness:?} does not match host {host_endianness:?}"),
            ));
        };

        // Update Database to save for later
        let image_manifest_key = base_key.to_string() + "manifest.json";

        let image_manifest_json = serde_json::to_string(&image_manifest)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        sled_insert(root_db, &image_manifest_key, image_manifest_json.as_bytes()).map_err(|e| {
            ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
        })?;

        let image_config_path = base_key.to_string() + config_sha;

        root_db
            .insert(image_config_path, config_contents.as_str())
            .map_err(|e| {
                ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
            })?;

        let bytecode_sha = image_manifest.layers[0]
            .digest
            .split(':')
            .collect::<Vec<&str>>()[1];

        let bytecode_path = base_key.to_string() + bytecode_sha;

        sled_insert(root_db, &bytecode_path, &image_content).map_err(|e| {
            ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
        })?;

        root_db.flush().map_err(|e| {
            ImageError::DatabaseError("failed to flush db".to_string(), e.to_string())
        })?;

        Ok(image_labels)
    }

    pub(crate) fn get_bytecode_from_image_store(
        &self,
        root_db: &Db,
        base_key: String,
    ) -> Result<Vec<u8>, ImageError> {
        let manifest = serde_json::from_str::<OciImageManifest>(
            std::str::from_utf8(
                &sled_get(root_db, &(base_key.clone() + "manifest.json")).map_err(|e| {
                    ImageError::DatabaseError("failed to read db".to_string(), e.to_string())
                })?,
            )
            .unwrap(),
        )
        .map_err(|e| {
            ImageError::DatabaseError(
                "failed to parse image manifest from db".to_string(),
                e.to_string(),
            )
        })?;

        let bytecode_sha = &manifest.layers[0].digest;
        let bytecode_key = base_key + bytecode_sha.clone().split(':').collect::<Vec<&str>>()[1];

        debug!(
            "bytecode is stored as tar+gzip file at key {}",
            bytecode_key
        );

        let f = sled_get(root_db, &bytecode_key).map_err(|e| {
            ImageError::DatabaseError("failed to read db".to_string(), e.to_string())
        })?;

        let mut hasher = Sha256::new();
        copy(&mut f.as_slice(), &mut hasher).expect("cannot copy bytecode to hasher");
        let hash = hasher.finalize();
        let expected_sha = "sha256:".to_owned() + &base16ct::lower::encode_string(&hash);

        if *bytecode_sha != expected_sha {
            debug!(
                "actual SHA256: {}\nexpected SHA256:{:?}",
                bytecode_sha, expected_sha
            );
            panic!("Bpf Bytecode has been compromised")
        }

        Ok(get_bytecode_from_gzip(f))
    }

    fn load_image_meta(
        &self,
        root_db: &Db,
        image_content_key: &str,
    ) -> Result<ContainerImageMetadata, ImageError> {
        let manifest = serde_json::from_str::<OciImageManifest>(
            std::str::from_utf8(
                &sled_get(root_db, &(image_content_key.to_string() + "manifest.json")).map_err(
                    |e| ImageError::DatabaseError("failed to read db".to_string(), e.to_string()),
                )?,
            )
            .unwrap(),
        )
        .map_err(|e| {
            ImageError::DatabaseError(
                "failed to parse db entry to image manifest".to_string(),
                e.to_string(),
            )
        })?;

        let config_sha = &manifest.config.digest.split(':').collect::<Vec<&str>>()[1];

        let image_config_key = image_content_key.to_string() + config_sha;

        let db_content = sled_get(root_db, &image_config_key).map_err(|e| {
            ImageError::DatabaseError("failed to read db".to_string(), e.to_string())
        })?;

        let file_content = std::str::from_utf8(&db_content)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        let image_config: Value =
            serde_json::from_str(file_content).expect("cannot parse image config from database");
        debug!(
            "Raw container image config {}",
            &image_config["config"]["Labels"].to_string()
        );

        let labels_map = image_config["config"]["Labels"].as_object().ok_or(
            ImageError::ByteCodeImageProcessFailure(anyhow!("Labels not found")),
        )?;

        Ok(
            match (
                labels_map.get(OCI_MAPS_LABEL),
                labels_map.get(OCI_PROGRAMS_LABEL),
            ) {
                (Some(maps), Some(programs)) => ContainerImageMetadata {
                    _maps: serde_json::from_str::<HashMap<String, String>>(maps.as_str().unwrap())
                        .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?,
                    programs: serde_json::from_str::<HashMap<String, String>>(
                        programs.as_str().unwrap(),
                    )
                    .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?,
                },
                _ => {
                    // Try to deserialize from older version of metadata
                    match serde_json::from_str::<ContainerImageMetadataV1>(
                        &image_config["config"]["Labels"].to_string(),
                    ) {
                        Ok(labels) => labels.into(),
                        Err(e) => return Err(ImageError::ByteCodeImageProcessFailure(e.into())),
                    }
                }
            },
        )
    }
}

fn serde_label(labels: &Value, label_type: String) -> Result<HashMap<String, String>, ImageError> {
    debug!("found {} labels - {:?}", label_type, labels);
    let val = match serde_json::from_str::<HashMap<String, String>>(
        labels.as_str().unwrap_or_default(),
    ) {
        Ok(l) => l,
        Err(e) => {
            let err_str = format!("error pulling image, invalid image {label_type} label");
            error!("{err_str}");
            return Err(ImageError::BytecodeImageParseFailure(
                err_str,
                e.to_string(),
            ));
        }
    };

    Ok(val)
}

fn get_image_content_key(image: &Reference) -> String {
    // Try to get the tag, if it doesn't exist, get the digest
    // if neither exist, return "latest" as the tag
    let tag = match image.tag() {
        Some(t) => t,
        _ => match image.digest() {
            Some(d) => d,
            _ => "latest",
        },
    };

    format!(
        "{}_{}_{}",
        image.registry(),
        image.repository().replace('/', "_"),
        tag
    )
}

fn get_bytecode_from_gzip(bytes: Vec<u8>) -> Vec<u8> {
    let decoder = GzDecoder::new(bytes.as_slice());
    Archive::new(decoder)
        .entries()
        .expect("unable to parse tarball entries")
        .filter_map(|e| e.ok())
        .map(|mut entry| {
            let mut data = Vec::new();
            entry
                .read_to_end(&mut data)
                .expect("unable to read bytecode tarball entry");
            data
        })
        .collect::<Vec<Vec<u8>>>()
        .first()
        .expect("unable to get bytecode file bytes")
        .to_owned()
}

#[cfg(test)]
mod tests {
    use assert_matches::assert_matches;

    use super::*;
    use crate::{config::SigningConfig, get_db_config, init_database};

    #[tokio::test]
    async fn image_pull_and_bytecode_verify_legacy() {
        let root_db = init_database(get_db_config())
            .await
            .expect("Unable to open root database for unit test");
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .await
        .unwrap();
        let (image_content_key, _) = mgr
            .get_image(
                &root_db,
                "quay.io/bpfman-bytecode/go-xdp-counter-legacy-labels:latest",
                ImagePullPolicy::Always,
                None,
                None,
            )
            .await
            .expect("failed to pull bytecode");

        let program_bytes = mgr
            .get_bytecode_from_image_store(&root_db, image_content_key)
            .expect("failed to get bytecode from image store");

        assert!(!program_bytes.is_empty())
    }

    #[tokio::test]
    async fn image_pull_and_bytecode_verify() {
        let root_db = init_database(get_db_config())
            .await
            .expect("Unable to open root database for unit test");
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .await
        .unwrap();
        let (image_content_key, _) = mgr
            .get_image(
                &root_db,
                "quay.io/bpfman-bytecode/go-xdp-counter:latest",
                ImagePullPolicy::Always,
                None,
                None,
            )
            .await
            .expect("failed to pull bytecode");

        let program_bytes = mgr
            .get_bytecode_from_image_store(&root_db, image_content_key)
            .expect("failed to get bytecode from image store");

        assert!(!program_bytes.is_empty())
    }

    #[tokio::test]
    async fn image_pull_policy_never_failure() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .await
        .unwrap();
        let root_db = init_database(get_db_config())
            .await
            .expect("Unable to open root database for unit test");

        let result = mgr
            .get_image(
                &root_db,
                "quay.io/bpfman-bytecode/xdp_pass:latest",
                ImagePullPolicy::Never,
                None,
                None,
            )
            .await;

        assert_matches!(result, Err(ImageError::ByteCodeImageNotfound(_)));
    }

    #[tokio::test]
    #[should_panic]
    async fn private_image_pull_failure() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .await
        .unwrap();
        let root_db = init_database(get_db_config())
            .await
            .expect("Unable to open root database for unit test");

        mgr.get_image(
            &root_db,
            "quay.io/bpfman-bytecode/xdp_pass_private:latest",
            ImagePullPolicy::Always,
            None,
            None,
        )
        .await
        .expect("failed to pull bytecode");
    }

    #[tokio::test]
    async fn private_image_pull_and_bytecode_verify() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .await
        .unwrap();
        let root_db = init_database(get_db_config())
            .await
            .expect("Unable to open root database for unit test");

        let (image_content_key, _) = mgr
            .get_image(
                &root_db,
                "quay.io/bpfman-bytecode/xdp_pass_private:latest",
                ImagePullPolicy::Always,
                Some("bpfman-bytecode+bpfmancreds".to_owned()),
                Some("D49CKWI1MMOFGRCAT8SHW5A56FSVP30TGYX54BBWKY2J129XRI6Q5TVH2ZZGTJ1M".to_owned()),
            )
            .await
            .expect("failed to pull bytecode");

        let program_bytes = mgr
            .get_bytecode_from_image_store(&root_db, image_content_key)
            .expect("failed to get bytecode from image store");

        assert!(!program_bytes.is_empty())
    }

    #[tokio::test]
    async fn image_pull_failure() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .await
        .unwrap();
        let root_db = init_database(get_db_config())
            .await
            .expect("Unable to open root database for unit test");

        let result = mgr
            .get_image(
                &root_db,
                "quay.io/bpfman-bytecode/xdp_pass:latest",
                ImagePullPolicy::Never,
                None,
                None,
            )
            .await;

        assert_matches!(result, Err(ImageError::ByteCodeImageNotfound(_)));
    }

    #[test]
    fn test_good_image_content_key() {
        struct Case {
            input: &'static str,
            output: &'static str,
        }
        let tt = vec![
            Case{input: "busybox", output: "docker.io_library_busybox_latest"},
            Case{input:"quay.io/busybox", output: "quay.io_busybox_latest"},
            Case{input:"docker.io/test:tag", output: "docker.io_library_test_tag"},
            Case{input:"quay.io/test:5000", output: "quay.io_test_5000"},
            Case{input:"test.com/repo:tag", output: "test.com_repo_tag"},
            Case{
                input:"test.com/repo@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
                output: "test.com_repo_sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
            }
        ];

        for t in tt {
            let good_reference: Reference = t.input.parse().unwrap();
            let image_content_key = get_image_content_key(&good_reference);
            assert_eq!(image_content_key, t.output);
        }
    }
}
