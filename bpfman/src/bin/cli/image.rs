// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use base64::{engine::general_purpose, Engine};
use bpfman::{oci_utils::image_manager::BytecodeImage, BpfManager};
use bpfman_api::ImagePullPolicy;

use crate::args::{ImageSubCommand, PullBytecodeArgs};

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
        let image_pull_policy: ImagePullPolicy = value.pull_policy.as_str().try_into()?;
        let (username, password) = match &value.registry_auth {
            Some(a) => {
                let auth_raw = general_purpose::STANDARD.decode(a)?;
                let auth_string = String::from_utf8(auth_raw)?;
                let (username, password) = auth_string.split_once(':').unwrap();
                (Some(username.to_owned()), Some(password.to_owned()))
            }
            None => (None, None),
        };

        Ok(BytecodeImage {
            image_url: value.image_url.clone(),
            image_pull_policy,
            username,
            password,
        })
    }
}

pub(crate) async fn execute_pull(
    bpf_manager: &mut BpfManager,
    args: &PullBytecodeArgs,
) -> anyhow::Result<()> {
    let image: BytecodeImage = args.try_into()?;
    bpf_manager.pull_bytecode(image).await?;

    Ok(())
}
