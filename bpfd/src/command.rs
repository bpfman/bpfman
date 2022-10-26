// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

//! Commands between the RPC thread and the BPF thread
use std::{fmt, fs, io::BufReader, str::FromStr};

use bpfd_api::{util::directories::RTDIR_PROGRAMS, ParseError};
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;
use uuid::Uuid;

use crate::errors::BpfdError;

pub(crate) const DEFAULT_XDP_PROCEED_ON_PASS: i32 = 2;
pub(crate) const DEFAULT_XDP_PROCEED_ON_DISPATCHER_RETURN: i32 = 31;
// Default is Pass and DispatcherReturn
pub(crate) const DEFAULT_XDP_ACTIONS_MAP: u32 =
    1 << DEFAULT_XDP_PROCEED_ON_PASS as u32 | 1 << DEFAULT_XDP_PROCEED_ON_DISPATCHER_RETURN as u32;
pub(crate) const DEFAULT_ACTIONS_MAP_TC: u32 = 1 << 3; // TC_ACT_PIPE;

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
pub(crate) enum Command {
    /// Load a program
    Load {
        path: String,
        section_name: String,
        program_type: ProgramType,
        direction: Option<Direction>,
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
    Ingress,
    Egress,
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
    pub(crate) proceed_on: Vec<i32>,
    pub(crate) position: i32,
}

#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: String,
    pub(crate) name: String,
    pub(crate) path: String,
    pub(crate) program_type: ProgramType,
    pub(crate) direction: Option<Direction>,
    pub(crate) attach_type: AttachType,
}

#[derive(Debug, Eq, Ord, PartialEq, PartialOrd, Serialize, Deserialize)]
pub(crate) struct Metadata {
    pub(crate) priority: i32,
    pub(crate) name: String,
    pub(crate) attached: bool,
}

#[derive(Serialize, Deserialize)]
pub(crate) enum Program {
    Xdp(ProgramData, NetworkMultiAttachInfo),
    Tracepoint(ProgramData, String),
    Tc(ProgramData, NetworkMultiAttachInfo, Direction),
}

#[derive(Serialize, Deserialize)]
pub(crate) struct ProgramData {
    pub(crate) path: String,
    pub(crate) section_name: String,
    pub(crate) owner: String,
}

#[derive(Serialize, Deserialize)]
pub(crate) struct NetworkMultiAttachInfo {
    pub(crate) if_name: String,
    pub(crate) if_index: u32,
    #[serde(skip)]
    pub(crate) current_position: Option<usize>,
    pub(crate) metadata: Metadata,
    pub(crate) proceed_on: Vec<i32>,
}

impl NetworkMultiAttachInfo {
    pub(crate) fn proceed_on_mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        if self.proceed_on.is_empty() {
            proceed_on_mask = DEFAULT_XDP_ACTIONS_MAP;
        } else {
            for action in self.proceed_on.clone().into_iter() {
                proceed_on_mask |= 1 << action;
            }
        }
        proceed_on_mask
    }
}

impl Program {
    pub(crate) fn owner(&self) -> &String {
        match self {
            Program::Xdp(d, _) => &d.owner,
            Program::Tracepoint(d, _) => &d.owner,
            Program::Tc(d, _, _) => &d.owner,
        }
    }

    pub(crate) fn set_attached(&mut self) {
        match self {
            Program::Xdp(_, m) => m.metadata.attached = true,
            Program::Tc(_, m, _) => m.metadata.attached = true,
            Program::Tracepoint(_, _) => (),
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
        let path = format!("/var/run/bpfd/fs/prog_{uuid}");
        fs::remove_file(path)?;
        let path = format!("/var/run/bpfd/fs/maps/{uuid}");
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
}
