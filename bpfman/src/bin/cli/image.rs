// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    env, fs,
    io::{BufRead, BufReader},
    path::{Path, PathBuf},
    process::{Command, Stdio},
};

use anyhow::{anyhow, Context, Result};
use aya_obj::Object;
use base64::{engine::general_purpose, Engine};
use bpfman::{
    pull_bytecode,
    types::{BytecodeImage, ImagePullPolicy, MapType, ProgramType},
};
use log::{debug, warn};
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
    let mut container_tool = if let Some(runtime) = &args.runtime {
        match runtime.as_str() {
            "docker" => ContainerRuntime::Docker,
            "podman" => ContainerRuntime::Podman,
            p => {
                warn!("Provided runtime {p} is not supported defaulting to whatever is avaliable");
                ContainerRuntime::new()?
            }
        }
    } else {
        ContainerRuntime::new()?
    };

    if let Some(bytecode_file) = &args.bytecode_file.bytecode_file {
        // parse program data from bytecode file
        let (prog_labels, map_labels) =
            build_image_labels(bytecode_file, Some(Endianness::default()))?;
        debug!(
            "Bytecode: {} contains the following. \n
            programs: {prog_labels}\n
            maps: {map_labels}",
            bytecode_file.display()
        );
        container_tool.build_image(
            &args.tag,
            bytecode_file,
            &args.container_file,
            prog_labels,
            map_labels,
        )?;
    } else if let Some(multi_arch_files) = &args.bytecode_file.bytecode_file_arch {
        // Information r.e a given platform https://github.com/containerd/containerd/blob/v1.4.3/platforms/platforms.go#L63
        let mut platforms: Vec<String> = Vec::new();
        let mut build_args: Vec<String> = Vec::new();

        if let Some(bc) = &multi_arch_files.bc_386_el {
            debug!("Found bytecode file build for 386");
            platforms.push("linux/386".to_string());
            build_args.push(format!("BC_386_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_amd64_el {
            debug!("Found bytecode file build for amd64");
            platforms.push("linux/amd64".to_string());
            build_args.push(format!("BC_AMD64_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_arm_el {
            debug!("Found bytecode file build for arm");
            platforms.push("linux/arm".to_string());
            build_args.push(format!("BC_ARM_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_arm64_el {
            debug!("Found bytecode file build for arm64");
            platforms.push("linux/arm64".to_string());
            build_args.push(format!("BC_ARM64_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_loong64_el {
            debug!("Found bytecode file build for loong64");
            platforms.push("linux/loong64".to_string());
            build_args.push(format!("BC_LOONG64_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_mips_eb {
            debug!("Found bytecode file build for mips");
            platforms.push("linux/mips".to_string());
            build_args.push(format!("BC_MIPS_EB={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_mipsle_el {
            debug!("Found bytecode file build for mips64le");
            platforms.push("linux/mipsle".to_string());
            build_args.push(format!("BC_MIPSLE_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_mips64_eb {
            debug!("Found bytecode file build for mips64");
            platforms.push("linux/mips64".to_string());
            build_args.push(format!("BC_MIPS64_EB={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_mips64le_el {
            debug!("Found bytecode file build for mips64le");
            platforms.push("linux/mips64le".to_string());
            build_args.push(format!("BC_MIPS64LE_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_ppc64_eb {
            debug!("Found bytecode file build for ppc64");
            platforms.push("linux/ppc64".to_string());
            build_args.push(format!("BC_PPC64_EB={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_ppc64le_el {
            debug!("Found bytecode file build for ppc64le");
            platforms.push("linux/ppc64le".to_string());
            build_args.push(format!("BC_PPC64LE_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_riscv64_el {
            debug!("Found bytecode file build for riscv64");
            platforms.push("linux/riscv64".to_string());
            build_args.push(format!("BC_RISCV64_EL={}", bc.display()));
        }

        if let Some(bc) = &multi_arch_files.bc_s390x_eb {
            debug!("Found bytecode file build for s390x");
            platforms.push("linux/s390x".to_string());
            build_args.push(format!("BC_S390X_EB={}", bc.display()));
        }

        if platforms.is_empty() || build_args.is_empty() {
            return Err(anyhow!(
                "No bytecode files found for building multi-arch image"
            ));
        }

        // use first bytecode path to get the program and map labels
        // parse program data from bytecode file
        let first_arg: Vec<&str> = build_args[0].split('=').collect();
        let bc_file = PathBuf::from(first_arg[1]);
        let (prog_labels, map_labels) = if first_arg[0].contains("EL") {
            build_image_labels(&bc_file, Some(Endianness::Little))?
        } else {
            build_image_labels(&bc_file, Some(Endianness::Big))?
        };

        container_tool.build_multi_arch_image(
            &args.tag,
            &args.container_file,
            build_args,
            platforms,
            prog_labels,
            map_labels,
        )?;
    }
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

pub(crate) enum ContainerRuntime {
    Docker,
    Podman,
}

impl ContainerRuntime {
    // Default to using docker if it's available.
    fn new() -> Result<Self, anyhow::Error> {
        if Command::new("docker")
            .arg("buildx")
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

    fn get_command(&self) -> Command {
        match self {
            ContainerRuntime::Docker => Command::new("docker"),
            ContainerRuntime::Podman => Command::new("podman"),
        }
    }

    fn build_and_push_base(&self) -> Command {
        match self {
            ContainerRuntime::Docker => {
                let mut command = Command::new("docker");
                command.args(["buildx", "build", "--push"]);
                command
            }
            ContainerRuntime::Podman => {
                let mut command = Command::new("podman");
                command.arg("build");
                command
            }
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
        let mut command = self.build_and_push_base();
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

        self.execute_and_tail_logs(&mut command)?;

        // if command is podman add extra push command
        if let ContainerRuntime::Podman = self {
            let mut command = self.get_command();
            command.arg("push").arg(image_tag);

            self.execute_and_tail_logs(&mut command)?;
        }

        Ok(())
    }

    fn build_multi_arch_image(
        &mut self,
        image_tag: &str,
        container_file: &Path,
        args: Vec<String>,
        platforms: Vec<String>,
        program_labels: String,
        map_labels: String,
    ) -> anyhow::Result<()> {
        let mut command = self.build_and_push_base();

        command.arg("-t").arg(image_tag);
        command.arg("-f").arg(container_file);

        command
            .arg("--build-arg")
            .arg(format! {"PROGRAMS={}",program_labels});
        command
            .arg("--build-arg")
            .arg(format! {"MAPS={}",map_labels});

        args.into_iter().for_each(|a| {
            command.arg("--build-arg").arg(a);
        });

        command.arg("--progress").arg("plain");

        command.arg("--platform").arg(platforms.join(","));

        let current_dir = env::current_dir()?;
        command.arg(current_dir.as_os_str().to_str().unwrap_or("."));

        debug!(
            "Building multi-arch bytecode image with command: {} {}",
            command.get_program().to_string_lossy(),
            command
                .get_args()
                .map(|arg| arg.to_string_lossy().into_owned())
                .collect::<Vec<String>>()
                .join(" ")
        );

        self.execute_and_tail_logs(&mut command)?;

        // if command is podman add extra push command
        if let ContainerRuntime::Podman = self {
            let mut command = self.get_command();
            command.arg("push").arg(image_tag);

            self.execute_and_tail_logs(&mut command)?;
        }

        Ok(())
    }

    fn execute_and_tail_logs(&self, command: &mut Command) -> anyhow::Result<()> {
        let stdout = command
            .stdout(Stdio::piped())
            .spawn()?
            .stdout
            .context("Could not capture standard output.")?;
        let reader = BufReader::new(stdout);

        reader
            .lines()
            .map_while(Result::ok)
            .for_each(|line| println!("{}", line));

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
