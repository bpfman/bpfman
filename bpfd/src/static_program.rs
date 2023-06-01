// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::{
    collections::HashMap,
    path::{Path, PathBuf},
};

use anyhow::bail;
use bpfd_api::{util::directories::CFGDIR_STATIC_PROGRAMS, ProgramType};
use log::{info, warn};
use serde::Deserialize;
use tokio::fs;

use crate::{
    command::{
        Location::{File, Image},
        Metadata, Program, ProgramData, TcAttachInfo, TcProgram, TcProgramInfo,
        TracepointAttachInfo, TracepointProgram, TracepointProgramInfo, XdpAttachInfo, XdpProgram,
        XdpProgramInfo,
    },
    oci_utils::BytecodeImage,
    utils::{get_ifindex, read_to_string},
};

#[derive(Debug, Deserialize, Clone)]
pub struct StaticProgramEntry {
    bytecode_image: Option<BytecodeImage>,
    file_path: Option<String>,
    section_name: String,
    global_data: HashMap<String, Vec<u8>>,
    program_type: ProgramType,
    xdp_attach: Option<XdpAttachInfo>,
    tc_attach: Option<TcAttachInfo>,
    tracepoint_attach: Option<TracepointAttachInfo>,
}

impl StaticProgramEntry {
    pub(crate) fn get_bytecode_image(self) -> Option<BytecodeImage> {
        self.bytecode_image
    }
}

#[derive(Debug, Deserialize)]
pub struct NetworkAttach {
    pub interface: String,
    pub priority: i32,
    pub proceed_on: Vec<String>,
}

#[derive(Debug, Deserialize, Clone)]
struct StaticProgramManager {
    #[serde(skip)]
    path: PathBuf,
    programs: Vec<StaticProgramEntry>,
}

impl StaticProgramManager {
    async fn programs_from_directory(mut self) -> Result<(), anyhow::Error> {
        if let Ok(mut entries) = fs::read_dir(self.path).await {
            while let Some(file) = entries.next_entry().await? {
                let path = &file.path();
                // ignore directories
                if path.is_dir() {
                    continue;
                }

                if let Ok(contents) = read_to_string(path).await {
                    let program = toml::from_str(&contents)?;

                    self.programs.push(program);
                } else {
                    warn!("Failed to parse program static file {:?}.", path.to_str());
                    continue;
                }
            }
        }
        Ok(())
    }
}

pub(crate) async fn get_static_programs<P: AsRef<Path>>(
    path: P,
) -> Result<Vec<Program>, anyhow::Error> {
    let static_program_manager = StaticProgramManager {
        path: path.as_ref().to_path_buf(),
        programs: Vec::new(),
    };

    static_program_manager
        .clone()
        .programs_from_directory()
        .await?;

    let mut programs: Vec<Program> = Vec::new();

    // Load any static programs first
    if !static_program_manager.programs.is_empty() {
        info!("Loading static programs from {CFGDIR_STATIC_PROGRAMS}");
        for program in static_program_manager.programs {
            let location = match program.file_path {
                Some(p) => File(p),
                None => Image(
                    program
                        .clone()
                        .get_bytecode_image()
                        .expect("static program did not provide bytecode"),
                ),
            };
            let prog = match program.program_type {
                ProgramType::Xdp => {
                    if let Some(m) = program.xdp_attach {
                        let if_index = get_ifindex(&m.iface)?;
                        let metadata = Metadata::new(m.priority, program.section_name.clone());
                        Program::Xdp(XdpProgram::new(
                            ProgramData::new(
                                location,
                                program.section_name.clone(),
                                program.global_data,
                                None,
                                String::from("bpfd"),
                            ),
                            XdpProgramInfo {
                                if_index,
                                current_position: None,
                                metadata,
                                proceed_on: m.proceed_on,
                                if_name: m.iface,
                            },
                        ))
                    } else {
                        bail!("invalid attach type for xdp program")
                    }
                }
                ProgramType::Tc => {
                    if let Some(m) = program.tc_attach {
                        let if_index = get_ifindex(&m.iface)?;
                        let metadata = Metadata::new(m.priority, program.section_name.clone());
                        Program::Tc(TcProgram {
                            data: ProgramData::new(
                                location,
                                program.section_name.clone(),
                                program.global_data,
                                None,
                                String::from("bpfd"),
                            ),
                            info: TcProgramInfo {
                                if_index,
                                current_position: None,
                                metadata,
                                proceed_on: m.proceed_on,
                                if_name: m.iface,
                                direction: m.direction,
                            },
                        })
                    } else {
                        bail!("invalid attach type for tc program")
                    }
                }
                ProgramType::Tracepoint => {
                    if let Some(m) = program.tracepoint_attach {
                        Program::Tracepoint(TracepointProgram::new(
                            ProgramData::new(
                                location,
                                program.section_name.clone(),
                                program.global_data,
                                None,
                                String::from("bpfd"),
                            ),
                            TracepointProgramInfo {
                                tracepoint: m.tracepoint,
                            },
                        ))
                    } else {
                        bail!("invalid attach type for tc program")
                    }
                }
                m => bail!("program type not yet supported: {:?}", m),
            };

            programs.push(prog)
        }
    };

    Ok(programs)
}

#[cfg(test)]
mod test {
    use super::*;

    #[tokio::test]
    async fn test_parse_program_from_invalid_path() {
        let static_program_manager = StaticProgramManager {
            path: "/tmp/file.toml".into(),
            programs: Vec::new(),
        };

        static_program_manager
            .clone()
            .programs_from_directory()
            .await
            .unwrap();
        assert!(static_program_manager.programs.is_empty())
    }

    #[test]
    fn test_parse_single_file() {
        let input: &str = r#"
        [[programs]]
        name = "program1"
        file_path = "/opt/bin/myapp/lib/myebpf.o"
        section_name = "firewall"
        global_data = { }
        program_type ="Xdp"
        xdp_attach = { iface = "eth0", priority = 50, proceed_on = [], position=0 }

        [[programs]]
        name = "program2"
        bytecode_image = { image_url = "quay.io/bpfd-bytecode/xdp_pass:latest", image_pull_policy="Always" }
        section_name = "pass"
        global_data = { }
        program_type ="Xdp"
        xdp_attach = { iface = "eth0", priority = 55, proceed_on = [], position=0 }

        [[programs]]
        name = "program3"
        bytecode_image = { image_url = "quay.io/bpfd-bytecode/xdp_pass:latest", image_pull_policy="Always" }
        section_name = "counter"
        global_data = { }
        program_type ="Tc"
        tc_attach = { iface = "eth0", priority = 55, proceed_on = [], position=0, direction="Ingress" }
        
        [[programs]]
        name = "program"
        bytecode_image = { image_url = "quay.io/bpfd-bytecode/tracepoint:latest", image_pull_policy="Always" }
        section_name = "tracepoint"
        global_data = { }
        program_type ="Tracepoint"
        tracepoint_attach = { tracepoint = "syscalls/sys_enter_openat" }
        "#;

        let mut programs: StaticProgramManager =
            toml::from_str(input).expect("error parsing toml input");
        match programs.programs.pop() {
            Some(i) => {
                if let Some(m) = i.xdp_attach {
                    assert_eq!(m.iface, "eth0");
                    assert_eq!(m.priority, 55);
                } else if let Some(m) = i.tracepoint_attach {
                    assert_eq!(m.tracepoint, "syscalls/sys_enter_openat")
                } else {
                    panic!("incorrect attach type")
                }
            }
            None => panic!("expected programs to be present"),
        }
    }
}
