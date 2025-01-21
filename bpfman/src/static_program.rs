// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

// TODO(astoycos) see issue #881
// use std::{
//     collections::HashMap,
//     path::{Path, PathBuf},
// };

// use anyhow::bail;
// use bpfman_api::{
//     util::directories::CFGDIR_STATIC_PROGRAMS, ProgramType, TcProceedOn, XdpProceedOn,
// };
// use log::{info, warn};
// use serde::Deserialize;
// use tokio::fs;

// use crate::{
//     command::{
//         Direction,
//         Location::{File, Image},
//         Program, ProgramData, TcProgram, TracepointProgram, XdpProgram,
//     },
//     oci_utils::image_manager::BytecodeImage,
//     utils::read_to_string,
// };

// #[derive(Debug, Clone, Deserialize)]
// pub(crate) struct XdpAttachInfo {
//     pub(crate) priority: i32,
//     pub(crate) iface: String,
//     pub(crate) proceed_on: XdpProceedOn,
// }

// #[derive(Debug, Clone, Deserialize)]
// pub(crate) struct TcAttachInfo {
//     pub(crate) priority: i32,
//     pub(crate) iface: String,
//     pub(crate) proceed_on: TcProceedOn,
//     pub(crate) direction: Direction,
// }

// #[derive(Debug, Clone, Deserialize)]
// pub(crate) struct TracepointAttachInfo {
//     pub(crate) tracepoint: String,
// }

// // TODO not yet implemented
// // #[derive(Debug, Clone, Deserialize)]
// // pub(crate) struct KprobeAttachInfo {
// //     pub(crate) fn_name: String,
// //     pub(crate) offset: u64,
// //     pub(crate) retprobe: bool,
// //     pub(crate) container_pid: Option<i32>,
// // }

// // #[derive(Debug, Clone, Deserialize)]
// // pub(crate) struct UprobeAttachInfo {
// //     pub(crate) fn_name: Option<String>,
// //     pub(crate) offset: u64,
// //     pub(crate) target: String,
// //     pub(crate) retprobe: bool,
// //     pub(crate) pid: Option<i32>,
// //     pub(crate) container_pid: Option<i32>,
// // }

// #[derive(Debug, Deserialize, Clone)]
// pub struct StaticProgramEntry {
//     bytecode_image: Option<BytecodeImage>,
//     file_path: Option<String>,
//     name: String,
//     global_data: HashMap<String, Vec<u8>>,
//     program_type: ProgramType,
//     xdp_attach: Option<XdpAttachInfo>,
//     tc_attach: Option<TcAttachInfo>,
//     tracepoint_attach: Option<TracepointAttachInfo>,
// }

// impl StaticProgramEntry {
//     pub(crate) fn get_bytecode_image(self) -> Option<BytecodeImage> {
//         self.bytecode_image
//     }
// }

// #[derive(Debug, Deserialize)]
// pub struct NetworkAttach {
//     pub interface: String,
//     pub priority: i32,
//     pub proceed_on: Vec<String>,
// }

// #[derive(Debug, Deserialize, Clone)]
// struct StaticProgramManager {
//     #[serde(skip)]
//     path: PathBuf,
//     programs: Vec<StaticProgramEntry>,
// }

// impl StaticProgramManager {
//     fn programs_from_directory(mut self) -> Result<(), anyhow::Error> {
//         if let Ok(mut entries) = fs::read_dir(self.path) {
//             while let Some(file) = entries.next_entry()? {
//                 let path = &file.path();
//                 // ignore directories
//                 if path.is_dir() {
//                     continue;
//                 }

//                 if let Ok(contents) = read_to_string(path) {
//                     let program = toml::from_str(&contents)?;

//                     self.programs.push(program);
//                 } else {
//                     warn!("Failed to parse program static file {:?}.", path.to_str());
//                     continue;
//                 }
//             }
//         }
//         Ok(())
//     }
// }

// pub(crate) fn get_static_programs<P: AsRef<Path>>(
//     path: P,
// ) -> Result<Vec<Program>, anyhow::Error> {
//     let static_program_manager = StaticProgramManager {
//         path: path.as_ref().to_path_buf(),
//         programs: Vec::new(),
//     };

//     static_program_manager
//         .clone()
//         .programs_from_directory()
//         ?;

//     let mut programs: Vec<Program> = Vec::new();

//     // Load any static programs first
//     if !static_program_manager.programs.is_empty() {
//         info!("Loading static programs from {CFGDIR_STATIC_PROGRAMS}");
//         for program in static_program_manager.programs {
//             let location = match program.file_path {
//                 Some(p) => File(p),
//                 None => Image(
//                     program
//                         .clone()
//                         .get_bytecode_image()
//                         .expect("static program did not provide bytecode"),
//                 ),
//             };

//             let data = ProgramData::new(
//                 location,
//                 program.name,
//                 HashMap::new(),
//                 program.global_data,
//                 None,
//             );
//             let prog = match program.program_type {
//                 ProgramType::Xdp => {
//                     if let Some(m) = program.xdp_attach {
//                         Program::Xdp(XdpProgram::new(data, m.priority, m.iface, m.proceed_on))
//                     } else {
//                         bail!("invalid info for xdp program")
//                     }
//                 }
//                 ProgramType::Tc => {
//                     if let Some(m) = program.tc_attach {
//                         Program::Tc(TcProgram::new(
//                             data,
//                             m.priority,
//                             m.iface,
//                             m.proceed_on,
//                             m.direction,
//                         ))
//                     } else {
//                         bail!("invalid attach type for tc program")
//                     }
//                 }
//                 ProgramType::Tracepoint => {
//                     if let Some(m) = program.tracepoint_attach {
//                         Program::Tracepoint(TracepointProgram::new(data, m.tracepoint))
//                     } else {
//                         bail!("invalid attach type for tc program")
//                     }
//                 }
//                 m => bail!("program type not yet supported to load statically: {:?}", m),
//             };

//             programs.push(prog)
//         }
//     };

//     Ok(programs)
// }

// #[cfg(test)]
// mod test {
//     use super::*;

//     #[tokio::test]
//     fn test_parse_program_from_invalid_path() {
//         let static_program_manager = StaticProgramManager {
//             path: "/tmp/file.toml".into(),
//             programs: Vec::new(),
//         };

//         static_program_manager
//             .clone()
//             .programs_from_directory()
//
//             .unwrap();
//         assert!(static_program_manager.programs.is_empty())
//     }

//     #[test]
//     fn test_parse_single_file() {
//         let input: &str = r#"
//         [[programs]]
//         name = "firewall"
//         file_path = "/opt/bin/myapp/lib/myebpf.o"
//         global_data = { }
//         program_type ="Xdp"
//         xdp_attach = { iface = "eth0", priority = 50, proceed_on = [], position=0 }

//         [[programs]]
//         name = "pass"
//         bytecode_image = { image_url = "quay.io/bpfman-bytecode/xdp_pass:latest", image_pull_policy="Always" }
//         global_data = { }
//         program_type ="Xdp"
//         xdp_attach = { iface = "eth0", priority = 55, proceed_on = [], position=0 }

//         [[programs]]
//         name = "counter"
//         bytecode_image = { image_url = "quay.io/bpfman-bytecode/xdp_pass:latest", image_pull_policy="Always" }
//         global_data = { }
//         program_type ="Tc"
//         tc_attach = { iface = "eth0", priority = 55, proceed_on = [], position=0, direction="Ingress" }

//         [[programs]]
//         name = "tracepoint"
//         bytecode_image = { image_url = "quay.io/bpfman-bytecode/tracepoint:latest", image_pull_policy="Always" }
//         global_data = { }
//         program_type ="Tracepoint"
//         tracepoint_attach = { tracepoint = "syscalls/sys_enter_openat" }
//         "#;

//         let mut programs: StaticProgramManager =
//             toml::from_str(input).expect("error parsing toml input");
//         match programs.programs.pop() {
//             Some(i) => {
//                 if let Some(m) = i.xdp_attach {
//                     assert_eq!(m.iface, "eth0");
//                     assert_eq!(m.priority, 55);
//                 } else if let Some(m) = i.tracepoint_attach {
//                     assert_eq!(m.tracepoint, "syscalls/sys_enter_openat")
//                 } else {
//                     panic!("incorrect attach type")
//                 }
//             }
//             None => panic!("expected programs to be present"),
//         }
//     }
// }
