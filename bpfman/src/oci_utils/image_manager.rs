// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    io::{Read, copy},
};

use anyhow::anyhow;
use base64::Engine;
use comfy_table::Table;
use flate2::read::GzDecoder;
use log::{debug, error, info, trace};
use object::{Endianness, Object};
use oci_client::{
    Client, Reference,
    client::{ClientConfig, ClientProtocol},
    manifest::{self, OciImageManifest},
    secrets::RegistryAuth,
};
use serde::Deserialize;
use serde_json::Value;
use sha2::{Digest, Sha256};
use sled::Db;
use tar::{Archive, Entry};

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
const IMAGE_PREFIX: &str = "image_";
const MANIFEST_EXTENSION: &str = "_manifest.json";

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

/// Represents a container image with eBPF bytecode and associated metadata.
///
/// This struct holds the complete contents of an OCI image that contains eBPF programs,
/// including the manifest, configuration, and the actual bytecode data.
#[derive(Debug)]
struct DatabaseImage {
    /// The OCI image manifest containing layer and configuration references
    manifest: OciImageManifest,
    /// The image configuration as a JSON string, containing eBPF labels and metadata
    config_contents: String,
    /// The raw eBPF bytecode data stored as a gzipped tar archive
    byte_code: Vec<u8>,
}

impl DatabaseImage {
    /// Extracts eBPF program and map metadata from the image configuration labels.
    ///
    /// Parses the image configuration JSON to extract eBPF-specific labels that define
    /// the programs and maps contained in the image. Supports both current and legacy
    /// label formats for backward compatibility.
    ///
    /// # Returns
    /// * `Ok(ContainerImageMetadata)` - The extracted metadata containing program and map information
    /// * `Err(ImageError)` - If labels are missing or malformed
    fn extract_image_labels(&self) -> Result<ContainerImageMetadata, ImageError> {
        let image_config: Value = serde_json::from_str(&self.config_contents)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        trace!("Raw container image config {image_config}");

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
        Ok(image_labels)
    }

    /// Verifies that the bytecode endianness matches the host system endianness.
    ///
    /// Extracts the bytecode from the gzipped tar archive and parses it as an ELF object
    /// to determine its endianness. Ensures compatibility between the bytecode and the
    /// host system before storing or loading the image.
    ///
    /// # Returns
    /// * `Ok(())` - If endianness matches the host system
    /// * `Err(ImageError::BytecodeImageExtractFailure)` - If endianness mismatch or parsing fails
    fn verify_endianness(&self) -> Result<(), ImageError> {
        let unzipped_content = self.get_bytecode_from_gzip()?;
        let obj_endianness = object::read::File::parse(unzipped_content.as_slice())
            .map_err(|e| ImageError::ByteCodeImageExtractFailure(e.to_string()))?
            .endianness();
        let host_endianness = Endianness::default();

        if host_endianness != obj_endianness {
            return Err(ImageError::ByteCodeImageExtractFailure(format!(
                "image bytecode endianness: {obj_endianness:?} does not match host {host_endianness:?}"
            )));
        };
        Ok(())
    }

    /// Verifies the integrity of the bytecode by comparing SHA256 hashes.
    ///
    /// Computes the SHA256 hash of the stored bytecode and compares it with the expected
    /// hash from the image manifest. This ensures the bytecode has not been corrupted
    /// or tampered with during storage or transmission.
    ///
    /// # Returns
    /// * `Ok(())` - If the bytecode hash matches the expected value
    /// * `Err(ImageError::ByteCodeImageCompromised)` - If hashes don't match
    fn verify_bytecode(&self) -> Result<(), ImageError> {
        let mut hasher = Sha256::new();
        copy(&mut self.byte_code.as_slice(), &mut hasher).expect("cannot copy bytecode to hasher");
        let hash = hasher.finalize();
        let expected_sha = "sha256:".to_owned() + &base16ct::lower::encode_string(&hash);
        let bytecode_sha = &self.manifest.layers[0].digest;
        if *bytecode_sha != expected_sha {
            return Err(ImageError::ByteCodeImageCompromised(format!(
                "actual SHA256: {bytecode_sha}\nexpected SHA256:{expected_sha:?}"
            )));
        }
        Ok(())
    }

    /// Extracts the eBPF bytecode from the gzipped tar archive.
    ///
    /// # Returns
    /// * `Ok(Vec<u8>)` - The raw eBPF bytecode
    /// * `Err(ImageError)` - If extraction fails or no bytecode found
    fn get_bytecode_from_gzip(&self) -> Result<Vec<u8>, ImageError> {
        let bytes = self.byte_code.clone();
        let decoder = GzDecoder::new(bytes.as_slice());

        let map_function = |mut entry: Entry<'_, GzDecoder<&[u8]>>| {
            let mut data = Vec::new();
            entry.read_to_end(&mut data).map_err(|e| {
                ImageError::ByteCodeImageParseFailure(
                    "unable to read bytecode tarball entry".to_string(),
                    e.to_string(),
                )
            })?;
            Ok(data)
        };

        let archive = Archive::new(decoder)
            .entries()
            .map_err(|e| {
                ImageError::ByteCodeImageParseFailure(
                    "unable to parse tarball entries".to_string(),
                    e.to_string(),
                )
            })?
            .filter_map(|e| e.ok())
            .map(map_function)
            .collect::<Result<Vec<Vec<u8>>, ImageError>>()?
            .first()
            .ok_or_else(|| {
                ImageError::ByteCodeImageParseFailure(
                    "no bytecode file found in tarball".to_string(),
                    "empty archive".to_string(),
                )
            })?
            .to_owned();

        Ok(archive)
    }

    /// Extracts the SHA hash portion from a digest string.
    fn extract_sha_from_digest(digest: &str) -> &str {
        digest.split(':').collect::<Vec<&str>>()[1]
    }

    /// Generates the database key for storing the image manifest.
    fn generate_manifest_key(image_content_key: &str) -> String {
        format!("{image_content_key}{MANIFEST_EXTENSION}")
    }

    /// Generates the database key for storing the image configuration.
    fn generate_config_key(image_content_key: &str, config_digest: &str) -> String {
        format!(
            "{image_content_key}_{}",
            Self::extract_sha_from_digest(config_digest)
        )
    }

    /// Generates the database key for storing the image bytecode.
    fn generate_bytecode_key(image_content_key: &str, bytecode_digest: &str) -> String {
        format!(
            "{image_content_key}_{}",
            Self::extract_sha_from_digest(bytecode_digest)
        )
    }

    /// Generates the base content key for an image from its URL.
    fn generate_image_content_key(image_url: &str) -> Result<String, ImageError> {
        let image_reference: Reference = image_url.parse().map_err(ImageError::InvalidImageUrl)?;
        let image_content_key = get_image_content_key(&image_reference);
        Ok(image_content_key)
    }

    /// Checks if an image exists in the database by looking for its manifest.
    ///
    /// Determines whether an image with the given base key is already stored in the
    /// database by checking for the existence of its manifest file. This is used
    /// to implement pull policies like "IfNotPresent".
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL
    ///
    /// # Returns
    /// * `Ok(true)` - If the image exists in the database
    /// * `Ok(false)` - If the image does not exist
    /// * `Err(ImageError::DatabaseReadError)` - If database access fails
    fn exists(root_db: &Db, image_url: &str) -> Result<bool, ImageError> {
        let image_content_key = Self::generate_image_content_key(image_url)?;
        root_db
            .contains_key(DatabaseImage::generate_manifest_key(&image_content_key))
            .map_err(|e| ImageError::DatabaseReadError(e.to_string()))
    }

    /// Saves the complete image data to the database.
    ///
    /// Stores all components of the image (manifest, configuration, and bytecode) in the
    /// database using generated keys for efficient retrieval.
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL
    /// * `image` - The image instance to save
    ///
    /// # Returns
    /// * `Ok(())` - If all components are successfully saved
    /// * `Err` - If any database operation fails
    fn save(root_db: &Db, image_url: &str, image: &Self) -> anyhow::Result<()> {
        let image_content_key = Self::generate_image_content_key(image_url)?;

        // Update Database to save for later
        let image_manifest_key = Self::generate_manifest_key(&image_content_key);

        let image_manifest_json = serde_json::to_string(&image.manifest)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        sled_insert(root_db, &image_manifest_key, image_manifest_json.as_bytes()).map_err(|e| {
            ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
        })?;

        let image_config_path =
            Self::generate_config_key(&image_content_key, &image.manifest.config.digest);

        root_db
            .insert(image_config_path, image.config_contents.as_str())
            .map_err(|e| {
                ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
            })?;

        let bytecode_path =
            Self::generate_bytecode_key(&image_content_key, &image.manifest.layers[0].digest);

        sled_insert(root_db, &bytecode_path, &image.byte_code).map_err(|e| {
            ImageError::DatabaseError("failed to write to db".to_string(), e.to_string())
        })?;

        root_db.flush().map_err(|e| {
            ImageError::DatabaseError("failed to flush db".to_string(), e.to_string())
        })?;

        Ok(())
    }

    /// Loads a complete image from the database using the given base key.
    ///
    /// Reconstructs an Image instance by loading all its components (manifest, configuration,
    /// and bytecode) from the database. Performs integrity verification on the loaded bytecode
    /// to ensure it hasn't been corrupted.
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL to load
    ///
    /// # Returns
    /// * `Ok(Image)` - The reconstructed image with verified bytecode
    /// * `Err(ImageError)` - If any component is missing, corrupted, or verification fails
    fn load(root_db: &Db, image_url: &str) -> Result<Self, ImageError> {
        let image_content_key = Self::generate_image_content_key(image_url)?;

        // Manifest
        let manifest_key = Self::generate_manifest_key(&image_content_key);
        let manifest = Self::load_manifest(root_db, &manifest_key)?;

        // Bytecode
        let bytecode_key =
            Self::generate_bytecode_key(&image_content_key, &manifest.layers[0].digest);
        debug!("bytecode is stored as tar+gzip file at key {bytecode_key}");
        let byte_code = sled_get(root_db, &bytecode_key)
            .map_err(|e| ImageError::DatabaseReadError(e.to_string()))?;

        // Config contents
        let image_config_key =
            Self::generate_config_key(&image_content_key, &manifest.config.digest);

        let db_content = sled_get(root_db, &image_config_key)
            .map_err(|e| ImageError::DatabaseReadError(e.to_string()))?;

        let config_contents = std::str::from_utf8(&db_content)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?
            .to_string();

        let image = DatabaseImage {
            manifest,
            byte_code,
            config_contents,
        };
        image.verify_bytecode().map_err(|e| match e {
            ImageError::ByteCodeImageCompromised(msg) => ImageError::ByteCodeImageCompromised(
                format!("{msg} (image_content_key: {image_content_key})"),
            ),
            _ => e,
        })?;
        Ok(image)
    }

    // Helper function to load the manifest only.
    fn load_manifest(root_db: &Db, manifest_key: &str) -> Result<OciImageManifest, ImageError> {
        let manifest = serde_json::from_str::<OciImageManifest>(
            std::str::from_utf8(
                &sled_get(root_db, manifest_key)
                    .map_err(|e| ImageError::ImageNotFound(e.to_string()))?,
            )
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?,
        )
        .map_err(|e| {
            ImageError::DatabaseError(
                "failed to parse image manifest from db".to_string(),
                e.to_string(),
            )
        })?;
        Ok(manifest)
    }

    /// Deletes a complete image from the database.
    ///
    /// Removes all components of an image (manifest, configuration, and bytecode) from the
    /// database. The image must exist in the database or an error will be returned.
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL to delete
    ///
    /// # Returns
    /// * `Vec<ImageError>` - A vector containing any errors encountered
    fn delete(root_db: &Db, image_url: &str) -> Vec<ImageError> {
        let image_content_key = match Self::generate_image_content_key(image_url) {
            Ok(key) => key,
            Err(e) => {
                return vec![e];
            }
        };

        // Manifest
        let manifest_key = Self::generate_manifest_key(&image_content_key);
        let manifest = match Self::load_manifest(root_db, &manifest_key.clone()) {
            Ok(manifest) => manifest,
            Err(e) => match e {
                ImageError::ImageNotFound(_) => {
                    return vec![ImageError::ImageNotFound(image_url.to_string())];
                }
                _ => return vec![e],
            },
        };

        let mut delete_errors = vec![];

        if let Err(e) = root_db.remove(&manifest_key) {
            delete_errors.push(ImageError::ImageDeleteError(e.to_string()));
        } else {
            debug!("Deleted manifest key: {manifest_key}");
        }

        // Bytecode
        let bytecode_key =
            Self::generate_bytecode_key(&image_content_key, &manifest.layers[0].digest);
        if let Err(e) = root_db.remove(&bytecode_key) {
            delete_errors.push(ImageError::DatabaseDeleteError(e.to_string()));
        } else {
            debug!("Deleted bytecode key: {bytecode_key}");
        }

        // Config contents
        let image_config_key =
            Self::generate_config_key(&image_content_key, &manifest.config.digest);
        if let Err(e) = root_db.remove(&image_config_key) {
            delete_errors.push(ImageError::DatabaseDeleteError(e.to_string()));
        } else {
            debug!("Deleted image config key: {image_config_key}");
        }

        delete_errors
    }

    // list_images lists all images in the database. Valid images start with
    // `IMAGE_PREFIX` and end with `MANIFEST_EXTENSION`.
    // # Arguments
    // * `root_db` - The database connection
    // # Returns
    // * `(Vec<String>, Vec<ImageError>)` - A tuple containing a list of image names and any errors encountered
    fn list(root_db: &Db) -> (Vec<String>, Vec<ImageError>) {
        let mut image_names = vec![];
        let mut image_errors = vec![];
        for res in root_db.into_iter() {
            let (key, _) = match res {
                Ok(key_val) => key_val,
                Err(e) => {
                    image_errors.push(ImageError::DatabaseError(
                        "could not parse key from database".to_string(),
                        e.to_string(),
                    ));
                    continue;
                }
            };
            let key_string = String::from_utf8_lossy(&key).to_string();
            if key_string.starts_with(IMAGE_PREFIX) && key_string.ends_with(MANIFEST_EXTENSION) {
                let end = key_string.len() - MANIFEST_EXTENSION.len();
                let image_content_key = &key_string[..end];
                match parse_image_content_key(image_content_key) {
                    Ok(image_name) => image_names.push(image_name),
                    Err(e) => image_errors.push(e),
                }
            }
        }
        (image_names, image_errors)
    }
}

pub struct ImageManager {
    client: Client,
    cosign_verifier: Option<CosignVerifier>,
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

    /// Retrieves an image from the database or pulls it from a registry based on the pull policy.
    /// Returns the image and a list of program names found in the image.
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL to retrieve
    /// * `pull_policy` - The policy for when to pull the image
    /// * `username` - Optional username for registry authentication
    /// * `password` - Optional password for registry authentication
    ///
    /// # Returns
    /// * `Ok((Image, Vec<String>))` - The image and list of program names
    /// * `Err(ImageError)` - If retrieval or pulling fails
    pub(crate) fn get_image(
        &mut self,
        root_db: &Db,
        image_url: &str,
        pull_policy: ImagePullPolicy,
        username: Option<String>,
        password: Option<String>,
    ) -> Result<(Vec<u8>, Vec<String>), ImageError> {
        if let Some(cosign_verifier) = &mut self.cosign_verifier {
            cosign_verifier.verify(image_url, username.as_deref(), password.as_deref())?;
        } else {
            info!("Cosign verification is disabled, so skipping verification");
        }

        let exists: bool = DatabaseImage::exists(root_db, image_url)?;
        let image = match pull_policy {
            ImagePullPolicy::Always => self.pull_image(root_db, image_url, username, password)?,
            ImagePullPolicy::IfNotPresent => {
                if exists {
                    DatabaseImage::load(root_db, image_url)?
                } else {
                    self.pull_image(root_db, image_url, username, password)?
                }
            }
            ImagePullPolicy::Never => {
                if exists {
                    DatabaseImage::load(root_db, image_url)?
                } else {
                    Err(ImageError::ByteCodeImageNotfound(image_url.to_string()))?
                }
            }
        };
        let bytecode = image.get_bytecode_from_gzip()?;
        let image_meta = image.extract_image_labels()?;
        Ok((bytecode, image_meta.programs.into_keys().collect()))
    }

    /// Pulls an image from a registry and saves it to the database.
    /// Most of the business logic is implemented in async_pull_image.
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL to pull
    /// * `username` - Optional username for registry authentication
    /// * `password` - Optional password for registry authentication
    ///
    /// # Returns
    /// * `Ok(Image)` - The pulled and saved image
    /// * `Err(ImageError)` - If pulling or saving fails
    fn pull_image(
        &mut self,
        root_db: &Db,
        image_url: &str,
        username: Option<String>,
        password: Option<String>,
    ) -> Result<DatabaseImage, ImageError> {
        let client = self.client.clone();

        let image = rt().unwrap().block_on(async_pull_image(
            client,
            root_db.clone(),
            image_url,
            username,
            password,
        ))?;
        Ok(image)
    }

    /// Deletes a complete image from the database.
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    /// * `image_url` - The image URL to delete
    ///
    /// # Returns
    /// * `Ok(())` - If success
    /// * `Err(ImageErroror) - If failure
    pub fn delete_image(&mut self, root_db: &Db, image_url: &str) -> Result<(), ImageError> {
        let delete_errors = DatabaseImage::delete(root_db, image_url);
        if !delete_errors.is_empty() {
            let error_messages: Vec<String> = delete_errors.iter().map(|e| e.to_string()).collect();
            let error_message = error_messages.join(", ");
            return Err(ImageError::DatabaseDeleteError(error_message));
        }
        Ok(())
    }

    /// Prints (and returns) all container images stored in the database
    ///
    /// # Arguments
    /// * `root_db` - The database connection
    ///
    /// # Returns
    /// * `Ok(Vec<String>)` - If listing succeeds
    /// * `Err(ImageError)` - If listing fails
    pub fn list_images(&mut self, root_db: &Db) -> Result<Vec<String>, ImageError> {
        let (images, image_errors) = DatabaseImage::list(root_db);
        let mut table = Table::new();
        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec!["Image"]);
        for v in &images {
            table.add_row(vec![v]);
        }
        println!("{table}");
        if !image_errors.is_empty() {
            // return joined image_errors as anyhow::Error
            let error_messages: Vec<String> = image_errors.iter().map(|e| e.to_string()).collect();
            let error_message = error_messages.join(", ");
            return Err(ImageError::DatabaseError(
                "One or more images could not be listed".to_string(),
                error_message,
            ));
        }
        Ok(images)
    }
}

fn serde_label(labels: &Value, label_type: String) -> Result<HashMap<String, String>, ImageError> {
    debug!("found {label_type} labels - {labels:?}");
    let val = match serde_json::from_str::<HashMap<String, String>>(
        labels.as_str().unwrap_or_default(),
    ) {
        Ok(l) => l,
        Err(e) => {
            let err_str = format!("error pulling image, invalid image {label_type} label");
            error!("{err_str}");
            return Err(ImageError::ByteCodeImageParseFailure(
                err_str,
                e.to_string(),
            ));
        }
    };

    Ok(val)
}

/// Generates a base64-encoded content key for an image from its reference.
///
/// Converts an OCI image reference to a safe database key by base64 encoding the full image URL.
/// This approach avoids character conflicts that can occur with Docker images containing underscores,
/// forward slashes, or other special characters. The key format is: `image_<base64_encoded_url>`
///
/// # Arguments
/// * `image` - The OCI image reference to convert
///
/// # Returns
/// * `String` - The base64-encoded database key with IMAGE_PREFIX
fn get_image_content_key(image: &Reference) -> String {
    // Try to get the tag, if it doesn't exist, get the digest
    // if neither exist, return "latest" as the tag
    let tag = match image.tag() {
        Some(tag) => format!(":{tag}"),
        _ => match image.digest() {
            Some(digest) => format!("@{digest}"),
            _ => "latest".to_string(),
        },
    };
    let image_url = format!("{}/{}{tag}", image.registry(), image.repository());
    let encoded = base64::engine::general_purpose::STANDARD.encode(image_url);
    format!("{IMAGE_PREFIX}{encoded}")
}

/// Parses a base64-encoded image content key back to the original image URL.
///
/// Reverses the process of `get_image_content_key` by extracting the base64-encoded portion
/// from a database key and decoding it back to the original image URL. This enables
/// accurate reconstruction of image URLs even when they contain special characters.
///
/// # Arguments
/// * `image_content_key` - The base64-encoded database key to parse
///
/// # Returns
/// * `Ok(String)` - The decoded original image URL
/// * `Err(ImageError::DatabaseError)` - If the key is malformed
fn parse_image_content_key(image_content_key: &str) -> Result<String, ImageError> {
    if !image_content_key.starts_with(IMAGE_PREFIX) {
        return Err(ImageError::DatabaseError(
            "invalid image key".to_string(),
            image_content_key.to_string(),
        ));
    }
    let start = IMAGE_PREFIX.len();
    let encoded = &image_content_key[start..];
    let decoded = base64::engine::general_purpose::STANDARD
        .decode(encoded)
        .map_err(|e| {
            ImageError::DatabaseError("could not decode image key".to_string(), e.to_string())
        })?;
    Ok(String::from_utf8_lossy(&decoded).to_string())
}

/// Asynchronously pulls an image from a registry, verifies it, and saves it to the database.
///
/// # Arguments
/// * `client` - The OCI client for pulling images
/// * `root_db` - The database connection
/// * `image_url` - The image URL to pull
/// * `username` - Optional username for registry authentication
/// * `password` - Optional password for registry authentication
///
/// # Returns
/// * `Ok(Image)` - The pulled, verified, and saved image
/// * `Err(ImageError)` - If pulling, verification, or saving fails
async fn async_pull_image(
    client: Client,
    root_db: Db,
    image_url: &str,
    username: Option<String>,
    password: Option<String>,
) -> Result<DatabaseImage, ImageError> {
    // The reference created here is created using the krustlet oci-distribution
    // crate. It currently contains many defaults more of which can be seen
    // here: https://github.com/krustlet/oci-distribution/blob/main/src/reference.rs#L58
    let image: Reference = image_url.parse().map_err(ImageError::InvalidImageUrl)?;
    let auth = get_auth_for_registry(username, password);
    let (image_manifest, _, config_contents) = client
        .pull_manifest_and_config(&image.clone(), &auth)
        .await
        .map_err(ImageError::ByteCodeImagePullFailure)?;
    trace!("Raw container image manifest {image_manifest}");

    debug!("Pulling bytecode from image path: {image_url}");
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
        .map_err(ImageError::ByteCodeImagePullFailure)?
        .layers
        .into_iter()
        .next()
        .map(|layer| layer.data)
        .ok_or(ImageError::ByteCodeImageExtractFailure(
            "No data in bytecode image layer".to_string(),
        ))?;

    let image = DatabaseImage {
        config_contents,
        manifest: image_manifest,
        byte_code: image_content,
    };
    image.verify_endianness()?;
    DatabaseImage::save(&root_db, image_url, &image)?;
    Ok(image)
}

fn get_auth_for_registry(username: Option<String>, password: Option<String>) -> RegistryAuth {
    match (username, password) {
        (Some(username), Some(password)) => RegistryAuth::Basic(username, password),
        _ => RegistryAuth::Anonymous,
    }
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
        let image_url = "quay.io/bpfman-bytecode/go-xdp-counter-legacy-labels:latest";
        let (program_bytes, _) = mgr
            .get_image(root_db, image_url, ImagePullPolicy::Always, None, None)
            .expect("failed to pull bytecode");
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
        let image_url = "quay.io/bpfman-bytecode/go-xdp-counter:latest";
        let (program_bytes, _) = mgr
            .get_image(root_db, image_url, ImagePullPolicy::Always, None, None)
            .expect("failed to pull bytecode");
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

        let _ = mgr
            .get_image(
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

        let image_url = "quay.io/bpfman-bytecode/xdp_pass_private:latest";
        let (program_bytes, _) = mgr
            .get_image(
                root_db,
                image_url,
                ImagePullPolicy::Always,
                Some("bpfman-bytecode+bpfmancreds".to_owned()),
                Some("D49CKWI1MMOFGRCAT8SHW5A56FSVP30TGYX54BBWKY2J129XRI6Q5TVH2ZZGTJ1M".to_owned()),
            )
            .expect("failed to pull bytecode");
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
}
