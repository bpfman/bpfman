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
    /// Load a program
    Load(LoadArgs),
    Unload(UnloadArgs),
    List {
        responder: Responder<Result<Vec<Program>, BpfdError>>,
    },
    PullBytecode(PullBytecodeArgs),
}

#[derive(Debug)]
pub(crate) struct LoadArgs {
    pub(crate) program: Program,
    pub(crate) responder: Responder<Result<u32, BpfdError>>,
}

#[derive(Debug, Serialize, Deserialize, Clone, PartialEq)]
pub(crate) enum Program {
    Xdp(XdpProgram),
    Tc(TcProgram),
    Tracepoint(TracepointProgram),
    Kprobe(KprobeProgram),
    Uprobe(UprobeProgram),
    Unsupported(KernelProgramInfo),
}

#[derive(Debug)]
pub(crate) struct UnloadArgs {
    pub(crate) id: u32,
    pub(crate) responder: Responder<Result<(), BpfdError>>,
}

#[derive(Debug)]
pub(crate) struct PullBytecodeArgs {
    pub(crate) image: BytecodeImage,
    pub(crate) responder: Responder<Result<(), BpfdError>>,
}

#[derive(Debug, Serialize, Deserialize, Clone, PartialEq)]
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
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq)]
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
            program_type: prog.program_type(),
            loaded_at: DateTime::<Local>::from(prog.loaded_at())
                .format("%Y-%m-%dT%H:%M:%S%z")
                .to_string(),
            tag: format!("{:x}", prog.tag()),
            gpl_compatible: prog.gpl_compatible(),
            map_ids: prog.map_ids().map_err(BpfdError::BpfProgramError)?,
            btf_id: prog.btf_id().map_or(0, |n| n.into()),
            bytes_xlated: prog.size_translated(),
            jited: prog.size_jitted() != 0,
            bytes_jited: prog.size_jitted(),
            bytes_memlock: prog.memory_locked().map_err(BpfdError::BpfProgramError)?,
            verified_insns: prog.verified_instruction_count(),
        })
    }
}

/// ProgramInfo stores information about bpf programs that are loaded and managed
/// by bpfd.
#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) struct ProgramData {
    // known at load time, set by user
    pub(crate) name: String,
    pub(crate) location: Location,
    pub(crate) metadata: HashMap<String, String>,
    pub(crate) global_data: Option<HashMap<String, Vec<u8>>>,
    pub(crate) map_owner_id: Option<u32>,

    // populated after load
    pub(crate) kernel_info: Option<KernelProgramInfo>,
    pub(crate) map_pin_path: Option<PathBuf>,
    pub(crate) map_used_by: Option<Vec<u32>>,
}

// Only compare fields which are known at load time
impl PartialEq for ProgramData {
    fn eq(&self, other: &Self) -> bool {
        self.name == other.name
            && self.location == other.location
            && self.metadata == other.metadata
            && self.global_data == other.global_data
            && self.map_owner_id == other.map_owner_id
    }
}

impl ProgramData {
    pub(crate) fn new(
        location: Location,
        name: String,
        metadata: HashMap<String, String>,
        global_data: Option<HashMap<String, Vec<u8>>>,
        map_owner_id: Option<u32>,
    ) -> Self {
        Self {
            name,
            location,
            metadata,
            global_data,
            map_owner_id,
            kernel_info: None,
            map_pin_path: None,
            map_used_by: None,
        }
    }

    pub(crate) async fn program_bytes(&mut self) -> Result<Vec<u8>, BpfdError> {
        match self.location.get_program_bytes().await {
            Err(e) => Err(e),
            Ok((v, s)) => {
                match self.location {
                    Location::Image(_) => {
                        // If program name isn't provided and we're loading from a container
                        // image use the program name provided in the image metadata, otherwise
                        // always use the provided program name.
                        let provided_name = self.name.clone();

                        if provided_name.is_empty() {
                            self.name = s;
                        } else if s != provided_name {
                            return Err(BpfdError::BytecodeMetaDataMismatch {
                                image_program_name: s,
                                provided_program_name: provided_name,
                            });
                        }
                    }
                    Location::File(_) => {}
                }
                Ok(v)
            }
        }
    }
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) struct XdpProgram {
    pub(crate) data: ProgramData,
    // known at load time
    pub(crate) priority: i32,
    pub(crate) iface: String,
    pub(crate) proceed_on: XdpProceedOn,
    // populated after load
    #[serde(skip)]
    pub(crate) current_position: Option<usize>,
    pub(crate) if_index: Option<u32>,
    pub(crate) attached: bool,
}

// Only compare fields which are known at load time
impl PartialEq for XdpProgram {
    fn eq(&self, other: &Self) -> bool {
        self.data == other.data
            && self.priority == other.priority
            && self.proceed_on == other.proceed_on
    }
}

impl XdpProgram {
    pub(crate) fn new(
        data: ProgramData,
        priority: i32,
        iface: String,
        proceed_on: XdpProceedOn,
    ) -> Self {
        Self {
            data,
            priority,
            iface,
            proceed_on,
            current_position: None,
            if_index: None,
            attached: false,
        }
    }
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) struct TcProgram {
    pub(crate) data: ProgramData,
    // known at load time
    pub(crate) priority: i32,
    pub(crate) iface: String,
    pub(crate) proceed_on: TcProceedOn,
    pub(crate) direction: Direction,
    // populated after load
    #[serde(skip)]
    pub(crate) current_position: Option<usize>,
    pub(crate) if_index: Option<u32>,
    pub(crate) attached: bool,
}

// Only compare fields which are known at load time
impl PartialEq for TcProgram {
    fn eq(&self, other: &Self) -> bool {
        self.data == other.data
            && self.priority == other.priority
            && self.proceed_on == other.proceed_on
            && self.direction == other.direction
    }
}

impl TcProgram {
    pub(crate) fn new(
        data: ProgramData,
        priority: i32,
        iface: String,
        proceed_on: TcProceedOn,
        direction: Direction,
    ) -> Self {
        Self {
            data,
            priority,
            iface,
            proceed_on,
            direction,
            current_position: None,
            if_index: None,
            attached: false,
        }
    }
}

#[derive(Debug, Serialize, Deserialize, Clone, PartialEq)]
pub(crate) struct TracepointProgram {
    pub(crate) data: ProgramData,
    // known at load time
    pub(crate) tracepoint: String,
}

impl TracepointProgram {
    pub(crate) fn new(data: ProgramData, tracepoint: String) -> Self {
        Self { data, tracepoint }
    }
}

#[derive(Debug, Serialize, Deserialize, Clone, PartialEq)]
pub(crate) struct KprobeProgram {
    pub(crate) data: ProgramData,
    // Known at load time
    pub(crate) fn_name: String,
    pub(crate) offset: u64,
    pub(crate) retprobe: bool,
    pub(crate) namespace: Option<String>,
}

impl KprobeProgram {
    pub(crate) fn new(
        data: ProgramData,
        fn_name: String,
        offset: u64,
        retprobe: bool,
        namespace: Option<String>,
    ) -> Self {
        Self {
            data,
            fn_name,
            offset,
            retprobe,
            namespace,
        }
    }
}

#[derive(Debug, Serialize, Deserialize, Clone, PartialEq)]
pub(crate) struct UprobeProgram {
    pub(crate) data: ProgramData,
    // Known at load time
    pub(crate) fn_name: Option<String>,
    pub(crate) offset: u64,
    pub(crate) target: String,
    pub(crate) retprobe: bool,
    pub(crate) pid: Option<i32>,
    pub(crate) namespace: Option<String>,
}

impl UprobeProgram {
    pub(crate) fn new(
        data: ProgramData,
        fn_name: Option<String>,
        offset: u64,
        target: String,
        retprobe: bool,
        pid: Option<i32>,
        namespace: Option<String>,
    ) -> Self {
        Self {
            data,
            fn_name,
            offset,
            target,
            retprobe,
            pid,
            namespace,
        }
    }
}

impl Program {
    pub(crate) fn kind(&self) -> ProgramType {
        match self {
            Program::Xdp(_) => ProgramType::Xdp,
            Program::Tc(_) => ProgramType::Tc,
            Program::Tracepoint(_) => ProgramType::Tracepoint,
            Program::Kprobe(_) => ProgramType::Probe,
            Program::Uprobe(_) => ProgramType::Probe,
            Program::Unsupported(i) => i.program_type.try_into().unwrap(),
        }
    }

    pub(crate) fn dispatcher_id(&self) -> Option<DispatcherId> {
        match self {
            Program::Xdp(p) => Some(DispatcherId::Xdp(DispatcherInfo(p.if_index.unwrap(), None))),
            Program::Tc(p) => Some(DispatcherId::Tc(DispatcherInfo(
                // if_index should be resolved by this point
                p.if_index.unwrap(),
                Some(p.direction),
            ))),
            _ => None,
        }
    }

    // TODO(astoycos) move this to `ProgramData` specific methods
    pub(crate) fn data_mut(&mut self) -> &mut ProgramData {
        match self {
            Program::Xdp(p) => Some(&mut p.data),
            Program::Tracepoint(p) => Some(&mut p.data),
            Program::Tc(p) => Some(&mut p.data),
            Program::Kprobe(p) => Some(&mut p.data),
            Program::Uprobe(p) => Some(&mut p.data),
            Program::Unsupported(_) => None,
        }
        .expect("Unsupported maps have no bpfd specific data")
    }

    // TODO convert to using data_op everwhere, the extra unwrap is just a bit
    // cumbersome.
    pub(crate) fn data(&self) -> &ProgramData {
        match self {
            Program::Xdp(p) => Some(&p.data),
            Program::Tracepoint(p) => Some(&p.data),
            Program::Tc(p) => Some(&p.data),
            Program::Kprobe(p) => Some(&p.data),
            Program::Uprobe(p) => Some(&p.data),
            Program::Unsupported(_) => None,
        }
        .expect("Unsupported maps have no bpfd specific data")
    }

    pub(crate) fn data_op(&self) -> Option<&ProgramData> {
        match self {
            Program::Xdp(p) => Some(&p.data),
            Program::Tracepoint(p) => Some(&p.data),
            Program::Tc(p) => Some(&p.data),
            Program::Kprobe(p) => Some(&p.data),
            Program::Uprobe(p) => Some(&p.data),
            Program::Unsupported(_) => None,
        }
    }

    pub(crate) fn attached(&self) -> bool {
        match self {
            Program::Xdp(p) => p.attached,
            Program::Tc(p) => p.attached,
            _ => false,
        }
    }

    pub(crate) fn set_attached(&mut self) {
        match self {
            Program::Xdp(p) => p.attached = true,
            Program::Tc(p) => p.attached = true,
            _ => (),
        }
    }

    pub(crate) fn set_position(&mut self, pos: Option<usize>) {
        match self {
            Program::Xdp(p) => p.current_position = pos,
            Program::Tc(p) => p.current_position = pos,
            Program::Tracepoint(_) => (),
            Program::Kprobe(_) => (),
            Program::Uprobe(_) => (),
            Program::Unsupported(_) => (),
        }
    }

    pub(crate) fn set_kernel_info(&mut self, info: KernelProgramInfo) {
        match self {
            Program::Xdp(p) => p.data.kernel_info = Some(info),
            Program::Tc(p) => p.data.kernel_info = Some(info),
            Program::Tracepoint(p) => p.data.kernel_info = Some(info),
            Program::Kprobe(p) => p.data.kernel_info = Some(info),
            Program::Uprobe(p) => p.data.kernel_info = Some(info),
            _ => panic!("Cannot set kernel info for unsupported program"),
        }
    }

    pub(crate) fn get_kernel_info(self) -> Option<KernelProgramInfo> {
        match self {
            Program::Xdp(p) => p.data.kernel_info,
            Program::Tc(p) => p.data.kernel_info,
            Program::Tracepoint(p) => p.data.kernel_info,
            Program::Kprobe(p) => p.data.kernel_info,
            Program::Uprobe(p) => p.data.kernel_info,
            // KernelProgramInfo will never be nil for Unsupported programs
            Program::Unsupported(p) => Some(p),
        }
    }

    pub(crate) fn save(&self, id: u32) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{id}");
        serde_json::to_writer(&fs::File::create(path)?, &self)?;
        Ok(())
    }

    pub(crate) fn delete(&self, id: u32) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{id}");
        if PathBuf::from(&path).exists() {
            fs::remove_file(path)?;
        }

        let path = format!("{RTDIR_FS}/prog_{id}");
        if PathBuf::from(&path).exists() {
            fs::remove_file(path)?;
        }
        let path = format!("{RTDIR_FS}/prog_{id}_link");
        if PathBuf::from(&path).exists() {
            fs::remove_file(path)?;
        }
        Ok(())
    }

    pub(crate) fn load(id: u32) -> Result<Self, anyhow::Error> {
        let path = format!("{RTDIR_PROGRAMS}/{id}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        Ok(prog)
    }

    pub(crate) fn if_index(&self) -> Option<u32> {
        match self {
            Program::Xdp(p) => p.if_index,
            Program::Tc(p) => p.if_index,
            _ => None,
        }
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) {
        match self {
            Program::Xdp(p) => p.if_index = Some(if_index),
            Program::Tc(p) => p.if_index = Some(if_index),
            _ => (),
        }
    }

    pub(crate) fn if_name(&self) -> Option<String> {
        match self {
            Program::Xdp(p) => Some(p.iface.clone()),
            Program::Tc(p) => Some(p.iface.clone()),
            _ => None,
        }
    }

    pub(crate) fn priority(&self) -> Option<i32> {
        match self {
            Program::Xdp(p) => Some(p.priority),
            Program::Tc(p) => Some(p.priority),
            _ => None,
        }
    }

    pub(crate) fn location(&self) -> Option<Location> {
        match self {
            Program::Xdp(p) => Some(p.data.clone().location),
            Program::Tracepoint(p) => Some(p.data.clone().location),
            Program::Tc(p) => Some(p.data.clone().location),
            Program::Kprobe(p) => Some(p.data.clone().location),
            Program::Uprobe(p) => Some(p.data.clone().location),
            Program::Unsupported(_) => None,
        }
    }

    pub(crate) fn name(&self) -> String {
        match self {
            Program::Xdp(p) => p.data.clone().name,
            Program::Tracepoint(p) => p.data.clone().name,
            Program::Tc(p) => p.data.clone().name,
            Program::Kprobe(p) => p.data.clone().name,
            Program::Uprobe(p) => p.data.clone().name,
            Program::Unsupported(k) => k.clone().name,
        }
    }
}

#[derive(Debug, Clone)]
pub(crate) struct BpfMap {
    pub(crate) map_pin_path: PathBuf,
    pub(crate) used_by: Vec<u32>,
}
