// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Commands between the RPC thread and the BPF thread
use std::{
    collections::HashMap,
    fmt, fs,
    io::BufReader,
    path::{Path, PathBuf},
};

use aya::programs::ProgramInfo as AyaProgInfo;
use bpfman_api::{
    util::directories::{RTDIR_FS, RTDIR_PROGRAMS},
    v1::{
        attach_info::Info, bytecode_location::Location as V1Location, AttachInfo, BytecodeLocation,
        KernelProgramInfo as V1KernelProgramInfo, KprobeAttachInfo, ProgramInfo as V1ProgramInfo,
        TcAttachInfo, TracepointAttachInfo, UprobeAttachInfo, XdpAttachInfo,
    },
    ParseError, ProgramType, TcProceedOn, XdpProceedOn,
};
use chrono::{prelude::DateTime, Local};
use log::info;
use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc::Sender, oneshot};

use crate::{
    errors::BpfmanError,
    multiprog::{DispatcherId, DispatcherInfo},
    oci_utils::image_manager::{BytecodeImage, Command as ImageManagerCommand},
};

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
pub(crate) enum Command {
    /// Load a program
    Load(LoadArgs),
    Unload(UnloadArgs),
    List {
        responder: Responder<Result<Vec<Program>, BpfmanError>>,
    },
    Get(GetArgs),
    PullBytecode(PullBytecodeArgs),
}

#[derive(Debug)]
pub(crate) struct LoadArgs {
    pub(crate) program: Program,
    pub(crate) responder: Responder<Result<Program, BpfmanError>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
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
    pub(crate) responder: Responder<Result<(), BpfmanError>>,
}

#[derive(Debug)]
pub(crate) struct GetArgs {
    pub(crate) id: u32,
    pub(crate) responder: Responder<Result<Program, BpfmanError>>,
}

#[derive(Debug)]
pub(crate) struct PullBytecodeArgs {
    pub(crate) image: BytecodeImage,
    pub(crate) responder: Responder<Result<(), BpfmanError>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub(crate) enum Location {
    Image(BytecodeImage),
    File(String),
}

// TODO astoycos remove this impl, as it's only needed for a hack in the rebuild
// dispatcher code.
impl Default for Location {
    fn default() -> Self {
        Location::File(String::new())
    }
}

impl Location {
    async fn get_program_bytes(
        &self,
        image_manager: Sender<ImageManagerCommand>,
    ) -> Result<(Vec<u8>, String), BpfmanError> {
        match self {
            Location::File(l) => Ok((crate::utils::read(l).await?, "".to_owned())),
            Location::Image(l) => {
                let (tx, rx) = oneshot::channel();
                image_manager
                    .send(ImageManagerCommand::Pull {
                        image: l.image_url.clone(),
                        pull_policy: l.image_pull_policy.clone(),
                        username: l.username.clone(),
                        password: l.password.clone(),
                        resp: tx,
                    })
                    .await
                    .map_err(|e| BpfmanError::RpcSendError(e.into()))?;
                let (path, bpf_function_name) = rx
                    .await
                    .map_err(BpfmanError::RpcRecvError)?
                    .map_err(BpfmanError::BpfBytecodeError)?;

                let (tx, rx) = oneshot::channel();
                image_manager
                    .send(ImageManagerCommand::GetBytecode { path, resp: tx })
                    .await
                    .map_err(|e| BpfmanError::RpcSendError(e.into()))?;

                let bytecode = rx
                    .await
                    .map_err(BpfmanError::RpcRecvError)?
                    .map_err(BpfmanError::BpfBytecodeError)?;

                Ok((bytecode, bpf_function_name))
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
    type Error = BpfmanError;

    fn try_from(prog: AyaProgInfo) -> Result<Self, Self::Error> {
        Ok(KernelProgramInfo {
            id: prog.id(),
            name: prog
                .name_as_str()
                .expect("Program name is not valid unicode")
                .to_string(),
            program_type: prog.program_type(),
            loaded_at: DateTime::<Local>::from(prog.loaded_at())
                .format("%Y-%m-%dT%H:%M:%S%z")
                .to_string(),
            tag: format!("{:x}", prog.tag()),
            gpl_compatible: prog.gpl_compatible(),
            map_ids: prog.map_ids().map_err(BpfmanError::BpfProgramError)?,
            btf_id: prog.btf_id().map_or(0, |n| n.into()),
            bytes_xlated: prog.size_translated(),
            jited: prog.size_jitted() != 0,
            bytes_jited: prog.size_jitted(),
            bytes_memlock: prog.memory_locked().map_err(BpfmanError::BpfProgramError)?,
            verified_insns: prog.verified_instruction_count(),
        })
    }
}

impl TryFrom<&Program> for V1ProgramInfo {
    type Error = BpfmanError;

    fn try_from(program: &Program) -> Result<Self, Self::Error> {
        let data = program.data()?;

        let bytecode = match program.location() {
            Some(l) => match l {
                crate::command::Location::Image(m) => {
                    Some(BytecodeLocation {
                        location: Some(V1Location::Image(bpfman_api::v1::BytecodeImage {
                            url: m.get_url().to_string(),
                            image_pull_policy: m.get_pull_policy().to_owned() as i32,
                            // Never dump Plaintext Credentials
                            username: Some(String::new()),
                            password: Some(String::new()),
                        })),
                    })
                }
                crate::command::Location::File(m) => Some(BytecodeLocation {
                    location: Some(V1Location::File(m.to_string())),
                }),
            },
            None => None,
        };

        let attach_info = AttachInfo {
            info: match program.clone() {
                Program::Xdp(p) => Some(Info::XdpAttachInfo(XdpAttachInfo {
                    priority: p.priority,
                    iface: p.iface,
                    position: p.current_position.unwrap_or(0) as i32,
                    proceed_on: p.proceed_on.as_action_vec(),
                })),
                Program::Tc(p) => Some(Info::TcAttachInfo(TcAttachInfo {
                    priority: p.priority,
                    iface: p.iface,
                    position: p.current_position.unwrap_or(0) as i32,
                    direction: p.direction.to_string(),
                    proceed_on: p.proceed_on.as_action_vec(),
                })),
                Program::Tracepoint(p) => Some(Info::TracepointAttachInfo(TracepointAttachInfo {
                    tracepoint: p.tracepoint,
                })),
                Program::Kprobe(p) => Some(Info::KprobeAttachInfo(KprobeAttachInfo {
                    fn_name: p.fn_name,
                    offset: p.offset,
                    retprobe: p.retprobe,
                    namespace: p.namespace,
                })),
                Program::Uprobe(p) => Some(Info::UprobeAttachInfo(UprobeAttachInfo {
                    fn_name: p.fn_name,
                    offset: p.offset,
                    target: p.target,
                    retprobe: p.retprobe,
                    pid: p.pid,
                    namespace: p.namespace,
                })),
                Program::Unsupported(_) => None,
            },
        };

        // Populate the Program Info with bpfman data
        Ok(V1ProgramInfo {
            name: data.name().to_owned(),
            bytecode,
            attach: Some(attach_info),
            global_data: data.global_data().to_owned(),
            map_owner_id: data.map_owner_id(),
            map_pin_path: data
                .map_pin_path()
                .map_or(String::new(), |v| v.to_str().unwrap().to_string()),
            map_used_by: data
                .maps_used_by()
                .map_or(vec![], |m| m.iter().map(|m| m.to_string()).collect()),
            metadata: data.metadata().to_owned(),
        })
    }
}

impl TryFrom<&Program> for V1KernelProgramInfo {
    type Error = BpfmanError;

    fn try_from(program: &Program) -> Result<Self, Self::Error> {
        // Get the Kernel Info.
        let kernel_info = program.kernel_info().ok_or(BpfmanError::Error(
            "program kernel info not available".to_string(),
        ))?;

        // Populate the Kernel Info.
        Ok(V1KernelProgramInfo {
            id: kernel_info.id,
            name: kernel_info.name.to_owned(),
            program_type: program.kind() as u32,
            loaded_at: kernel_info.loaded_at.to_owned(),
            tag: kernel_info.tag.to_owned(),
            gpl_compatible: kernel_info.gpl_compatible,
            map_ids: kernel_info.map_ids.to_owned(),
            btf_id: kernel_info.btf_id,
            bytes_xlated: kernel_info.bytes_xlated,
            jited: kernel_info.jited,
            bytes_jited: kernel_info.bytes_jited,
            bytes_memlock: kernel_info.bytes_memlock,
            verified_insns: kernel_info.verified_insns,
        })
    }
}

/// ProgramInfo stores information about bpf programs that are loaded and managed
/// by bpfman.
#[derive(Debug, Serialize, Deserialize, Clone, Default)]
pub(crate) struct ProgramData {
    // known at load time, set by user
    name: String,
    location: Location,
    metadata: HashMap<String, String>,
    global_data: HashMap<String, Vec<u8>>,
    map_owner_id: Option<u32>,

    // populated after load
    kernel_info: Option<KernelProgramInfo>,
    map_pin_path: Option<PathBuf>,
    maps_used_by: Option<Vec<u32>>,

    // program_bytes is used to temporarily cache the raw program data during
    // the loading process.  It MUST be cleared following a load so that there
    // is not a long lived copy of the program data living on the heap.
    #[serde(skip_serializing, skip_deserializing)]
    program_bytes: Vec<u8>,
}

impl ProgramData {
    pub(crate) fn new(
        location: Location,
        name: String,
        metadata: HashMap<String, String>,
        global_data: HashMap<String, Vec<u8>>,
        map_owner_id: Option<u32>,
    ) -> Self {
        Self {
            name,
            location,
            metadata,
            global_data,
            map_owner_id,
            program_bytes: Vec::new(),
            kernel_info: None,
            map_pin_path: None,
            maps_used_by: None,
        }
    }

    pub(crate) fn name(&self) -> &str {
        &self.name
    }

    pub(crate) fn id(&self) -> Option<u32> {
        // use as_ref here so we don't consume self.
        self.kernel_info.as_ref().map(|i| i.id)
    }

    pub(crate) fn set_kernel_info(&mut self, info: Option<KernelProgramInfo>) {
        self.kernel_info = info
    }

    pub(crate) fn kernel_info(&self) -> Option<&KernelProgramInfo> {
        self.kernel_info.as_ref()
    }

    pub(crate) fn global_data(&self) -> &HashMap<String, Vec<u8>> {
        &self.global_data
    }

    pub(crate) fn metadata(&self) -> &HashMap<String, String> {
        &self.metadata
    }

    pub(crate) fn set_map_pin_path(&mut self, path: Option<PathBuf>) {
        self.map_pin_path = path
    }

    pub(crate) fn map_pin_path(&self) -> Option<&Path> {
        self.map_pin_path.as_deref()
    }

    pub(crate) fn map_owner_id(&self) -> Option<u32> {
        self.map_owner_id
    }

    pub(crate) fn set_maps_used_by(&mut self, used_by: Option<Vec<u32>>) {
        self.maps_used_by = used_by
    }

    pub(crate) fn maps_used_by(&self) -> Option<&Vec<u32>> {
        self.maps_used_by.as_ref()
    }

    pub(crate) fn program_bytes(&self) -> &[u8] {
        &self.program_bytes
    }

    // In order to ensure that the program bytes, which can be a large amount
    // of data is only stored for as long as needed, make sure to call
    // clear_program_bytes following a load.
    pub(crate) fn clear_program_bytes(&mut self) {
        self.program_bytes = Vec::new();
    }

    pub(crate) async fn set_program_bytes(
        &mut self,
        image_manager: Sender<ImageManagerCommand>,
    ) -> Result<(), BpfmanError> {
        match self.location.get_program_bytes(image_manager).await {
            Err(e) => Err(e),
            Ok((v, s)) => {
                match &self.location {
                    Location::Image(l) => {
                        info!(
                            "Loading program bytecode from container image: {}",
                            l.get_url()
                        );
                        // If program name isn't provided and we're loading from a container
                        // image use the program name provided in the image metadata, otherwise
                        // always use the provided program name.
                        let provided_name = self.name.clone();

                        if provided_name.is_empty() {
                            self.name = s;
                        } else if s != provided_name {
                            return Err(BpfmanError::BytecodeMetaDataMismatch {
                                image_prog_name: s,
                                provided_prog_name: provided_name,
                            });
                        }
                    }
                    Location::File(l) => {
                        info!("Loading program bytecode from file: {}", l);
                    }
                }
                self.program_bytes = v;
                Ok(())
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

#[derive(Debug, Serialize, Deserialize, Clone)]
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

#[derive(Debug, Serialize, Deserialize, Clone)]
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

#[derive(Debug, Serialize, Deserialize, Clone)]
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
            Program::Xdp(p) => Some(DispatcherId::Xdp(DispatcherInfo(
                p.if_index.expect("if_index should be known at this point"),
                None,
            ))),
            Program::Tc(p) => Some(DispatcherId::Tc(DispatcherInfo(
                p.if_index.expect("if_index should be known at this point"),
                Some(p.direction),
            ))),
            _ => None,
        }
    }

    pub(crate) fn data_mut(&mut self) -> Result<&mut ProgramData, BpfmanError> {
        match self {
            Program::Xdp(p) => Ok(&mut p.data),
            Program::Tracepoint(p) => Ok(&mut p.data),
            Program::Tc(p) => Ok(&mut p.data),
            Program::Kprobe(p) => Ok(&mut p.data),
            Program::Uprobe(p) => Ok(&mut p.data),
            Program::Unsupported(_) => Err(BpfmanError::Error(
                "Unsupported program type has no ProgramData".to_string(),
            )),
        }
    }

    pub(crate) fn data(&self) -> Result<&ProgramData, BpfmanError> {
        match self {
            Program::Xdp(p) => Ok(&p.data),
            Program::Tracepoint(p) => Ok(&p.data),
            Program::Tc(p) => Ok(&p.data),
            Program::Kprobe(p) => Ok(&p.data),
            Program::Uprobe(p) => Ok(&p.data),
            Program::Unsupported(_) => Err(BpfmanError::Error(
                "Unsupported program type has no ProgramData".to_string(),
            )),
        }
    }

    pub(crate) fn attached(&self) -> Option<bool> {
        match self {
            Program::Xdp(p) => Some(p.attached),
            Program::Tc(p) => Some(p.attached),
            _ => None,
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
            _ => (),
        }
    }

    pub(crate) fn kernel_info(&self) -> Option<&KernelProgramInfo> {
        match self {
            Program::Xdp(p) => p.data.kernel_info.as_ref(),
            Program::Tc(p) => p.data.kernel_info.as_ref(),
            Program::Tracepoint(p) => p.data.kernel_info.as_ref(),
            Program::Kprobe(p) => p.data.kernel_info.as_ref(),
            Program::Uprobe(p) => p.data.kernel_info.as_ref(),
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

    pub(crate) fn location(&self) -> Option<&Location> {
        match self {
            Program::Xdp(p) => Some(&p.data.location),
            Program::Tracepoint(p) => Some(&p.data.location),
            Program::Tc(p) => Some(&p.data.location),
            Program::Kprobe(p) => Some(&p.data.location),
            Program::Uprobe(p) => Some(&p.data.location),
            Program::Unsupported(_) => None,
        }
    }

    pub(crate) fn direction(&self) -> Option<Direction> {
        match self {
            Program::Tc(p) => Some(p.direction),
            _ => None,
        }
    }
}

// BpfMap represents a single map pin path used by a Program.  It has to be a
// separate object becuase it's lifetime is slightly different from a Program.
// More specifically a BpfMap can outlive a Program if other Programs are using
// it.
#[derive(Debug, Clone)]
pub(crate) struct BpfMap {
    pub(crate) used_by: Vec<u32>,
}
