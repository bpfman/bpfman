// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    io::{Read, copy},
    process::Command,
    path::PathBuf,
};

use anyhow::anyhow;
use flate2::read::GzDecoder;
use log::{debug, error, info, trace};
use object::{Endianness, Object};
use oci_client::{
    Client, Reference,
    client::{ClientConfig, ClientProtocol},
    manifest,
    manifest::OciImageManifest,
    secrets::RegistryAuth,
};
use serde::Deserialize;
use serde_json::Value;
use sha2::{Digest, Sha256};
use sled::Db;
use tar::Archive;

use crate::{
    oci_utils::{
        ImageError,
        cosign::{CosignVerifier, fetch_sigstore_tuf_data},
        rt,
    },
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

#[derive(Debug, Clone, Copy)]
pub enum ContainerRuntime {
    Docker,
    Podman,
}

impl ImageManager {
    pub fn new(verify_enabled: bool, allow_unsigned: bool) -> Result<Self, anyhow::Error> {
        let cosign_verifier = if verify_enabled {
            let repo = rt()?.block_on(fetch_sigstore_tuf_data())?;
            Some(CosignVerifier::new(repo, allow_unsigned)?)
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

    pub fn image_exists_in_container_runtime(&self, image_url: &str) -> bool {
        let runtime = self.detect_container_runtime();

        match runtime {
            Some(ContainerRuntime::Docker) => {
                let output = Command::new("docker")
                    .args(["image", "inspect", image_url])
                    .output()
                    .ok();

                if let Some(output) = output {
                    output.status.success()
                } else {
                    false
                }
            },
            Some(ContainerRuntime::Podman) => {
                let output = Command::new("podman")
                    .args(["image", "inspect", image_url])
                    .output()
                    .ok();

                if let Some(output) = output {
                    output.status.success()
                } else {
                    false
                }
            },
            None => false,
        }
    }

    pub fn extract_bytecode_from_container_runtime(&self, image_url: &str) -> Result<(Vec<u8>, ContainerImageMetadata), ImageError> {
        let runtime = self.detect_container_runtime();

        match runtime {
            Some(runtime) => {
                let temp_dir = tempfile::tempdir()
                    .map_err(|e| ImageError::BytecodeImageExtractFailure(format!("Failed to create temp dir: {}", e)))?;

                let container_id = self.create_container_from_image(runtime, image_url, &temp_dir)?;
                let bytecode_path = self.locate_bytecode_in_container(runtime, &container_id, &temp_dir)?;
                let metadata = self.extract_metadata_from_container(runtime, &container_id)?;

                self.remove_container(runtime, &container_id)?;

                let bytecode = std::fs::read(bytecode_path)
                    .map_err(|e| ImageError::BytecodeImageExtractFailure(format!("Failed to read bytecode: {}", e)))?;

                Ok((bytecode, metadata))
            },
            None => Err(ImageError::BytecodeImageExtractFailure(
                "No container runtime detected".to_string(),
            )),
        }
    }

    fn detect_container_runtime(&self) -> Option<ContainerRuntime> {
        // Check if container runtime integration is enabled
        let config = crate::utils::open_config_file();
        if !config.container_runtime.enabled {
            return None;
        }
        
        // Check if a preferred runtime is specified
        if let Some(preferred) = &config.container_runtime.preferred_runtime {
            match preferred.to_lowercase().as_str() {
                "docker" => {
                    if Command::new("docker").arg("--version").output().is_ok() {
                        return Some(ContainerRuntime::Docker);
                    }
                },
                "podman" => {
                    if Command::new("podman").arg("--version").output().is_ok() {
                        return Some(ContainerRuntime::Podman);
                    }
                },
                _ => {
                    // Invalid preferred runtime specified, log a warning
                    log::warn!("Invalid preferred container runtime specified: {}", preferred);
                }
            }
        }

        // Auto-detect available runtimes
        if Command::new("docker").arg("--version").output().is_ok() {
            return Some(ContainerRuntime::Docker);
        }
        
        if Command::new("podman").arg("--version").output().is_ok() {
            return Some(ContainerRuntime::Podman);
        }
        
        None
    }

    pub(crate) fn get_image(
        &mut self,
        root_db: &Db,
        image_url: &str,
        pull_policy: ImagePullPolicy,
        username: Option<String>,
        password: Option<String>,
    ) -> Result<(String, Vec<String>), ImageError> {
        let image: Reference = image_url.parse().map_err(ImageError::InvalidImageUrl)?;

        if let Some(cosign_verifier) = &mut self.cosign_verifier {
            cosign_verifier.verify(image_url, username.as_deref(), password.as_deref())?;
        } else {
            info!("Cosign verification is disabled, so skipping verification");
        }

        let image_content_key = get_image_content_key(&image);

        let exists_in_db: bool = root_db
            .contains_key(image_content_key.to_string() + "manifest.json")
            .map_err(|e| {
                ImageError::DatabaseError("failed to read db".to_string(), e.to_string())
            })?;

        let exists_in_runtime = self.image_exists_in_container_runtime(image_url);

        let image_meta = match pull_policy {
            ImagePullPolicy::Always => {
                if self.detect_container_runtime().is_some() {
                    let _ = self.pull_with_container_runtime(image_url, username.as_deref(), password.as_deref());

                    if self.image_exists_in_container_runtime(image_url) {
                        let (bytecode, metadata) = self.extract_bytecode_from_container_runtime(image_url)?;

                        self.store_extracted_bytecode(root_db, &image_content_key, bytecode, &metadata)?;

                        metadata
                    } else {
                        self.pull_image(root_db, image, &image_content_key, username, password)?
                    }
                } else {
                    self.pull_image(root_db, image, &image_content_key, username, password)?
                }
            },
            ImagePullPolicy::IfNotPresent => {
                if exists_in_db {
                    self.load_image_meta(root_db, &image_content_key)?
                } else if exists_in_runtime {
                    let (bytecode, metadata) = self.extract_bytecode_from_container_runtime(image_url)?;

                    self.store_extracted_bytecode(root_db, &image_content_key, bytecode, &metadata)?;

                    metadata
                } else {
                    if self.detect_container_runtime().is_some() {
                        let _ = self.pull_with_container_runtime(image_url, username.as_deref(), password.as_deref());

                        if self.image_exists_in_container_runtime(image_url) {
                            let (bytecode, metadata) = self.extract_bytecode_from_container_runtime(image_url)?;
                            self.store_extracted_bytecode(root_db, &image_content_key, bytecode, &metadata)?;
                            metadata
                        } else {
                            self.pull_image(root_db, image, &image_content_key, username, password)?
                        }
                    } else {
                        self.pull_image(root_db, image, &image_content_key, username, password)?
                    }
                }
            },
            ImagePullPolicy::Never => {
                if exists_in_db {
                    self.load_image_meta(root_db, &image_content_key)?
                } else if exists_in_runtime {
                    let (bytecode, metadata) = self.extract_bytecode_from_container_runtime(image_url)?;

                    self.store_extracted_bytecode(root_db, &image_content_key, bytecode, &metadata)?;

                    metadata
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

    fn create_container_from_image(&self, runtime: ContainerRuntime, image_url: &str, temp_dir: &tempfile::TempDir) -> Result<String, ImageError> {
        let container_name = format!("bpfman-extract-{}", uuid::Uuid::new_v4());
        
        let output = match runtime {
            ContainerRuntime::Docker => {
                Command::new("docker")
                    .args(["create", "--name", &container_name, image_url])
                    .output()
            },
            ContainerRuntime::Podman => {
                Command::new("podman")
                    .args(["create", "--name", &container_name, image_url])
                    .output()
            }
        }.map_err(|e| ImageError::BytecodeImageExtractFailure(
            format!("Failed to create container: {}", e)
        ))?;
        
        if !output.status.success() {
            return Err(ImageError::BytecodeImageExtractFailure(
                format!("Failed to create container: {}", 
                    String::from_utf8_lossy(&output.stderr))
            ));
        }
        
        let container_id = String::from_utf8_lossy(&output.stdout)
            .trim()
            .to_string();
        
        Ok(container_id)
    }

    fn locate_bytecode_in_container(&self, runtime: ContainerRuntime, container_id: &str, temp_dir: &tempfile::TempDir) -> Result<PathBuf, ImageError> {
        // Create a destination path for the extracted content
        let dest_path = temp_dir.path().join("extracted");
        std::fs::create_dir_all(&dest_path).map_err(|e| 
            ImageError::BytecodeImageExtractFailure(format!("Failed to create directory: {}", e))
        )?;
        
        // Copy all files from the container to the temp directory
        let output = match runtime {
            ContainerRuntime::Docker => {
                Command::new("docker")
                    .args(["cp", &format!("{}:/", container_id), dest_path.to_str().unwrap()])
                    .output()
            },
            ContainerRuntime::Podman => {
                Command::new("podman")
                    .args(["cp", &format!("{}:/", container_id), dest_path.to_str().unwrap()])
                    .output()
            }
        }.map_err(|e| ImageError::BytecodeImageExtractFailure(
            format!("Failed to copy files from container: {}", e)
        ))?;
        
        if !output.status.success() {
            return Err(ImageError::BytecodeImageExtractFailure(
                format!("Failed to copy files from container: {}", 
                    String::from_utf8_lossy(&output.stderr))
            ));
        }
        
        // Look for ELF files in the extracted directory
        let bytecode_files = self.find_elf_files(&dest_path)?;
        
        if bytecode_files.is_empty() {
            return Err(ImageError::BytecodeImageExtractFailure(
                "No eBPF bytecode files found in container".to_string()
            ));
        }
        
        // For now, just return the first eBPF file found
        // A more sophisticated approach could look for specific names or labels
        Ok(bytecode_files[0].clone())
    }

    fn find_elf_files(&self, dir: &PathBuf) -> Result<Vec<PathBuf>, ImageError> {
        let mut results = Vec::new();
        
        if dir.is_dir() {
            for entry in std::fs::read_dir(dir).map_err(|e| 
                ImageError::BytecodeImageExtractFailure(format!("Failed to read directory: {}", e))
            )? {
                let entry = entry.map_err(|e| 
                    ImageError::BytecodeImageExtractFailure(format!("Failed to read directory entry: {}", e))
                )?;
                let path = entry.path();
                
                if path.is_dir() {
                    let mut subdirectory_results = self.find_elf_files(&path)?;
                    results.append(&mut subdirectory_results);
                } else if self.is_elf_file(&path) {
                    results.push(path);
                }
            }
        }
        
        Ok(results)
    }

    fn is_elf_file(&self, path: &PathBuf) -> bool {
        if let Ok(file) = std::fs::File::open(path) {
            let mut buffer = [0u8; 4];
            if let Ok(size) = file.take(4).read(&mut buffer) {
                if size >= 4 && buffer[0] == 0x7F && buffer[1] == b'E' && buffer[2] == b'L' && buffer[3] == b'F' {
                    return true;
                }
            }
        }
        false
    }

    fn extract_metadata_from_container(&self, runtime: ContainerRuntime, container_id: &str) -> Result<ContainerImageMetadata, ImageError> {
        let output = match runtime {
            ContainerRuntime::Docker => {
                Command::new("docker")
                    .args(["inspect", container_id])
                    .output()
            },
            ContainerRuntime::Podman => {
                Command::new("podman")
                    .args(["inspect", container_id])
                    .output()
            }
        }.map_err(|e| ImageError::BytecodeImageExtractFailure(
            format!("Failed to inspect container: {}", e)
        ))?;
        
        if !output.status.success() {
            return Err(ImageError::BytecodeImageExtractFailure(
                format!("Failed to inspect container: {}", 
                    String::from_utf8_lossy(&output.stderr))
            ));
        }
        
        let inspect_output: Value = serde_json::from_slice(&output.stdout)
            .map_err(|e| ImageError::BytecodeImageExtractFailure(
                format!("Failed to parse container inspect output: {}", e)
            ))?;
        
        // Extract labels from container inspect output
        let labels = &inspect_output[0]["Config"]["Labels"];
        
        // Parse labels to extract metadata
        let labels_map = labels.as_object().ok_or(
            ImageError::ByteCodeImageProcessFailure(anyhow!("Labels not found in container"))
        )?;
        
        Ok(match (
            labels_map.get(OCI_MAPS_LABEL),
            labels_map.get(OCI_PROGRAMS_LABEL),
        ) {
            (Some(maps), Some(programs)) => ContainerImageMetadata {
                _maps: serde_json::from_str::<HashMap<String, String>>(maps.as_str().unwrap())
                    .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?,
                programs: serde_json::from_str::<HashMap<String, String>>(programs.as_str().unwrap())
                    .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?,
            },
            _ => {
                // Try legacy format
                match serde_json::from_str::<ContainerImageMetadataV1>(&labels.to_string()) {
                    Ok(legacy_labels) => legacy_labels.into(),
                    Err(e) => return Err(ImageError::ByteCodeImageProcessFailure(e.into())),
                }
            }
        })
    }

    fn remove_container(&self, runtime: ContainerRuntime, container_id: &str) -> Result<(), ImageError> {
        let output = match runtime {
            ContainerRuntime::Docker => {
                Command::new("docker")
                    .args(["rm", container_id])
                    .output()
            },
            ContainerRuntime::Podman => {
                Command::new("podman")
                    .args(["rm", container_id])
                    .output()
            }
        }.map_err(|e| ImageError::BytecodeImageExtractFailure(
            format!("Failed to remove container: {}", e)
        ))?;
        
        if !output.status.success() {
            return Err(ImageError::BytecodeImageExtractFailure(
                format!("Failed to remove container: {}", 
                    String::from_utf8_lossy(&output.stderr))
            ));
        }
        
        Ok(())
    }

    fn pull_with_container_runtime(&self, image_url: &str, username: Option<&str>, password: Option<&str>) -> Result<(), ImageError> {
        // Determine which container runtime to use
        let runtime = self.detect_container_runtime().ok_or(
            ImageError::BytecodeImageExtractFailure("No container runtime detected".to_string())
        )?;
        
        // If authentication is provided, set it up
        if let (Some(username), Some(password)) = (username, password) {
            // Create a temporary file for docker login credentials
            let temp_auth_file = tempfile::NamedTempFile::new().map_err(|e| 
                ImageError::BytecodeImageExtractFailure(format!("Failed to create temp auth file: {}", e))
            )?;
            
            // Extract registry from image URL
            let registry = if image_url.contains('/') {
                image_url.split('/').next().unwrap_or("docker.io")
            } else {
                "docker.io"
            };
            
            // Perform login
            let login_output = match runtime {
                ContainerRuntime::Docker => {
                    Command::new("docker")
                        .args([
                            "login", 
                            registry, 
                            "--username", username, 
                            "--password-stdin"
                        ])
                        .stdin(std::process::Stdio::piped())
                        .output()
                },
                ContainerRuntime::Podman => {
                    Command::new("podman")
                        .args([
                            "login", 
                            registry, 
                            "--username", username, 
                            "--password-stdin"
                        ])
                        .stdin(std::process::Stdio::piped())
                        .output()
                }
            }.map_err(|e| ImageError::BytecodeImageExtractFailure(
                format!("Failed to login to registry: {}", e)
            ))?;
            
            if !login_output.status.success() {
                return Err(ImageError::BytecodeImageExtractFailure(
                    format!("Failed to login to registry: {}", 
                        String::from_utf8_lossy(&login_output.stderr))
                ));
            }
        }
        
        // Pull the image
        let pull_output = match runtime {
            ContainerRuntime::Docker => {
                Command::new("docker")
                    .args(["pull", image_url])
                    .output()
            },
            ContainerRuntime::Podman => {
                Command::new("podman")
                    .args(["pull", image_url])
                    .output()
            }
        }.map_err(|e| ImageError::BytecodeImageExtractFailure(
            format!("Failed to pull image: {}", e)
        ))?;
        
        if !pull_output.status.success() {
            return Err(ImageError::BytecodeImageExtractFailure(
                format!("Failed to pull image: {}", 
                    String::from_utf8_lossy(&pull_output.stderr))
            ));
        }
        
        Ok(())
    }

    fn store_extracted_bytecode(
        &self, 
        root_db: &Db, 
        image_content_key: &str,
        bytecode: Vec<u8>, 
        metadata: &ContainerImageMetadata
    ) -> Result<(), ImageError> {
        // Create a synthetic manifest for the database
        let manifest = OciImageManifest {
            schema_version: 2,
            config: manifest::ManifestDescriptor {
                media_type: manifest::IMAGE_CONFIG_MEDIA_TYPE.to_string(),
                digest: format!("sha256:{}", Sha256::digest(&bytecode).to_hex_string()),
                size: bytecode.len() as i64,
            },
            layers: vec![manifest::ManifestDescriptor {
                media_type: manifest::IMAGE_LAYER_GZIP_MEDIA_TYPE.to_string(),
                digest: format!("sha256:{}", Sha256::digest(&bytecode).to_hex_string()),
                size: bytecode.len() as i64,
            }],
            annotations: None,
        };
        
        // Create a synthetic config for the database
        let config = serde_json::json!({
            "config": {
                "Labels": {
                    OCI_PROGRAMS_LABEL: serde_json::to_string(&metadata.programs).map_err(|e| 
                        ImageError::ByteCodeImageProcessFailure(e.into())
                    )?,
                    OCI_MAPS_LABEL: serde_json::to_string(&metadata._maps).map_err(|e| 
                        ImageError::ByteCodeImageProcessFailure(e.into())
                    )?
                }
            }
        });
        
        // Store manifest in database
        let image_manifest_key = image_content_key.to_string() + "manifest.json";
        let image_manifest_json = serde_json::to_string(&manifest)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        sled_insert(
            root_db,
            &image_manifest_key,
            image_manifest_json.as_bytes(),
        ).map_err(|e| 
            ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
        )?;
        
        // Store config in database
        let config_sha = Sha256::digest(&config.to_string().as_bytes()).to_hex_string();
        let image_config_path = image_content_key.to_string() + &config_sha;
        
        root_db
            .insert(image_config_path, config.to_string().as_bytes())
            .map_err(|e| 
                ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
            )?;
        
        // Store bytecode in database
        let bytecode_sha = Sha256::digest(&bytecode).to_hex_string();
        let bytecode_path = image_content_key.to_string() + &bytecode_sha;
        
        // Create gzipped tarball of bytecode
        let mut buffer = Vec::new();
        let mut archive = tar::Builder::new(&mut buffer);
        
        let mut header = tar::Header::new_gnu();
        header.set_size(bytecode.len() as u64);
        header.set_mode(0o644);
        archive.append_data(&mut header, "program.o", bytecode.as_slice())
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;
        
        archive.finish().map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;
        
        // Compress the tar file
        let mut gzipped_data = Vec::new();
        let mut encoder = flate2::write::GzEncoder::new(&mut gzipped_data, flate2::Compression::default());
        encoder.write_all(&buffer).map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;
        encoder.finish().map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;
        
        // Store the gzipped tar file
        sled_insert(root_db, &bytecode_path, &gzipped_data).map_err(|e| 
            ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
        )?;
        
        // Flush database
        root_db
            .flush()
            .map_err(|e| ImageError::DatabaseError("failed to flush db".to_string(), e.to_string()))?;
        
        Ok(())
    }

    pub fn pull_image(
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
        let client = self.client.clone();

        let image_labels = rt().unwrap().block_on(get_image_labels(
            client,
            root_db.clone(),
            base_key.to_string(),
            image.clone(),
            auth,
        ))?;
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

async fn get_image_labels(
    client: Client,
    root_db: Db,
    base_key: String,
    image: Reference,
    auth: RegistryAuth,
) -> Result<ContainerImageMetadata, ImageError> {
    let (image_manifest, _, config_contents) = client
        .pull_manifest_and_config(&image.clone(), &auth)
        .await
        .map_err(ImageError::BytecodeImagePullFailure)?;

    trace!("Raw container image manifest {}", image_manifest);

    let config_sha = &image_manifest
        .config
        .digest
        .split(':')
        .collect::<Vec<&str>>()[1];

    let image_config: Value = serde_json::from_str(&config_contents)
        .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

    trace!("Raw container image config {}", image_config);

    let image_content = client
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

    let labels_map = image_config["config"]["Labels"].as_object().ok_or(
        ImageError::ByteCodeImageProcessFailure(anyhow!("Labels not found")),
    )?;

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
            match serde_json::from_str::<ContainerImageMetadataV1>(
                &image_config["config"]["Labels"].to_string(),
            ) {
                Ok(labels) => labels.into(),
                Err(e) => return Err(ImageError::ByteCodeImageProcessFailure(e.into())),
            }
        }
    };

    let unzipped_content = get_bytecode_from_gzip(image_content.clone());
    let obj_endianness = object::read::File::parse(unzipped_content.as_slice())
        .map_err(|e| ImageError::BytecodeImageExtractFailure(e.to_string()))?
        .endianness();
    let host_endianness = Endianness::default();

    if host_endianness != obj_endianness {
        return Err(ImageError::BytecodeImageExtractFailure(format!(
            "image bytecode endianness: {obj_endianness:?} does not match host {host_endianness:?}"
        )));
    };

    let image_manifest_key = base_key.to_string() + "manifest.json";

    let image_manifest_json = serde_json::to_string(&image_manifest)
        .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

    sled_insert(
        &root_db,
        &image_manifest_key,
        image_manifest_json.as_bytes(),
    )
    .map_err(|e| ImageError::DatabaseError("failed to write to db".to_string(), e.to_string()))?;

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

    sled_insert(&root_db, &bytecode_path, &image_content).map_err(|e| {
        ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
    })?;

    root_db
        .flush()
        .map_err(|e| ImageError::DatabaseError("failed to flush db".to_string(), e.to_string()))?;

    Ok(image_labels)
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

    #[test]
    fn image_pull_and_bytecode_verify_legacy() {
        let root_db =
            &init_database(get_db_config()).expect("Unable to open root database for unit test");
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .unwrap();
        let (image_content_key, _) = mgr
            .get_image(
                root_db,
                "quay.io/bpfman-bytecode/go-xdp-counter-legacy-labels:latest",
                ImagePullPolicy::Always,
                None,
                None,
            )
            .expect("failed to pull bytecode");

        let program_bytes = mgr
            .get_bytecode_from_image_store(root_db, image_content_key)
            .expect("failed to get bytecode from image store");

        assert!(!program_bytes.is_empty())
    }

    #[test]
    fn image_pull_and_bytecode_verify() {
        let root_db =
            &init_database(get_db_config()).expect("Unable to open root database for unit test");
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .unwrap();
        let (image_content_key, _) = mgr
            .get_image(
                root_db,
                "quay.io/bpfman-bytecode/go-xdp-counter:latest",
                ImagePullPolicy::Always,
                None,
                None,
            )
            .expect("failed to pull bytecode");

        let program_bytes = mgr
            .get_bytecode_from_image_store(root_db, image_content_key)
            .expect("failed to get bytecode from image store");

        assert!(!program_bytes.is_empty())
    }

    #[test]
    fn image_pull_policy_never_failure() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .unwrap();
        let root_db =
            &init_database(get_db_config()).expect("Unable to open root database for unit test");

        let result = mgr.get_image(
            root_db,
            "quay.io/bpfman-bytecode/xdp_pass:latest",
            ImagePullPolicy::Never,
            None,
            None,
        );

        assert_matches!(result, Err(ImageError::ByteCodeImageNotfound(_)));
    }

    #[test]
    #[should_panic]
    fn private_image_pull_failure() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .unwrap();
        let root_db =
            &init_database(get_db_config()).expect("Unable to open root database for unit test");

        mgr.get_image(
            root_db,
            "quay.io/bpfman-bytecode/xdp_pass_private:latest",
            ImagePullPolicy::Always,
            None,
            None,
        )
        .expect("failed to pull bytecode");
    }

    #[test]
    fn private_image_pull_and_bytecode_verify() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .unwrap();
        let root_db =
            &init_database(get_db_config()).expect("Unable to open root database for unit test");

        let (image_content_key, _) = mgr
            .get_image(
                root_db,
                "quay.io/bpfman-bytecode/xdp_pass_private:latest",
                ImagePullPolicy::Always,
                Some("bpfman-bytecode+bpfmancreds".to_owned()),
                Some("D49CKWI1MMOFGRCAT8SHW5A56FSVP30TGYX54BBWKY2J129XRI6Q5TVH2ZZGTJ1M".to_owned()),
            )
            .expect("failed to pull bytecode");

        let program_bytes = mgr
            .get_bytecode_from_image_store(root_db, image_content_key)
            .expect("failed to get bytecode from image store");

        assert!(!program_bytes.is_empty())
    }

    #[test]
    fn image_pull_failure() {
        let mut mgr = ImageManager::new(
            SigningConfig::default().verify_enabled,
            SigningConfig::default().allow_unsigned,
        )
        .unwrap();
        let root_db =
            &init_database(get_db_config()).expect("Unable to open root database for unit test");

        let result = mgr.get_image(
            root_db,
            "quay.io/bpfman-bytecode/xdp_pass:latest",
            ImagePullPolicy::Never,
            None,
            None,
        );

        assert_matches!(result, Err(ImageError::ByteCodeImageNotfound(_)));
    }

    #[test]
    fn test_good_image_content_key() {
        struct Case {
            input: &'static str,
            output: &'static str,
        }
        let tt = vec![
            Case {
                input: "busybox",
                output: "docker.io_library_busybox_latest",
            },
            Case {
                input: "quay.io/busybox",
                output: "quay.io_busybox_latest",
            },
            Case {
                input: "docker.io/test:tag",
                output: "docker.io_library_test_tag",
            },
            Case {
                input: "quay.io/test:5000",
                output: "quay.io_test_5000",
            },
            Case {
                input: "test.com/repo:tag",
                output: "test.com_repo_tag",
            },
            Case {
                input: "test.com/repo@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
                output: "test.com_repo_sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
            },
        ];

        for t in tt {
            let good_reference: Reference = t.input.parse().unwrap();
            let image_content_key = get_image_content_key(&good_reference);
            assert_eq!(image_content_key, t.output);
        }
    }

    #[test]
    fn test_container_runtime_detection() {
        let mgr = ImageManager::new(false, false).unwrap();
        let runtime = mgr.detect_container_runtime();
        
        // Test will succeed if either Docker or Podman is installed,
        // which should be the case in development environments
        assert!(runtime.is_some(), "No container runtime detected");
    }
    
    #[test]
    fn test_image_exists_in_container_runtime() {
        let mgr = ImageManager::new(false, false).unwrap();
        
        // Skip test if no container runtime is available
        if mgr.detect_container_runtime().is_none() {
            return;
        }
        
        // Test with an image that should be commonly available
        let exists = mgr.image_exists_in_container_runtime("alpine:latest");
        
        // Pull the image if it doesn't exist yet
        if !exists {
            let _ = mgr.pull_with_container_runtime("alpine:latest", None, None);
        }
        
        // Check again after attempting to pull
        let exists_after_pull = mgr.image_exists_in_container_runtime("alpine:latest");
        assert!(exists_after_pull, "Failed to detect alpine image in container runtime");
    }
}
