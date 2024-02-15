// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use base64::{engine::general_purpose, Engine};
use bpfman_api::ImagePullPolicy;
use log::info;
use tokio::sync::oneshot;

use crate::{
    bpf::BpfManager,
    cli::args::{ImageSubCommand, PullBytecodeArgs},
    errors::BpfmanError,
    oci_utils::image_manager::{BytecodeImage, Command as ImageManagerCommand},
};

impl ImageSubCommand {
    pub(crate) async fn execute(&self, bpf_manager: &mut BpfManager) -> anyhow::Result<()> {
        match self {
            ImageSubCommand::Pull(args) => execute_pull(bpf_manager, args).await,
        }
    }
}

impl TryFrom<&PullBytecodeArgs> for BytecodeImage {
    type Error = anyhow::Error;

    fn try_from(value: &PullBytecodeArgs) -> Result<Self, Self::Error> {
        let pull_policy: ImagePullPolicy = value.pull_policy.as_str().try_into()?;
        let (username, password) = match &value.registry_auth {
            Some(a) => {
                let auth_raw = general_purpose::STANDARD.decode(a)?;
                let auth_string = String::from_utf8(auth_raw)?;
                let (username, password) = auth_string.split_once(':').unwrap();
                (username.to_owned(), password.to_owned())
            }
            None => ("".to_owned(), "".to_owned()),
        };

        Ok(BytecodeImage {
            image_url: value.image_url.clone(),
            image_pull_policy: pull_policy,
            username: Some(username),
            password: Some(password),
        })
    }
}

pub(crate) async fn execute_pull(
    bpf_manager: &mut BpfManager,
    args: &PullBytecodeArgs,
) -> anyhow::Result<()> {
    let image: BytecodeImage = args.try_into()?;
    let (tx, rx) = oneshot::channel();
    let res;
    if let Some(image_manager) = bpf_manager.image_manager.clone() {
        image_manager
            .send(ImageManagerCommand::Pull {
                image: image.image_url,
                pull_policy: image.image_pull_policy.clone(),
                username: image.username.clone(),
                password: image.password.clone(),
                resp: tx,
            })
            .await?;
        res = match rx.await? {
            Ok(_) => {
                info!("Successfully pulled bytecode");
                Ok(())
            }
            Err(e) => Err(BpfmanError::BpfBytecodeError(e)),
        };
    } else {
        res = Err(BpfmanError::InternalError(
            "ImageManager not set.".to_string(),
        ));
    }

    res?;

    Ok(())
}
