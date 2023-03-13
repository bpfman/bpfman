// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
use std::{collections::HashMap, fs, path::Path};

use log::warn;
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct StaticProgramEntry {
    pub name: String,
    pub location: String,
    pub section_name: String,
    pub global_data: HashMap<String, Vec<u8>>,
    pub program_type: String,
    pub attach: Option<String>,
    pub network_attach: Option<NetworkAttach>,
}

#[derive(Debug, Deserialize)]
pub struct NetworkAttach {
    pub interface: String,
    pub priority: i32,
    pub proceed_on: Vec<String>,
}

#[derive(Debug, Deserialize)]
pub struct StaticPrograms {
    pub programs: Vec<StaticProgramEntry>,
}

pub fn programs_from_directory<P: AsRef<Path>>(
    path: P,
) -> Result<Vec<StaticPrograms>, anyhow::Error> {
    let mut static_programs: Vec<StaticPrograms> = Vec::new();

    if let Ok(entries) = fs::read_dir(path) {
        for file in entries.flatten() {
            let path = &file.path();
            // ignore directories
            if path.is_dir() {
                continue;
            }

            if let Ok(contents) = fs::read_to_string(path) {
                let program = toml::from_str(&contents)?;

                static_programs.push(program);
            } else {
                warn!("Failed to parse program static file {:?}.", path.to_str());
                continue;
            }
        }
    }

    Ok(static_programs)
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_parse_program_from_invalid_path() {
        let result = programs_from_directory("/tmp/file.toml");
        assert!(result.unwrap().is_empty())
    }

    #[test]
    fn test_parse_single_file() {
        let input: &str = r#"
        [[programs]]
        name = "program1"
        location = "file:///opt/bin/myapp/lib/myebpf.o"
        section_name = "firewall"
        program_type ="xdp"
        network_attach = { interface = "eth0", priority = 50, proceed_on = ["pass", "dispatcher_return"] }

        [[programs]]
        name = "program2"
        location = "image://quay.io/bpfd-bytecode/xdp_pass:latest"
        section_name = "firewall"
        program_type ="xdp"
        network_attach = { interface = "eth0", priority = 55, proceed_on = ["pass", "dispatcher_return"] }
        "#;
        let mut programs: StaticPrograms = toml::from_str(input).expect("error parsing toml input");
        match programs.programs.pop() {
            Some(i) => {
                if let Some(m) = i.network_attach {
                    assert_eq!(m.interface, "eth0");
                    assert_eq!(m.priority, 55);
                } else {
                    panic!("incorrect attach type")
                }
            }
            None => panic!("expected programs to be present"),
        }
    }
}
