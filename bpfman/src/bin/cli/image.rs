// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{collections::HashMap, env, fs, path::Path, process::Command};

use anyhow::{anyhow, Context, Result};
use aya_obj::Object;
use base64::{engine::general_purpose, Engine};
use bpfman::{
    pull_bytecode,
    types::{BytecodeImage, ImagePullPolicy, MapType, ProgramType},
};
use log::debug;
use object::Endianness;

use crate::args::{BuildBytecodeArgs, GenerateArgs, ImageSubCommand, PullBytecodeArgs};

impl ImageSubCommand {
    pub(crate) async fn execute(&self) -> anyhow::Result<()> {
        match self {
            ImageSubCommand::Pull(args) => execute_pull(args).await,
            ImageSubCommand::Build(args) => execute_build(args).await,
            ImageSubCommand::GenerateBuildArgs(args) => execute_build_args(args).await,
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

pub(crate) async fn execute_pull(args: &PullBytecodeArgs) -> anyhow::Result<()> {
    let image: BytecodeImage = args.try_into()?;
    pull_bytecode(image).await?;

    Ok(())
}

pub(crate) async fn execute_build(args: &BuildBytecodeArgs) -> anyhow::Result<()> {
    // parse program data from bytecode file
    let (prog_labels, map_labels) =
        build_image_labels(&args.bytecode_file, Some(Endianness::default()))?;
    debug!(
        "Bytecode: {} contains the following. \n
        programs: {prog_labels}\n
        maps: {map_labels}",
        args.bytecode_file.display()
    );
    let container_tool = ContainerRuntime::new()?;
    container_tool.build_image(
        &args.tag,
        &args.bytecode_file,
        &args.container_file,
        prog_labels,
        map_labels,
    )?;

    Ok(())
}

pub(crate) async fn execute_build_args(args: &GenerateArgs) -> anyhow::Result<()> {
    // parse program data from bytecode file
    let (prog_labels, map_labels) =
        build_image_labels(&args.bytecode_file, Some(Endianness::default()))?;
    debug!(
        "Bytecode: {} contains the following. \n
        programs: {prog_labels}\n
        maps: {map_labels}",
        args.bytecode_file.display()
    );

    println!("PROGRAMS={}", prog_labels);
    println!("MAPS={}", map_labels);

    Ok(())
}

enum ContainerRuntime {
    Docker,
    Podman,
}

impl ContainerRuntime {
    // Default to using docker if it's available.
    fn new() -> Result<Self, anyhow::Error> {
        if Command::new("docker")
            .arg("version")
            .output()
            .is_ok_and(|o| o.status.success())
        {
            debug!("using docker for container runtime");
            Ok(ContainerRuntime::Docker)
        } else if Command::new("podman")
            .arg("version")
            .output()
            .is_ok_and(|o| o.status.success())
        {
            debug!("using podman for container runtime");
            Ok(ContainerRuntime::Podman)
        } else {
            Err(anyhow!(
                "No container runtime found. Please install either docker or podman."
            ))
        }
    }

    fn command(&self) -> Command {
        match self {
            ContainerRuntime::Docker => Command::new("docker"),
            ContainerRuntime::Podman => Command::new("podman"),
        }
    }

    fn build_image(
        &self,
        image_tag: &str,
        bc_file: &Path,
        container_file: &Path,
        program_labels: String,
        map_labels: String,
    ) -> anyhow::Result<()> {
        let mut command = self.command();
        command.arg("build");
        command.arg("-t").arg(image_tag);
        command.arg("-f").arg(container_file);

        command
            .arg("--build-arg")
            .arg(format! {"PROGRAMS={}",program_labels});
        command
            .arg("--build-arg")
            .arg(format! {"MAPS={}",map_labels});

        let current_dir = env::current_dir()?;

        let build_arg = format!("BYTECODE_FILE={}", bc_file.display());
        command.arg("--build-arg").arg(build_arg);
        command.arg(current_dir.as_os_str().to_str().unwrap_or("."));

        debug!(
            "Building bytecode images with command: {} {}",
            command.get_program().to_string_lossy(),
            command
                .get_args()
                .map(|arg| arg.to_string_lossy().into_owned())
                .collect::<Vec<String>>()
                .join(" ")
        );

        let output = command.output()?;
        if !output.status.success() {
            return Err(anyhow!(
                "Failed to build image: {}",
                String::from_utf8(output.stderr)?
            ));
        }

        Ok(())
    }
}

fn build_image_labels(
    file: &Path,
    expected_endianness: Option<object::Endianness>,
) -> Result<(String, String), anyhow::Error> {
    let bc_content = fs::read(file).context("cannot find bytecode")?;
    let bc = Object::parse(&bc_content)?;

    if expected_endianness.is_some_and(|e| e != bc.endianness) {
        return Err(anyhow!(
            "Bytcode: {file:?} doesn't match expected endianness {expected_endianness:?}"
        ));
    }

    if bc.programs.is_empty() {
        return Err(anyhow!("No programs found in bytecode: {file:?}"));
    }

    let program_labels: HashMap<String, String> = bc
        .programs
        .into_iter()
        .map(|(k, v)| {
            let prog_type = ProgramType::from(v.section);
            (k, prog_type.to_string())
        })
        .collect();

    let map_labels: HashMap<String, String> = bc
        .maps
        .into_iter()
        .filter_map(|(name, v)| {
            if !(name.contains(".rodata") || name.contains(".bss") || name.contains(".data")) {
                let map_type = MapType::from(v.map_type());
                Some((name, map_type.to_string()))
            } else {
                None
            }
        })
        .collect();
    Ok((
        serde_json::to_string(&program_labels)?,
        serde_json::to_string(&map_labels)?,
    ))
}
