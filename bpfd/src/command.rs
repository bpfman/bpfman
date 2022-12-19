// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

//! Commands between the RPC thread and the BPF thread
use std::{fmt, fs, io::BufReader, path::PathBuf, str::FromStr};

use bpfd_api::{
    util::directories::{RTDIR_FS, RTDIR_FS_MAPS, RTDIR_PROGRAMS},
    ParseError,
};
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;
use url::Url;
use uuid::Uuid;

use crate::{
    errors::BpfdError,
    multiprog::{DispatcherId, DispatcherInfo, XDP_DISPATCHER_RET, XDP_PASS},
    pull_bytecode::pull_bytecode,
};

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
pub(crate) enum Command {
    /// Load a program
    Load {
        location: String,
        section_name: String,
        program_type: ProgramType,
        attach_type: AttachType,
        username: String,
        responder: Responder<Result<Uuid, BpfdError>>,
    },
    Unload {
        id: Uuid,
        username: String,
        responder: Responder<Result<(), BpfdError>>,
    },
    List {
        responder: Responder<Result<Vec<ProgramInfo>, BpfdError>>,
    },
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub enum AttachType {
    NetworkMultiAttach(NetworkMultiAttach),
    SingleAttach(String),
}

#[derive(Debug, Copy, Clone, Serialize, Deserialize, Eq, PartialEq)]
pub(crate) enum ProgramType {
    Xdp,
    Tc,
    Tracepoint,
}

impl FromStr for ProgramType {
    type Err = BpfdError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "xdp" => Ok(Self::Xdp),
            "tc" => Ok(Self::Tc),
            "tracepoint" => Ok(Self::Tracepoint),
            other => Err(BpfdError::InvalidProgramType(other.to_string())),
        }
    }
}

impl TryFrom<i32> for ProgramType {
    type Error = ParseError;

    fn try_from(t: i32) -> Result<Self, Self::Error> {
        let bpf_api_type = t.try_into()?;
        match bpf_api_type {
            bpfd_api::v1::ProgramType::Xdp => Ok(Self::Xdp),
            bpfd_api::v1::ProgramType::Tc => Ok(Self::Tc),
            bpfd_api::v1::ProgramType::Tracepoint => Ok(Self::Tracepoint),
        }
    }
}

impl fmt::Display for ProgramType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            ProgramType::Xdp => "xdp",
            ProgramType::Tc => "tc",
            ProgramType::Tracepoint => "tracepoint",
        };
        f.write_str(s)
    }
}

#[derive(Debug, Serialize, Hash, Deserialize, Eq, PartialEq, Copy, Clone)]
pub(crate) enum Direction {
    Ingress = 1,
    Egress = 2,
}

impl TryFrom<i32> for Direction {
    type Error = ParseError;

    fn try_from(v: i32) -> Result<Self, Self::Error> {
        match v {
            1 => Ok(Self::Ingress),
            2 => Ok(Self::Egress),
            _ => Err(ParseError::InvalidDirection {}),
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

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct NetworkMultiAttach {
    pub(crate) iface: String,
    pub(crate) priority: i32,
    pub(crate) proceed_on: ProceedOn,
    pub(crate) direction: Option<Direction>,
    pub(crate) position: i32,
}

#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: String,
    pub(crate) name: String,
    pub(crate) location: String,
    pub(crate) program_type: ProgramType,
    pub(crate) attach_type: AttachType,
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
    pub(crate) info: NetworkMultiAttachInfo,
}

impl XdpProgram {
    pub(crate) fn new(data: ProgramData, info: NetworkMultiAttachInfo) -> Self {
        Self { data, info }
    }
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TcProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: NetworkMultiAttachInfo,
    pub(crate) direction: Direction,
}
#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TracepointProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: String,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct ProgramData {
    pub(crate) location: String,
    pub(crate) path: String,
    pub(crate) section_name: String,
    pub(crate) owner: String,
}

impl ProgramData {
    pub(crate) async fn new_from_location(
        location: String,
        section_name: String,
        owner: String,
    ) -> Result<Self, ParseError> {
        let bytecode_url =
            Url::parse(&location).map_err(ParseError::BytecodeLocationParseFailure)?;

        match bytecode_url.scheme() {
            "file" => {
                // File URL isn't local
                if bytecode_url.has_host() {
                    return Err(ParseError::InvalidBytecodeLocation { location });
                }

                Ok(ProgramData {
                    location,
                    path: bytecode_url.path().to_string(),
                    section_name,
                    owner,
                })
            }
            "image" => {
                let image_path = format!(
                    "{}{}",
                    bytecode_url
                        .host_str()
                        .ok_or(ParseError::InvalidBytecodeLocation {
                            location: location.clone()
                        })?,
                    bytecode_url.path()
                );

                let program_overrides = pull_bytecode(&image_path)
                    .await
                    .map_err(ParseError::BytecodePullFaiure)?;

                Ok(ProgramData {
                    location,
                    path: program_overrides.path,
                    section_name: program_overrides.image_meta.section_name,
                    owner,
                })
            }
            _ => Err(ParseError::InvalidBytecodeLocation { location }),
        }
    }
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct NetworkMultiAttachInfo {
    pub(crate) if_name: String,
    pub(crate) if_index: u32,
    #[serde(skip)]
    pub(crate) current_position: Option<usize>,
    pub(crate) metadata: Metadata,
    pub(crate) proceed_on: ProceedOn,
}

impl NetworkMultiAttachInfo {
    pub(crate) fn new(
        if_name: String,
        if_index: u32,
        metadata: Metadata,
        proceed_on: ProceedOn,
    ) -> Self {
        NetworkMultiAttachInfo {
            if_name,
            if_index,
            current_position: None,
            metadata,
            proceed_on,
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub(crate) struct ProceedOn(pub(crate) Vec<i32>);
impl ProceedOn {
    pub fn default_xdp() -> Self {
        ProceedOn(vec![XDP_PASS, XDP_DISPATCHER_RET])
    }

    // Default this to an actual when it's actually supported
    // in the TC dispatcher.
    pub fn default_tc() -> Self {
        ProceedOn(vec![])
    }

    pub(crate) fn mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        for action in self.0.clone().into_iter() {
            proceed_on_mask |= 1 << action;
        }
        proceed_on_mask
    }
}

impl Program {
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

    pub(crate) fn save(&self, uuid: Uuid) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{uuid}");
        serde_json::to_writer(&fs::File::create(path)?, &self)?;
        Ok(())
    }

    pub(crate) fn delete(&self, uuid: Uuid) -> Result<(), anyhow::Error> {
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

    pub(crate) fn load(uuid: Uuid) -> Result<Self, anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{uuid}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        Ok(prog)
    }

    pub(crate) fn kind(&self) -> ProgramType {
        match self {
            Program::Xdp(_) => ProgramType::Xdp,
            Program::Tracepoint(_) => ProgramType::Tracepoint,
            Program::Tc(_) => ProgramType::Tc,
        }
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
