// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

//! Commands between the RPC thread and the BPF thread
use std::{collections::HashMap, fmt, fs, io::BufReader, path::PathBuf};

use bpfd_api::{
    util::directories::{RTDIR_FS, RTDIR_FS_MAPS, RTDIR_PROGRAMS},
    ParseError, ProgramType, TcProceedOn, XdpProceedOn,
};
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;

use crate::{
    errors::BpfdError,
    multiprog::{DispatcherId, DispatcherInfo},
    oci_utils::BytecodeImage,
};

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
#[allow(clippy::large_enum_variant)]
pub(crate) enum Command {
    /// Load an XDP program
    LoadXDP {
        location: Location,
        section_name: String,
        id: Option<String>,
        global_data: HashMap<String, Vec<u8>>,
        iface: String,
        priority: i32,
        proceed_on: XdpProceedOn,
        username: String,
        responder: Responder<Result<String, BpfdError>>,
    },
    /// Load a TC Program
    LoadTC {
        location: Location,
        section_name: String,
        id: Option<String>,
        global_data: HashMap<String, Vec<u8>>,
        iface: String,
        priority: i32,
        direction: Direction,
        proceed_on: TcProceedOn,
        username: String,
        responder: Responder<Result<String, BpfdError>>,
    },
    // Load a Tracepoint Program
    LoadTracepoint {
        location: Location,
        id: Option<String>,
        section_name: String,
        global_data: HashMap<String, Vec<u8>>,
        tracepoint: String,
        username: String,
        responder: Responder<Result<String, BpfdError>>,
    },
    Unload {
        id: String,
        username: String,
        responder: Responder<Result<(), BpfdError>>,
    },
    List {
        responder: Responder<Result<Vec<ProgramInfo>, BpfdError>>,
    },
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) enum Location {
    Image(BytecodeImage),
    File(String),
}

#[derive(Debug, Serialize, Hash, Deserialize, Eq, PartialEq, Copy, Clone)]
pub(crate) enum Direction {
    Ingress = 1,
    Egress = 2,
}

impl TryFrom<String> for Direction {
    type Error = ParseError;

    fn try_from(v: String) -> Result<Self, Self::Error> {
        match v.as_str() {
            "ingress" => Ok(Self::Ingress),
            "egress" => Ok(Self::Egress),
            m => Err(ParseError::InvalidDirection {
                direction: m.to_string(),
            }),
        }
    }
}

impl std::fmt::Display for Direction {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Direction::Ingress => f.write_str("in"),
            Direction::Egress => f.write_str("eg"),
        }
    }
}

#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: String,
    pub(crate) name: String,
    pub(crate) location: Location,
    pub(crate) program_type: i32,
    pub(crate) attach_info: AttachInfo,
}

#[derive(Debug, Clone)]
pub(crate) enum AttachInfo {
    Xdp(XdpAttachInfo),
    Tc(TcAttachInfo),
    Tracepoint(TracepointAttachInfo),
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct XdpAttachInfo {
    pub(crate) priority: i32,
    pub(crate) iface: String,
    pub(crate) position: i32,
    pub(crate) proceed_on: XdpProceedOn,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct TcAttachInfo {
    pub(crate) priority: i32,
    pub(crate) iface: String,
    pub(crate) position: i32,
    pub(crate) proceed_on: TcProceedOn,
    pub(crate) direction: Direction,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct TracepointAttachInfo {
    pub(crate) tracepoint: String,
}

#[derive(Debug, Eq, Ord, PartialEq, PartialOrd, Serialize, Deserialize, Clone)]
pub(crate) struct Metadata {
    pub(crate) priority: i32,
    pub(crate) name: String,
    pub(crate) attached: bool,
}

impl Metadata {
    pub(crate) fn new(priority: i32, name: String) -> Self {
        Metadata {
            priority,
            name,
            attached: false,
        }
    }
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) enum Program {
    Xdp(XdpProgram),
    Tracepoint(TracepointProgram),
    Tc(TcProgram),
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct XdpProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: XdpProgramInfo,
}

impl XdpProgram {
    pub(crate) fn new(data: ProgramData, info: XdpProgramInfo) -> Self {
        Self { data, info }
    }
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TcProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: TcProgramInfo,
    pub(crate) direction: Direction,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TracepointProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: TracepointProgramInfo,
}

impl TracepointProgram {
    pub(crate) fn new(data: ProgramData, info: TracepointProgramInfo) -> Self {
        Self { data, info }
    }
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct ProgramData {
    pub(crate) location: Location,
    pub(crate) section_name: String,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) path: String,
    pub(crate) owner: String,
}

impl ProgramData {
    pub(crate) async fn new(
        location: Location,
        mut section_name: String,
        global_data: HashMap<String, Vec<u8>>,
        owner: String,
    ) -> Result<Self, BpfdError> {
        match location.clone() {
            Location::File(l) => Ok(ProgramData {
                location,
                path: l,
                section_name,
                owner,
                global_data,
            }),
            Location::Image(l) => {
                let program_overrides = l
                    .get_image(None)
                    .await
                    .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?;

                // If section name isn't provided and we're loading from a container
                // image use the section name provided in the image metadata, otherwise
                // always use the provided section name.
                if section_name.is_empty() {
                    section_name = program_overrides.image_meta.section_name
                } else if program_overrides.image_meta.section_name != section_name {
                    return Err(BpfdError::BytecodeMetaDataMismatch {
                        image_sec_name: program_overrides.image_meta.section_name,
                        provided_sec_name: section_name,
                    });
                }

                Ok(ProgramData {
                    location,
                    path: program_overrides.path,
                    section_name,
                    global_data,
                    owner,
                })
            }
        }
    }
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct XdpProgramInfo {
    pub(crate) if_name: String,
    pub(crate) if_index: u32,
    #[serde(skip)]
    pub(crate) current_position: Option<usize>,
    pub(crate) metadata: Metadata,
    pub(crate) proceed_on: XdpProceedOn,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TcProgramInfo {
    pub(crate) if_name: String,
    pub(crate) if_index: u32,
    #[serde(skip)]
    pub(crate) current_position: Option<usize>,
    pub(crate) metadata: Metadata,
    pub(crate) proceed_on: TcProceedOn,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TracepointProgramInfo {
    pub(crate) tracepoint: String,
}

impl Program {
    pub(crate) fn kind(&self) -> ProgramType {
        match self {
            Program::Xdp(_) => ProgramType::Xdp,
            Program::Tc(_) => ProgramType::Tc,
            Program::Tracepoint(_) => ProgramType::Tracepoint,
        }
    }

    pub(crate) fn dispatcher_id(&self) -> Option<DispatcherId> {
        match self {
            Program::Xdp(p) => Some(DispatcherId::Xdp(DispatcherInfo(p.info.if_index, None))),
            Program::Tc(p) => Some(DispatcherId::Tc(DispatcherInfo(
                p.info.if_index,
                Some(p.direction),
            ))),
            _ => None,
        }
    }

    pub(crate) fn data(&self) -> &ProgramData {
        match self {
            Program::Xdp(p) => &p.data,
            Program::Tracepoint(p) => &p.data,
            Program::Tc(p) => &p.data,
        }
    }

    pub(crate) fn metadata(&self) -> Option<&Metadata> {
        match self {
            Program::Xdp(p) => Some(&p.info.metadata),
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(&p.info.metadata),
        }
    }

    pub(crate) fn owner(&self) -> &String {
        match self {
            Program::Xdp(p) => &p.data.owner,
            Program::Tracepoint(p) => &p.data.owner,
            Program::Tc(p) => &p.data.owner,
        }
    }

    pub(crate) fn set_attached(&mut self) {
        match self {
            Program::Xdp(p) => p.info.metadata.attached = true,
            Program::Tc(p) => p.info.metadata.attached = true,
            Program::Tracepoint(_) => (),
        }
    }

    pub(crate) fn set_position(&mut self, pos: Option<usize>) {
        match self {
            Program::Xdp(p) => p.info.current_position = pos,
            Program::Tc(p) => p.info.current_position = pos,
            Program::Tracepoint(_) => (),
        }
    }

    pub(crate) fn save(&self, uuid: String) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{uuid}");
        serde_json::to_writer(&fs::File::create(path)?, &self)?;
        Ok(())
    }

    pub(crate) fn delete(&self, uuid: String) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{uuid}");
        fs::remove_file(path)?;

        let path = format!("{RTDIR_FS}/prog_{uuid}");
        if PathBuf::from(&path).exists() {
            fs::remove_file(path)?;
        }
        let path = format!("{RTDIR_FS}/prog_{uuid}_link");
        if PathBuf::from(&path).exists() {
            fs::remove_file(path)?;
        }
        let path = format!("{RTDIR_FS_MAPS}/{uuid}");
        fs::remove_dir_all(path)?;
        Ok(())
    }

    pub(crate) fn load(uuid: String) -> Result<Self, anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{uuid}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        Ok(prog)
    }

    pub(crate) fn if_index(&self) -> Option<u32> {
        match self {
            Program::Xdp(p) => Some(p.info.if_index),
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(p.info.if_index),
        }
    }

    pub(crate) fn if_name(&self) -> Option<String> {
        match self {
            Program::Xdp(p) => Some(p.info.if_name.clone()),
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(p.info.if_name.clone()),
        }
    }

    pub(crate) fn direction(&self) -> Option<Direction> {
        match self {
            Program::Xdp(_) => None,
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(p.direction),
        }
    }
}
