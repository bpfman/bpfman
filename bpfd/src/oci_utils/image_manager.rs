// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{
    fs,
    fs::{create_dir_all, read_to_string, File, OpenOptions},
    io::{copy, Read, Write},
    path::{Path, PathBuf},
};

use anyhow::Context;
use bpfd_api::{util::directories::BYTECODE_IMAGE_CONTENT_STORE, ImagePullPolicy};
use flate2::read::GzDecoder;
use log::debug;
use oci_distribution::{
    client, manifest, manifest::OciImageManifest, secrets::RegistryAuth, Client, Reference,
};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use tar::Archive;

use super::ImageError;
use crate::utils::read;

#[derive(Debug, Deserialize, Default)]
pub struct ContainerImageMetadata {
    #[serde(rename(deserialize = "io.ebpf.program_name"))]
    pub name: String,
    #[serde(rename(deserialize = "io.ebpf.section_name"))]
    pub section_name: String,
    #[serde(rename(deserialize = "io.ebpf.program_type"))]
    pub program_type: String,
    #[serde(rename(deserialize = "io.ebpf.filename"))]
    pub filename: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) struct BytecodeImage {
    image_url: String,
    image_pull_policy: ImagePullPolicy,
    username: Option<String>,
    password: Option<String>,
}

#[derive(Debug, Deserialize, Default)]
pub struct ProgramOverrides {
    pub path: String,
    pub image_meta: ContainerImageMetadata,
}

impl BytecodeImage {
    pub(crate) fn new(
        image_url: String,
        image_pull_policy: i32,
        username: Option<String>,
        password: Option<String>,
    ) -> Self {
        Self {
            image_url,
            image_pull_policy: image_pull_policy
                .try_into()
                .expect("Unable to parse ImagePullPolicy"),
            username,
            password,
        }
    }

    pub(crate) fn get_url(&self) -> &str {
        &self.image_url
    }

    pub(crate) fn get_pull_policy(self) -> ImagePullPolicy {
        self.image_pull_policy
    }

    pub(crate) async fn get_image(
        self,
        base_dir: Option<String>,
    ) -> Result<ProgramOverrides, ImageError> {
        // The reference created here is created using the krustlet oci-distribution
        // crate. It currently contains many defaults more of which can be seen
        // here: https://github.com/krustlet/oci-distribution/blob/main/src/reference.rs#L58
        let image: Reference = self
            .image_url
            .parse()
            .map_err(ImageError::InvalidImageUrl)?;

        let mut image_content_path = get_image_content_dir(image.clone());

        // exclusively used for local unit testing
        image_content_path = match base_dir {
            Some(b) => Path::new(&format!("{}{}", b, image_content_path.display())).to_path_buf(),
            None => image_content_path,
        };

        // Make sure the actual image manifest exists so that we are sure the content is there
        let exists: bool = image_content_path.join("manifest.json").exists();

        let image_meta = match self.image_pull_policy {
            ImagePullPolicy::Always => self.pull_image(image, image_content_path.clone()).await?,
            ImagePullPolicy::IfNotPresent => {
                if exists {
                    load_image_meta(image_content_path.clone())?
                } else {
                    self.pull_image(image, image_content_path.clone()).await?
                }
            }
            ImagePullPolicy::Never => {
                if exists {
                    load_image_meta(image_content_path.clone())?
                } else {
                    Err(ImageError::ByteCodeImageNotfound(image.to_string()))?
                }
            }
        };

        Ok(ProgramOverrides {
            path: image_content_path.into_os_string().into_string().unwrap(),
            image_meta,
        })
    }

    pub async fn pull_image(
        self,
        image: Reference,
        content_dir: PathBuf,
    ) -> Result<ContainerImageMetadata, ImageError> {
        debug!("Pulling bytecode from image path: {}", self.image_url);
        let protocol = client::ClientProtocol::Https;

        let auth = self.get_registry_auth();

        let config = client::ClientConfig {
            protocol,
            ..Default::default()
        };

        let mut client = Client::new(config);

        // prep on disk storage for image
        let content_dir = prepare_storage_for_image(content_dir)?;

        let (image_manifest, _, config_contents) = client
            .pull_manifest_and_config(&image.clone(), &auth)
            .await
            .map_err(ImageError::ImageManifestPullFailure)?;

        debug!("Raw container image manifest {}", image_manifest);

        let image_manifest_path = Path::new(&content_dir).join("manifest.json");

        let image_manifest_file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .open(image_manifest_path.clone())
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        serde_json::to_writer_pretty(
            image_manifest_file
                .try_clone()
                .expect("failed to clone image_manifest_file"),
            &image_manifest.clone(),
        )
        .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        let config_sha = &image_manifest
            .config
            .digest
            .split(':')
            .collect::<Vec<&str>>()[1];

        let image_config_path = Path::new(&content_dir).join(config_sha);

        let image_config_file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .open(image_config_path.clone())
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        let bytecode_sha = image_manifest.layers[0]
            .digest
            .split(':')
            .collect::<Vec<&str>>()[1];
        let bytecode_path = Path::new(&content_dir).join(bytecode_sha);

        let mut image_bytecode_file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .open(bytecode_path.clone())
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        let image_config: Value = serde_json::from_str(&config_contents).unwrap();
        debug!("Raw container image config {}", image_config);

        // Deserialize image metadata(labels) from json config
        let image_labels: ContainerImageMetadata =
            serde_json::from_str(&image_config["config"]["Labels"].to_string())
                .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        serde_json::to_writer_pretty(
            image_config_file
                .try_clone()
                .expect("failed to clone image_config_file"),
            &image_config,
        )
        .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

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
            .ok_or(ImageError::BytecodeImageExtractFailure)?;

        image_bytecode_file
            .write_all(image_content.as_slice())
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        // once all file writing is complete set all files to r/o
        let mut image_manifest_perms = image_manifest_file
            .metadata()
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?
            .permissions();

        image_manifest_perms.set_readonly(true);
        fs::set_permissions(image_manifest_path, image_manifest_perms)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        let mut image_config_perms = image_config_file
            .metadata()
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?
            .permissions();

        image_config_perms.set_readonly(true);
        fs::set_permissions(image_config_path, image_config_perms)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        let mut bytecode_perms = image_bytecode_file
            .metadata()
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?
            .permissions();

        bytecode_perms.set_readonly(true);
        fs::set_permissions(bytecode_path.clone(), bytecode_perms)
            .map_err(|e| ImageError::ByteCodeImageProcessFailure(e.into()))?;

        Ok(image_labels)
    }

    fn get_registry_auth(self) -> RegistryAuth {
        match (self.username, self.password) {
            (Some(user), Some(pass)) => RegistryAuth::Basic(user, pass),
            _ => RegistryAuth::Anonymous,
        }
    }
}

impl From<bpfd_api::v1::BytecodeImage> for BytecodeImage {
    fn from(value: bpfd_api::v1::BytecodeImage) -> Self {
        BytecodeImage::new(
            value.url,
            value.image_pull_policy,
            match value.username.as_ref() {
                "" => None,
                u => Some(u.to_string()),
            },
            match value.password.as_ref() {
                "" => None,
                p => Some(p.to_string()),
            },
        )
    }
}

fn get_image_content_dir(image: Reference) -> PathBuf {
    // Try to get the tag, if it doesn't exist, get the digest
    // if neither exist, return "latest" as the tag
    let tag = match image.tag() {
        Some(t) => t,
        _ => match image.digest() {
            Some(d) => d,
            _ => "latest",
        },
    };

    Path::new(&format!(
        "{}/{}/{}/{}",
        BYTECODE_IMAGE_CONTENT_STORE,
        image.registry(),
        image.repository(),
        tag
    ))
    .to_owned()
}

fn prepare_storage_for_image(image_dir: PathBuf) -> Result<PathBuf, ImageError> {
    debug!(
        "Creating oci image content store at: {}",
        image_dir.display()
    );
    create_dir_all(image_dir.clone())
        .context(format!(
            "unable to create repo directory for image URL: {}",
            image_dir.display()
        ))
        .map_err(ImageError::ByteCodeImageProcessFailure)?;

    Ok(image_dir)
}

fn load_image_manifest(image_dir: PathBuf) -> Result<OciImageManifest, anyhow::Error> {
    let manifest_path = image_dir.join("manifest.json");

    // Get image manifest from disk
    let file_content = read_to_string(manifest_path).context(format!(
        "failed to read image manifest file {}",
        image_dir.display()
    ))?;
    Ok(serde_json::from_str::<OciImageManifest>(&file_content)?)
}

fn load_image_meta(image_content_path: PathBuf) -> Result<ContainerImageMetadata, anyhow::Error> {
    // Get image config from disk
    let image_manifest = load_image_manifest(image_content_path.clone())?;

    let config_sha = &image_manifest
        .config
        .digest
        .split(':')
        .collect::<Vec<&str>>()[1];

    let image_config_path = image_content_path.join(config_sha);
    let file_content =
        read_to_string(image_config_path).context("failed to read image config file")?;

    let image_config: Value = serde_json::from_str(&file_content).unwrap();
    debug!(
        "Raw container image config {}",
        &image_config["config"]["Labels"].to_string()
    );

    Ok(serde_json::from_str::<ContainerImageMetadata>(
        &image_config["config"]["Labels"].to_string(),
    )?)
}

pub(crate) async fn get_bytecode_from_image_store(
    content_dir: String,
) -> Result<Vec<u8>, anyhow::Error> {
    debug!("bytecode is stored as tar+gzip file at {}", content_dir);
    let image_dir = Path::new(&content_dir);
    // Get image manifest from disk
    let manifest = load_image_manifest(image_dir.to_path_buf())?;

    let bytecode_sha = &manifest.layers[0].digest;

    let bytecode_path = image_dir.join(bytecode_sha.clone().split(':').collect::<Vec<&str>>()[1]);

    let mut f = File::open(bytecode_path.clone()).context("failed to open compressed bytecode")?;

    let mut hasher = Sha256::new();
    copy(&mut f, &mut hasher)?;
    let hash = hasher.finalize();
    let expected_sha = "sha256:".to_owned() + &base16ct::lower::encode_string(&hash);

    if *bytecode_sha != expected_sha {
        debug!(
            "actual SHA256: {}\nexpected SHA256: {:?}",
            bytecode_sha, expected_sha
        );
        panic!("Bpf Bytecode has been compromised")
    }

    // The data is of OCI media type "application/vnd.oci.image.layer.v1.tar+gzip" or
    // "application/vnd.docker.image.rootfs.diff.tar.gzip"
    // decode and unpack to access bytecode
    let buf = read(bytecode_path).await?;
    let unzipped_tarball = GzDecoder::new(buf.as_slice());

    return Ok(Archive::new(unzipped_tarball)
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
        .to_owned());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn image_pull_and_bytecode_verify() {
        let tmpdir = tempfile::tempdir().unwrap();
        std::env::set_current_dir(&tmpdir).unwrap();

        let image = BytecodeImage {
            image_url: "quay.io/bpfd-bytecode/xdp_pass:latest".to_owned(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        };

        let program_overrides = image
            .get_image(Some(tmpdir.path().to_str().unwrap().to_owned()))
            .await
            .expect("failed to pull bytecode");

        assert!(Path::new(&program_overrides.path).exists());

        let program_bytes = get_bytecode_from_image_store(program_overrides.path)
            .await
            .expect("failed to get bytecode from image store");

        assert!(program_bytes.len() > 0)
    }

    #[tokio::test]
    #[should_panic]
    async fn private_image_pull_faiure() {
        let tmpdir = tempfile::tempdir().unwrap();
        std::env::set_current_dir(&tmpdir).unwrap();

        let image = BytecodeImage {
            image_url: "quay.io/bpfd-bytecode/xdp_pass_private:latest".to_owned(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        };

        image
            .get_image(Some(tmpdir.path().to_str().unwrap().to_owned()))
            .await
            .expect("failed to pull bytecode");
    }

    #[tokio::test]
    async fn private_image_pull_and_bytecode_verify() {
        let tmpdir = tempfile::tempdir().unwrap();
        std::env::set_current_dir(&tmpdir).unwrap();

        let image = BytecodeImage {
            image_url: "quay.io/bpfd-bytecode/xdp_pass_private:latest".to_owned(),
            image_pull_policy: ImagePullPolicy::Always,
            username: Some("bpfd-bytecode+bpfdcreds".to_owned()),
            password: Some(
                "JOGZ3FA6A9L2297JAT4FFN6CJU87LKTIY6X1ZGKWJ0W0XLKY0KPT5YKTBBEAGSF5".to_owned(),
            ),
        };

        let program_overrides = image
            .get_image(Some(tmpdir.path().to_str().unwrap().to_owned()))
            .await
            .expect("failed to pull bytecode");

        assert!(Path::new(&program_overrides.path).exists());

        let program_bytes = get_bytecode_from_image_store(program_overrides.path)
            .await
            .expect("failed to get bytecode from image store");

        assert!(program_bytes.len() > 0)
    }

    #[tokio::test]
    async fn image_pull_failure() {
        let tmpdir = tempfile::tempdir().unwrap();
        std::env::set_current_dir(&tmpdir).unwrap();

        let image = BytecodeImage {
            image_url: "quay.io/bpfd-bytecode/xdp_pass:latest".to_owned(),
            image_pull_policy: ImagePullPolicy::Never,
            username: None,
            password: None,
        };

        let result = image
            .get_image(Some(tmpdir.path().to_str().unwrap().to_owned()))
            .await;

        assert!(matches!(result, Err(ImageError::ByteCodeImageNotfound(_))));
    }

    #[test]
    fn test_good_image_content_path() {
        struct Case {
            input: &'static str,
            output: PathBuf,
        }
        let tt = vec![
            Case{input: "busybox", output: PathBuf::from("/var/lib/bpfd/io.bpfd.image.content/docker.io/library/busybox/latest")},
            Case{input:"quay.io/busybox", output: PathBuf::from("/var/lib/bpfd/io.bpfd.image.content/quay.io/busybox/latest")},
            Case{input:"docker.io/test:tag", output: PathBuf::from("/var/lib/bpfd/io.bpfd.image.content/docker.io/library/test/tag")},
            Case{input:"quay.io/test:5000", output: PathBuf::from("/var/lib/bpfd/io.bpfd.image.content/quay.io/test/5000")},
            Case{input:"test.com/repo:tag", output: PathBuf::from("/var/lib/bpfd/io.bpfd.image.content/test.com/repo/tag")},
            Case{
                input:"test.com/repo@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
                output: PathBuf::from("/var/lib/bpfd/io.bpfd.image.content/test.com/repo/sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
            }
        ];
        for t in tt {
            let good_reference: Reference = t.input.parse().unwrap();
            let image_content_path = get_image_content_dir(good_reference);
            assert_eq!(image_content_path, t.output);
        }
    }
}
