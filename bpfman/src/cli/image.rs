// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use base64::{engine::general_purpose, Engine};
use bpfman_api::{
    config::Config,
    v1::{bpfman_client::BpfmanClient, BytecodeImage, PullBytecodeRequest},
    ImagePullPolicy,
};
use clap::{Args, Subcommand};

use crate::cli::select_channel;

#[derive(Subcommand, Debug)]
pub(crate) enum ImageSubCommand {
    /// Pull an eBPF bytecode image from a remote registry.
    Pull(PullBytecodeArgs),
}

impl ImageSubCommand {
    pub(crate) fn execute(&self, config: &mut Config) -> anyhow::Result<()> {
        match self {
            ImageSubCommand::Pull(args) => execute_pull(args, config),
        }
    }
}

#[derive(Args, Debug)]
pub(crate) struct PullBytecodeArgs {
    /// Required: Container Image URL.
    /// Example: --image-url quay.io/bpfman-bytecode/xdp_pass:latest
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) image_url: String,

    /// Optional: Registry auth for authenticating with the specified image registry.
    /// This should be base64 encoded from the '<username>:<password>' string just like
    /// it's stored in the docker/podman host config.
    /// Example: --registry_auth "YnjrcKw63PhDcQodiU9hYxQ2"
    #[clap(short, long, verbatim_doc_comment)]
    registry_auth: Option<String>,

    /// Optional: Pull policy for remote images.
    ///
    /// [possible values: Always, IfNotPresent, Never]
    #[clap(short, long, verbatim_doc_comment, default_value = "IfNotPresent")]
    pull_policy: String,
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

pub(crate) fn execute_pull(args: &PullBytecodeArgs, config: &mut Config) -> anyhow::Result<()> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let channel = select_channel(config).expect("failed to select channel");
            let mut client = BpfmanClient::new(channel);
            let image: BytecodeImage = args.try_into()?;
            let request = tonic::Request::new(PullBytecodeRequest { image: Some(image) });
            let _response = client.pull_bytecode(request).await?;
            Ok::<(), anyhow::Error>(())
        })
}
