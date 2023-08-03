// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

//! Commands between the RPC thread and the BPF thread
use std::{collections::HashMap, fmt, fs, io::BufReader, path::PathBuf};

use aya::programs::ProgramInfo as AyaProgInfo;
use bpfd_api::{
    util::directories::{RTDIR_FS, RTDIR_PROGRAMS},
    ParseError, ProgramType, TcProceedOn, XdpProceedOn,
};
use chrono::{prelude::DateTime, Local};
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;
use uuid::Uuid;

use crate::{
    errors::BpfdError,
    multiprog::{DispatcherId, DispatcherInfo},
    oci_utils::{image_manager::get_bytecode_from_image_store, BytecodeImage},
};

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
#[allow(clippy::large_enum_variant)]
pub(crate) enum Command {
    /// Load an XDP program
    LoadXDP(LoadXDPArgs),
    /// Load a TC program
    LoadTC(LoadTCArgs),
    // Load a Tracepoint program
    LoadTracepoint(LoadTracepointArgs),
    // Load a kprobe program
    LoadKprobe(LoadKprobeArgs),
    // Load a uprobe program
    LoadUprobe(LoadUprobeArgs),
    Unload(UnloadArgs),
    List {
        responder: Responder<Result<Vec<ProgramInfo>, BpfdError>>,
    },
    PullBytecode(PullBytecodeArgs),
}

#[derive(Debug)]
pub(crate) struct LoadXDPArgs {
    pub(crate) location: Location,
    pub(crate) section_name: String,
    pub(crate) id: Option<Uuid>,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) iface: String,
    pub(crate) priority: i32,
    pub(crate) proceed_on: XdpProceedOn,
    pub(crate) username: String,
    pub(crate) responder: Responder<Result<Uuid, BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct LoadTCArgs {
    pub(crate) location: Location,
    pub(crate) section_name: String,
    pub(crate) id: Option<Uuid>,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) iface: String,
    pub(crate) priority: i32,
    pub(crate) direction: Direction,
    pub(crate) proceed_on: TcProceedOn,
    pub(crate) username: String,
    pub(crate) responder: Responder<Result<Uuid, BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct LoadTracepointArgs {
    pub(crate) location: Location,
    pub(crate) id: Option<Uuid>,
    pub(crate) section_name: String,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) tracepoint: String,
    pub(crate) username: String,
    pub(crate) responder: Responder<Result<Uuid, BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct LoadKprobeArgs {
    pub(crate) location: Location,
    pub(crate) id: Option<Uuid>,
    pub(crate) section_name: String,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) fn_name: String,
    pub(crate) offset: u64,
    pub(crate) retprobe: bool,
    pub(crate) _namespace: Option<String>,
    pub(crate) username: String,
    pub(crate) responder: Responder<Result<Uuid, BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct LoadUprobeArgs {
    pub(crate) location: Location,
    pub(crate) id: Option<Uuid>,
    pub(crate) section_name: String,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) fn_name: Option<String>,
    pub(crate) offset: u64,
    pub(crate) target: String,
    pub(crate) retprobe: bool,
    pub(crate) pid: Option<i32>,
    pub(crate) _namespace: Option<String>,
    pub(crate) username: String,
    pub(crate) responder: Responder<Result<Uuid, BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct UnloadArgs {
    pub(crate) id: Uuid,
    pub(crate) username: String,
    pub(crate) responder: Responder<Result<(), BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct PullBytecodeArgs {
    pub(crate) image: BytecodeImage,
    pub(crate) responder: Responder<Result<(), BpfdError>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) enum Location {
    Image(BytecodeImage),
    File(String),
}

impl Location {
    async fn get_program_bytes(&self) -> Result<(Vec<u8>, String), BpfdError> {
        match self {
            Location::File(l) => Ok((crate::utils::read(l).await?, "".to_owned())),
            Location::Image(l) => {
                let (path, section_name) = l
                    .clone()
                    .get_image(None)
                    .await
                    .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?;

                Ok((
                    get_bytecode_from_image_store(path)
                        .await
                        .map_err(|e| BpfdError::Error(format!("Bytecode loading error: {e}")))?,
                    section_name,
                ))
            }
        }
    }
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

/// KernelProgramInfo stores information about ALL bpf programs loaded
/// on a system.
#[derive(Serialize, Deserialize, Debug, Clone)]
pub(crate) struct KernelProgramInfo {
    pub(crate) id: u32,
    pub(crate) name: String,
    pub(crate) program_type: u32,
    pub(crate) loaded_at: String,
    pub(crate) tag: String,
    pub(crate) gpl_compatible: bool,
    pub(crate) map_ids: Vec<u32>,
    pub(crate) btf_id: u32,
    pub(crate) bytes_xlated: u32,
    pub(crate) jited: bool,
    pub(crate) bytes_jited: u32,
    pub(crate) bytes_memlock: u32,
    pub(crate) verified_insns: u32,
}

impl TryFrom<AyaProgInfo> for KernelProgramInfo {
    type Error = BpfdError;

    fn try_from(prog: AyaProgInfo) -> Result<Self, Self::Error> {
        Ok(KernelProgramInfo {
            id: prog.id(),
            name: prog.name_as_str().unwrap().to_string(),
            program_type: prog.type_(),
            loaded_at: DateTime::<Local>::from(prog.loaded_at())
                .format("%Y-%m-%dT%H:%M:%S%z")
                .to_string(),
            tag: format!("{:x}", prog.tag()),
            gpl_compatible: prog.gpl_compatible(),
            map_ids: prog.map_ids().map_err(BpfdError::BpfProgramError)?,
            btf_id: prog.btf_id(),
            bytes_xlated: prog.bytes_xlated(),
            jited: prog.bytes_jited() != 0,
            bytes_jited: prog.bytes_jited(),
            bytes_memlock: prog.bytes_memlock().map_err(BpfdError::BpfProgramError)?,
            verified_insns: prog.verified_insns(),
        })
    }
}

/// ProgramInfo stores information about bpf programs loaded on a system
/// which are managed via bpfd.
#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: Option<Uuid>,
    pub(crate) name: Option<String>,
    pub(crate) program_type: Option<u32>,
    pub(crate) location: Option<Location>,
    pub(crate) global_data: Option<HashMap<String, Vec<u8>>>,
    pub(crate) map_pin_path: Option<String>,
    pub(crate) map_used_by: Option<Vec<Uuid>>,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) attach_info: Option<AttachInfo>,
    pub(crate) kernel_info: KernelProgramInfo,
}

#[derive(Debug, Clone)]
pub(crate) enum AttachInfo {
    Xdp(XdpAttachInfo),
    Tc(TcAttachInfo),
    Tracepoint(TracepointAttachInfo),
    Kprobe(KprobeAttachInfo),
    Uprobe(UprobeAttachInfo),
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

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct KprobeAttachInfo {
    pub(crate) fn_name: String,
    pub(crate) offset: u64,
    pub(crate) retprobe: bool,
    pub(crate) namespace: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct UprobeAttachInfo {
    pub(crate) fn_name: Option<String>,
    pub(crate) offset: u64,
    pub(crate) target: String,
    pub(crate) retprobe: bool,
    pub(crate) pid: Option<i32>,
    pub(crate) namespace: Option<String>,
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
    Kprobe(KprobeProgram),
    Uprobe(UprobeProgram),
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
pub(crate) struct KprobeProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: KprobeProgramInfo,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct UprobeProgram {
    pub(crate) data: ProgramData,
    pub(crate) info: UprobeProgramInfo,
}

impl KprobeProgram {
    pub(crate) fn _new(data: ProgramData, info: KprobeProgramInfo) -> Self {
        Self { data, info }
    }
}

impl UprobeProgram {
    pub(crate) fn _new(data: ProgramData, info: UprobeProgramInfo) -> Self {
        Self { data, info }
    }
}

// ProgramData represents all of the core information needed to load
// a program reguardless of ProgramType.
#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct ProgramData {
    pub(crate) location: Location,
    pub(crate) section_name: String,
    pub(crate) global_data: HashMap<String, Vec<u8>>,
    pub(crate) owner: String,
    pub(crate) map_owner_uuid: Option<Uuid>,
    pub(crate) kernel_info: Option<KernelProgramInfo>,
}

impl ProgramData {
    pub(crate) fn new(
        location: Location,
        section_name: String,
        global_data: HashMap<String, Vec<u8>>,
        map_owner_uuid: Option<Uuid>,
        owner: String,
    ) -> Self {
        Self {
            location,
            section_name,
            owner,
            global_data,
            map_owner_uuid,
            kernel_info: None,
        }
    }

    pub(crate) async fn program_bytes(&mut self) -> Result<Vec<u8>, BpfdError> {
        match self.location.get_program_bytes().await {
            Err(e) => Err(e),
            Ok((v, s)) => {
                // If section name isn't provided and we're loading from a container
                // image use the section name provided in the image metadata, otherwise
                // always use the provided section name.
                let provided_sec_name = self.section_name.clone();

                if provided_sec_name.is_empty() {
                    self.section_name = s;
                } else if s != provided_sec_name {
                    return Err(BpfdError::BytecodeMetaDataMismatch {
                        image_sec_name: s,
                        provided_sec_name,
                    });
                }

                Ok(v)
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
    pub(crate) direction: Direction,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct TracepointProgramInfo {
    pub(crate) tracepoint: String,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct KprobeProgramInfo {
    pub(crate) fn_name: String,
    pub(crate) offset: u64,
    pub(crate) retprobe: bool,
    pub(crate) namespace: Option<String>,
}

#[derive(Serialize, Deserialize, Clone)]
pub(crate) struct UprobeProgramInfo {
    pub(crate) fn_name: Option<String>,
    pub(crate) offset: u64,
    pub(crate) target: String,
    pub(crate) retprobe: bool,
    pub(crate) pid: Option<i32>,
    pub(crate) namespace: Option<String>,
}

impl Program {
    pub(crate) fn kind(&self) -> ProgramType {
        match self {
            Program::Xdp(_) => ProgramType::Xdp,
            Program::Tc(_) => ProgramType::Tc,
            Program::Tracepoint(_) => ProgramType::Tracepoint,
            Program::Kprobe(_) => ProgramType::Probe,
            Program::Uprobe(_) => ProgramType::Probe,
        }
    }

    pub(crate) fn dispatcher_id(&self) -> Option<DispatcherId> {
        match self {
            Program::Xdp(p) => Some(DispatcherId::Xdp(DispatcherInfo(p.info.if_index, None))),
            Program::Tc(p) => Some(DispatcherId::Tc(DispatcherInfo(
                p.info.if_index,
                Some(p.info.direction),
            ))),
            _ => None,
        }
    }

    pub(crate) fn data_mut(&mut self) -> &mut ProgramData {
        match self {
            Program::Xdp(p) => &mut p.data,
            Program::Tracepoint(p) => &mut p.data,
            Program::Tc(p) => &mut p.data,
            Program::Kprobe(p) => &mut p.data,
            Program::Uprobe(p) => &mut p.data,
        }
    }

    pub(crate) fn data(&self) -> &ProgramData {
        match self {
            Program::Xdp(p) => &p.data,
            Program::Tracepoint(p) => &p.data,
            Program::Tc(p) => &p.data,
            Program::Kprobe(p) => &p.data,
            Program::Uprobe(p) => &p.data,
        }
    }

    pub(crate) fn metadata(&self) -> Option<&Metadata> {
        match self {
            Program::Xdp(p) => Some(&p.info.metadata),
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(&p.info.metadata),
            Program::Kprobe(_) => None,
            Program::Uprobe(_) => None,
        }
    }

    pub(crate) fn owner(&self) -> &String {
        match self {
            Program::Xdp(p) => &p.data.owner,
            Program::Tracepoint(p) => &p.data.owner,
            Program::Tc(p) => &p.data.owner,
            Program::Kprobe(p) => &p.data.owner,
            Program::Uprobe(p) => &p.data.owner,
        }
    }

    pub(crate) fn set_attached(&mut self) {
        match self {
            Program::Xdp(p) => p.info.metadata.attached = true,
            Program::Tc(p) => p.info.metadata.attached = true,
            Program::Tracepoint(_) => (),
            Program::Kprobe(_) => (),
            Program::Uprobe(_) => (),
        }
    }

    pub(crate) fn set_position(&mut self, pos: Option<usize>) {
        match self {
            Program::Xdp(p) => p.info.current_position = pos,
            Program::Tc(p) => p.info.current_position = pos,
            Program::Tracepoint(_) => (),
            Program::Kprobe(_) => (),
            Program::Uprobe(_) => (),
        }
    }

    pub(crate) fn set_kernel_info(&mut self, info: KernelProgramInfo) {
        match self {
            Program::Xdp(p) => p.data.kernel_info = Some(info),
            Program::Tc(p) => p.data.kernel_info = Some(info),
            Program::Tracepoint(p) => p.data.kernel_info = Some(info),
            Program::Kprobe(p) => p.data.kernel_info = Some(info),
            Program::Uprobe(p) => p.data.kernel_info = Some(info),
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
        Ok(())
    }

    pub(crate) fn load(uuid: Uuid) -> Result<Self, anyhow::Error> {
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
            Program::Kprobe(_) => None,
            Program::Uprobe(_) => None,
        }
    }

    pub(crate) fn if_name(&self) -> Option<String> {
        match self {
            Program::Xdp(p) => Some(p.info.if_name.clone()),
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(p.info.if_name.clone()),
            Program::Kprobe(_) => None,
            Program::Uprobe(_) => None,
        }
    }

    pub(crate) fn direction(&self) -> Option<Direction> {
        match self {
            Program::Xdp(_) => None,
            Program::Tracepoint(_) => None,
            Program::Tc(p) => Some(p.info.direction),
            Program::Kprobe(_) => None,
            Program::Uprobe(_) => None,
        }
    }
}

#[derive(Debug, Clone)]
pub(crate) struct BpfMap {
    pub(crate) map_pin_path: String,
    pub(crate) used_by: Vec<Uuid>,
}
