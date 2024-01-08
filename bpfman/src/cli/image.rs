// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use base64::{engine::general_purpose, Engine};
use bpfman_api::{
    v1::{bpfman_client::BpfmanClient, BytecodeImage, PullBytecodeRequest},
    ImagePullPolicy,
};

use crate::cli::{
    args::{ImageSubCommand, PullBytecodeArgs},
    select_channel,
};

impl ImageSubCommand {
    pub(crate) async fn execute(&self) -> anyhow::Result<()> {
        match self {
            ImageSubCommand::Pull(args) => execute_pull(args).await,
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
            url: value.image_url.clone(),
            image_pull_policy: pull_policy.into(),
            username: Some(username),
            password: Some(password),
        })
    }
}

pub(crate) async fn execute_pull(args: &PullBytecodeArgs) -> anyhow::Result<()> {
    let channel = select_channel().expect("failed to select channel");
    let mut client = BpfmanClient::new(channel);
    let image: BytecodeImage = args.try_into()?;
    let request = tonic::Request::new(PullBytecodeRequest { image: Some(image) });
    let _response = client.pull_bytecode(request).await?;
    Ok(())
}
