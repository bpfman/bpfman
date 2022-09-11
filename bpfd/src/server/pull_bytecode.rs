// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use flate2::read::GzDecoder;
use log::debug;
use oci_distribution::{client, manifest, secrets::RegistryAuth, Client, Reference};
use serde::Deserialize;
use serde_json::Value;
use tar::Archive;
use thiserror::Error;

const CONTAINERIZED_BYTECODE_PATH: &str = "/var/bpfd/bytecode/";

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

#[derive(Debug, Error)]
pub enum ImageError {
    #[error("Failed to Parse bytecode Image URL: {0}")]
    InvalidImageUrl(#[source] oci_distribution::ParseError),
    #[error("Failed to pull bytecode Image manifest: {0}")]
    ImageManifestPullFailure(#[source] oci_distribution::errors::OciDistributionError),
    #[error("Failed to pull bytecode Image: {0}")]
    BytecodeImagePullFailure(#[source] oci_distribution::errors::OciDistributionError),
    #[error("Failed to extract bytecode from Image")]
    BytecodeImageExtractFailure,
}

#[derive(Debug, Deserialize, Default)]
pub struct ProgramOverrides {
    pub path: String,
    pub image_meta: ContainerImageMetadata,
}

pub async fn pull_bytecode(image_url: &String) -> Result<ProgramOverrides, anyhow::Error> {
    debug! {"Pulling bytecode from image path: {}", image_url}
    let image: Reference = image_url.parse().map_err(ImageError::InvalidImageUrl)?;

    let protocol = client::ClientProtocol::Https;

    // TODO(astoycos): Add option/flag to authenticate against private image repositories
    // https://github.com/redhat-et/bpfd/issues/119
    let auth = RegistryAuth::Anonymous;

    let config = client::ClientConfig {
        protocol,
        ..Default::default()
    };

    let mut client = Client::new(config);

    let (image_manifest, _, config_contents) = client
        .pull_manifest_and_config(&image, &auth)
        .await
        .map_err(ImageError::ImageManifestPullFailure)?;

    debug!("Raw container image manifest {}", image_manifest);

    let image_config: Value = serde_json::from_str(&config_contents).unwrap();
    debug!("Raw container image config {}", image_config);

    // Deserialize image metadata(labels) from json config
    let image_labels: ContainerImageMetadata =
        serde_json::from_str(&image_config["config"]["Labels"].to_string())?;

    let image_content = client
        .pull(&image, &auth, vec![manifest::IMAGE_LAYER_GZIP_MEDIA_TYPE])
        .await
        .map_err(ImageError::BytecodeImagePullFailure)?
        .layers
        .into_iter()
        .next()
        .map(|layer| layer.data)
        .ok_or(ImageError::BytecodeImageExtractFailure)?;

    let bytecode_path = CONTAINERIZED_BYTECODE_PATH.to_owned() + &image_labels.filename;

    // Create bytecode directory if not exists
    std::fs::create_dir_all(CONTAINERIZED_BYTECODE_PATH)?;

    // Data is of OCI media type "application/vnd.oci.image.layer.v1.tar+gzip"
    // decode and unpack to access bytecode
    let unzipped_tarball = GzDecoder::new(image_content.as_slice());
    let mut tarball = Archive::new(unzipped_tarball);
    tarball.set_preserve_permissions(true); 
    tarball.unpack(CONTAINERIZED_BYTECODE_PATH).unwrap();

    Ok(ProgramOverrides {
        path: bytecode_path,
        image_meta: image_labels,
    })
}
