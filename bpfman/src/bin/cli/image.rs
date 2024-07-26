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

use crate::args::{
    BuildBytecodeArgs, BytecodeFile, GenerateArgs, GoArch, ImageSubCommand, PullBytecodeArgs,
};

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

/// Image builder defines the platforms and build args that must be passed to
/// a container runtime to build an eBPF image. For single host arch images
/// platforms will be None and build args will contain a single string.
pub(crate) struct ImageBuilder {
    /// Information r.e a given platform https://github.com/containerd/containerd/blob/v1.4.3/platforms/platforms.go#L63
    pub(crate) platforms: Option<Vec<String>>,
    /// Container Build arguments which signify where the prebuilt bytecode files exist on disk.
    pub(crate) build_args: Vec<String>,
}

pub(crate) async fn execute_pull(args: &PullBytecodeArgs) -> anyhow::Result<()> {
    let image: BytecodeImage = args.try_into()?;
    pull_bytecode(image).await?;

    Ok(())
}

pub(crate) async fn execute_build(args: &BuildBytecodeArgs) -> anyhow::Result<()> {
    let container_tool = if let Some(runtime) = &args.runtime {
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

    let build_context = if let Some(project_path) = &args.bytecode_file.cilium_ebpf_project {
        parse_bytecode_from_cilium_ebpf_project(project_path)?
    } else {
        debug!("parsing multi-arch bytecode files from user input");
        args.bytecode_file.parse()
    };

    if build_context.build_args.is_empty() {
        return Err(anyhow!("No bytecode files found for building eBPF image"));
    }

    // use first bytecode path to get the program and map labels
    // parse program data from bytecode file
    let first_arg: Vec<&str> = build_context
        .build_args
        .first()
        .unwrap()
        .split('=')
        .collect();

    let bc_file = PathBuf::from(first_arg[1]);

    // Make sure bytecode matches host endian if we're building a host endian image
    // otherwise determine correct endianness based on build argument naming scheme
    let expected_endianess = if build_context.platforms.is_none() {
        Endianness::default()
    } else if first_arg[0].contains("EL") {
        Endianness::Little
    } else {
        Endianness::Big
    };

    let (prog_labels, map_labels) =
        build_bpf_info_image_labels(&bc_file, Some(expected_endianess))?;

    container_tool.build_image(
        &args.tag,
        &args.container_file,
        &build_context,
        prog_labels,
        map_labels,
    )?;

    Ok(())
}

impl BytecodeFile {
    /// parse takes user input and returns a list of platforms and build args,
    /// if a user specifies a single host-arch bytecode file platforms will
    /// have a length of 0 and build args will have a length of 1.
    pub(crate) fn parse(&self) -> ImageBuilder {
        let mut build_args: Vec<String> = Vec::new();

        // Single host-arch case
        if let Some(bc) = &self.bytecode {
            debug!("Found bytecode for host arch");
            build_args.push(format!("BYTECODE_FILE={}", bc.display()));
            return ImageBuilder {
                platforms: None,
                build_args,
            };
        }

        let mut platforms: Vec<String> = Vec::new();

        if let Some(bc) = &self.bc_386_el {
            debug!("Found bytecode file for 386");
            let arch = GoArch::X386;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_amd64_el {
            debug!("Found bytecode file for amd64");
            let arch = GoArch::Amd64;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_arm_el {
            debug!("Found bytecode file for arm");
            let arch = GoArch::Arm;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_arm64_el {
            debug!("Found bytecode file for arm64");
            let arch = GoArch::Arm64;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_loong64_el {
            debug!("Found bytecode file for loong64");
            let arch = GoArch::Loong64;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_mips_eb {
            debug!("Found bytecode file for mips");
            let arch = GoArch::Mips;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_mipsle_el {
            debug!("Found bytecode file for mips64le");
            let arch = GoArch::Mipsle;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_mips64_eb {
            debug!("Found bytecode file for mips64");
            let arch = GoArch::Mips64;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_mips64le_el {
            debug!("Found bytecode file for mips64le");
            let arch = GoArch::Mips64le;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_ppc64_eb {
            debug!("Found bytecode file for ppc64");
            let arch = GoArch::Ppc64;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_ppc64le_el {
            debug!("Found bytecode file for ppc64le");
            let arch = GoArch::Ppc64le;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_riscv64_el {
            debug!("Found bytecode file for riscv64");
            let arch = GoArch::Riscv64;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        if let Some(bc) = &self.bc_s390x_eb {
            debug!("Found bytecode file for s390x");
            let arch = GoArch::S390x;
            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(bc));
        }

        ImageBuilder {
            platforms: Some(platforms),
            build_args,
        }
    }
}

pub(crate) fn parse_bytecode_from_cilium_ebpf_project(
    dir: &Path,
) -> Result<ImageBuilder, anyhow::Error> {
    debug!("parsing multi-arch bytecode files from cilium eBPF project at {dir:?}");
    let mut platforms: Vec<String> = Vec::new();
    let mut build_args: Vec<String> = Vec::new();

    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.is_file() && path.extension() == Some(std::ffi::OsStr::new("o")) {
            let file_name = entry.file_name();
            debug!("inspecting {file_name:?}");
            let arch = if let Some(file_name) = file_name.to_str() {
                GoArch::from_cilium_ebpf_file_str(file_name).map_err(|e| anyhow!(e))
            } else {
                return Err(anyhow!(
                    "Could not parse file name {file_name:?} in cilium/ebpf project"
                ));
            }?;
            debug!("Found bytecode file for {arch:?} in cilium/ebpf project.");

            platforms.push(arch.get_platform());
            build_args.push(arch.get_build_arg(&entry.path()));
        }
    }

    Ok(ImageBuilder {
        platforms: Some(platforms),
        build_args,
    })
}

pub(crate) async fn execute_build_args(args: &GenerateArgs) -> anyhow::Result<()> {
    let build_context = if let Some(project_path) = &args.bytecode.cilium_ebpf_project {
        parse_bytecode_from_cilium_ebpf_project(project_path)?
    } else {
        debug!("parsing bytecode files from user input");
        args.bytecode.parse()
    };

    if build_context.build_args.is_empty() {
        return Err(anyhow!("No bytecode files found for building eBPF image"));
    }

    // use one of the bytecode paths to get the program and map labels
    // parse program data from bytecode file

    // TODO: Temporary fix for bpfman issue #1200
    // Find an entry that doesn't contain "S390 because parsing an S390 file
    // isn't currently supported by Aya"
    let valid_bc_path = build_context
        .build_args
        .iter()
        .find(|arg| !arg.contains("S390"));

    let valid_bc_path = match valid_bc_path {
        Some(arg) => arg.to_string(),
        None => {
            return Err(anyhow!(
                "An S390 bytecode file cannot be used to generate build args"
            ))
        }
    };

    debug!("Using: {:?} to generate build args", valid_bc_path);

    let bc_path: Vec<&str> = valid_bc_path.split('=').collect();
    let bc_file = PathBuf::from(bc_path[1]);

    // Make sure bytecode matches host endian if we're building a host endian image
    // otherwise determine correct endianness based on build argument naming scheme
    let expected_endianess = if build_context.platforms.is_none() {
        Endianness::default()
    } else if bc_path[0].contains("EL") {
        Endianness::Little
    } else {
        Endianness::Big
    };

    let (prog_labels, map_labels) =
        build_bpf_info_image_labels(&bc_file, Some(expected_endianess))?;

    build_context.build_args.into_iter().for_each(|a| {
        println!("{a}");
    });
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
                command.args(["build", "--push"]);
                command
            }
            ContainerRuntime::Podman => {
                let mut command = Command::new("podman");
                command.arg("build");
                command
            }
        }
    }

    fn build_and_push_manifest_base(&self, image_tag: &str) -> Command {
        match self {
            ContainerRuntime::Docker => {
                let mut command = Command::new("docker");
                command.args(["buildx", "build", "--push", "--tag", image_tag]);
                command
            }
            ContainerRuntime::Podman => {
                let mut command = Command::new("podman");
                command.args(["build", "--manifest", image_tag]);
                command
            }
        }
    }

    fn build_image(
        &self,
        tag: &str,
        container_file: &Path,
        build_context: &ImageBuilder,
        program_labels: String,
        map_labels: String,
    ) -> anyhow::Result<()> {
        let mut command: Command;

        if let Some(platforms) = &build_context.platforms {
            command = self.build_and_push_manifest_base(tag);

            build_context.build_args.iter().for_each(|a| {
                command.arg("--build-arg").arg(a);
            });

            command.arg("--progress").arg("plain");

            command.arg("--platform").arg(platforms.join(","));
        } else {
            command = self.build_and_push_base();
            command.arg("-t").arg(tag);
            command
                .arg("--build-arg")
                .arg(build_context.build_args.first().unwrap());
        }

        command.arg("-f").arg(container_file);

        command
            .arg("--build-arg")
            .arg(format! {"PROGRAMS={}",program_labels});
        command
            .arg("--build-arg")
            .arg(format! {"MAPS={}",map_labels});

        let current_dir = env::current_dir()?;
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

            if build_context.platforms.is_none() {
                command.arg("push").arg(tag);
            } else {
                command.args(["manifest", "push", tag]);
            }

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

fn build_bpf_info_image_labels(
    file: &Path,
    expected_endianness: Option<object::Endianness>,
) -> Result<(String, String), anyhow::Error> {
    let bc_content = fs::read(file).context("cannot find bytecode")?;
    let bc_result = Object::parse(&bc_content);
    let bc = match bc_result {
        Ok(bc) => bc,
        Err(e) => {
            return Err(anyhow!(
                "Failed to parse bytecode: {file:?} with error: {e}"
            ));
        }
    };

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
