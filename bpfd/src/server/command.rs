//! Commands between the RPC thread and the BPF thread

use std::fmt;

use bpfd_api::ParseError;
use tokio::sync::oneshot;
use uuid::Uuid;

use crate::server::errors::BpfdError;

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
        attach_type: AttachType,
        username: String,
        responder: Responder<Result<Uuid, BpfdError>>,
    },
    Unload {
        id: Uuid,
        iface: String,
        username: String,
        responder: Responder<Result<(), BpfdError>>,
    },
    List {
        iface: String,
        responder: Responder<Result<InterfaceInfo, BpfdError>>,
    },
}

#[derive(Debug)]
pub(crate) enum AttachType {
    NetworkMultiAttach(NetworkMultiAttach),
    SingleAttach(String),
}

#[derive(Debug, Copy, Clone)]
pub(crate) enum ProgramType {
    Xdp,
    TcIngress,
    TcEgress,
    Tracepoint,
}

impl TryFrom<i32> for ProgramType {
    type Error = ParseError;

    fn try_from(t: i32) -> Result<Self, Self::Error> {
        let bpf_api_type = t.try_into()?;
        match bpf_api_type {
            bpfd_api::v1::ProgramType::Xdp => Ok(Self::Xdp),
            bpfd_api::v1::ProgramType::TcIngress => Ok(Self::TcIngress),
            bpfd_api::v1::ProgramType::TcEgress => Ok(Self::TcEgress),
            bpfd_api::v1::ProgramType::Tracepoint => Ok(Self::Tracepoint),
        }
    }
}

impl fmt::Display for ProgramType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            ProgramType::Xdp => "xdp",
            ProgramType::TcIngress => "tc_in",
            ProgramType::TcEgress => "tc_eg",
            ProgramType::Tracepoint => "tracepoint",
        };
        f.write_str(s)
    }
}

#[derive(Debug)]
pub(crate) struct NetworkMultiAttach {
    pub(crate) iface: String,
    pub(crate) priority: i32,
    pub(crate) proceed_on: Vec<i32>,
}

#[derive(Debug, Clone)]
pub(crate) struct InterfaceInfo {
    pub(crate) xdp_mode: String,
    pub(crate) programs: Vec<ProgramInfo>,
}

#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: String,
    pub(crate) name: String,
    pub(crate) path: String,
    pub(crate) position: usize,
    pub(crate) priority: i32,
    pub(crate) proceed_on: Vec<i32>,
}
