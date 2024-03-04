// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Commands between the RPC thread and the BPF thread
use std::{
    collections::HashMap,
    fmt, fs,
    path::{Path, PathBuf},
    time::SystemTime,
};

use aya::programs::ProgramInfo as AyaProgInfo;
use bpfman_api::{
    util::directories::RTDIR_FS,
    v1::{
        attach_info::Info, bytecode_location::Location as V1Location, AttachInfo, BytecodeLocation,
        FentryAttachInfo, FexitAttachInfo, KernelProgramInfo as V1KernelProgramInfo,
        KprobeAttachInfo, ProgramInfo as V1ProgramInfo, TcAttachInfo, TracepointAttachInfo,
        UprobeAttachInfo, XdpAttachInfo,
    },
    ParseError, ProgramType, TcProceedOn, TcProceedOnEntry, XdpProceedOn, XdpProceedOnEntry,
};
use chrono::{prelude::DateTime, Local};
use log::{info, warn};
use rand::Rng;
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;

use crate::{
    errors::BpfmanError,
    multiprog::{DispatcherId, DispatcherInfo},
    oci_utils::image_manager::{BytecodeImage, ImageManager},
    utils::{
        bytes_to_bool, bytes_to_i32, bytes_to_string, bytes_to_u32, bytes_to_u64, bytes_to_usize,
        sled_get, sled_get_option, sled_insert,
    },
    ROOT_DB,
};

/// These constants define the key of SLED DB
pub(crate) const PROGRAM_PREFIX: &str = "program_";
pub(crate) const PROGRAM_PRE_LOAD_PREFIX: &str = "pre_load_program_";
const KIND: &str = "kind";
const NAME: &str = "name";
const ID: &str = "id";
const LOCATION_FILENAME: &str = "location_filename";
const LOCATION_IMAGE_URL: &str = "location_image_url";
const LOCATION_IMAGE_PULL_POLICY: &str = "location_image_pull_policy";
const LOCATION_USERNAME: &str = "location_username";
const LOCATION_PASSWORD: &str = "location_password";
const MAP_OWNER_ID: &str = "map_owner_id";
const MAP_PIN_PATH: &str = "map_pin_path";
const PREFIX_GLOBAL_DATA: &str = "global_data_";
const PREFIX_METADATA: &str = "metadata_";
const PREFIX_MAPS_USED_BY: &str = "maps_used_by_";
const PROGRAM_BYTES: &str = "program_bytes";

const KERNEL_NAME: &str = "kernel_name";
const KERNEL_PROGRAM_TYPE: &str = "kernel_program_type";
const KERNEL_LOADED_AT: &str = "kernel_loaded_at";
const KERNEL_TAG: &str = "kernel_tag";
const KERNEL_GPL_COMPATIBLE: &str = "kernel_gpl_compatible";
const KERNEL_BTF_ID: &str = "kernel_btf_id";
const KERNEL_BYTES_XLATED: &str = "kernel_bytes_xlated";
const KERNEL_JITED: &str = "kernel_jited";
const KERNEL_BYTES_JITED: &str = "kernel_bytes_jited";
const KERNEL_BYTES_MEMLOCK: &str = "kernel_bytes_memlock";
const KERNEL_VERIFIED_INSNS: &str = "kernel_verified_insns";
const PREFIX_KERNEL_MAP_IDS: &str = "kernel_map_ids_";

const XDP_PRIORITY: &str = "xdp_priority";
const XDP_IFACE: &str = "xdp_iface";
const XDP_CURRENT_POSITION: &str = "xdp_current_position";
const XDP_IF_INDEX: &str = "xdp_if_index";
const XDP_ATTACHED: &str = "xdp_attached";
const PREFIX_XDP_PROCEED_ON: &str = "xdp_proceed_on_";

const TC_PRIORITY: &str = "tc_priority";
const TC_IFACE: &str = "tc_iface";
const TC_CURRENT_POSITION: &str = "tc_current_position";
const TC_IF_INDEX: &str = "tc_if_index";
const TC_ATTACHED: &str = "tc_attached";
const TC_DIRECTION: &str = "tc_direction";
const PREFIX_TC_PROCEED_ON: &str = "tc_proceed_on_";

const TRACEPOINT_NAME: &str = "tracepoint_name";

const KPROBE_FN_NAME: &str = "kprobe_fn_name";
const KPROBE_OFFSET: &str = "kprobe_offset";
const KPROBE_RETPROBE: &str = "kprobe_retprobe";
const KPROBE_CONTAINER_PID: &str = "kprobe_container_pid";

const UPROBE_FN_NAME: &str = "uprobe_fn_name";
const UPROBE_OFFSET: &str = "uprobe_offset";
const UPROBE_RETPROBE: &str = "uprobe_retprobe";
const UPROBE_CONTAINER_PID: &str = "uprobe_container_pid";
const UPROBE_PID: &str = "uprobe_pid";
const UPROBE_TARGET: &str = "uprobe_target";

const FENTRY_FN_NAME: &str = "fentry_fn_name";

const FEXIT_FN_NAME: &str = "fexit_fn_name";

/// Provided by the requester and used by the manager task to send
/// the command response back to the requester.
type Responder<T> = oneshot::Sender<T>;

/// Multiple different commands are multiplexed over a single channel.
#[derive(Debug)]
pub enum Command {
    /// Load a program
    Load(LoadArgs),
    Unload(UnloadArgs),
    List {
        responder: Responder<Vec<Program>>,
        filter: ListFilter,
    },
    Get(GetArgs),
    PullBytecode(PullBytecodeArgs),
}

#[derive(Debug, Clone, Default)]
pub struct ListFilter {
    pub(crate) program_type: Option<u32>,
    pub(crate) metadata_selector: HashMap<String, String>,
    pub(crate) bpfman_programs_only: bool,
}

impl ListFilter {
    pub fn new(
        program_type: Option<u32>,
        metadata_selector: HashMap<String, String>,
        bpfman_programs_only: bool,
    ) -> Self {
        Self {
            program_type,
            metadata_selector,
            bpfman_programs_only,
        }
    }

    pub(crate) fn matches(&self, program: &Program) -> bool {
        if let Program::Unsupported(_) = program {
            if self.bpfman_programs_only {
                return false;
            }

            if let Some(prog_type) = self.program_type {
                match program.get_data().get_kernel_program_type() {
                    Ok(kernel_prog_type) => {
                        if kernel_prog_type != prog_type {
                            return false;
                        }
                    }
                    Err(e) => {
                        warn!("Failed to get kernel program type during list match: {}", e);
                        return false;
                    }
                }
            }

            // If a selector was provided, skip over non-bpfman loaded programs.
            if !self.metadata_selector.is_empty() {
                return false;
            }
        } else {
            // Program type filtering has to be done differently for bpfman owned
            // programs since XDP and TC programs have a type EXT when loaded by
            // bpfman.
            let prog_type_internal: u32 = program.kind().into();
            if let Some(prog_type) = self.program_type {
                if prog_type_internal != prog_type {
                    return false;
                }
            }
            // Filter on the input metadata field if provided
            for (key, value) in &self.metadata_selector {
                match program.get_data().get_metadata() {
                    Ok(metadata) => {
                        if let Some(v) = metadata.get(key) {
                            if *value != *v {
                                return false;
                            }
                        } else {
                            return false;
                        }
                    }
                    Err(e) => {
                        warn!("Failed to get metadata during list match: {}", e);
                        return false;
                    }
                }
            }
        }
        true
    }
}

#[derive(Debug)]
pub struct LoadArgs {
    pub(crate) program: Program,
    pub(crate) responder: Responder<Result<Program, BpfmanError>>,
}

#[derive(Debug, Clone)]
pub enum Program {
    Xdp(XdpProgram),
    Tc(TcProgram),
    Tracepoint(TracepointProgram),
    Kprobe(KprobeProgram),
    Uprobe(UprobeProgram),
    Fentry(FentryProgram),
    Fexit(FexitProgram),
    Unsupported(ProgramData),
}

#[derive(Debug)]
pub struct UnloadArgs {
    pub(crate) id: u32,
    pub(crate) responder: Responder<Result<(), BpfmanError>>,
}

#[derive(Debug)]
pub struct GetArgs {
    pub(crate) id: u32,
    pub(crate) responder: Responder<Result<Program, BpfmanError>>,
}

#[derive(Debug)]
pub struct PullBytecodeArgs {
    pub(crate) image: BytecodeImage,
    pub(crate) responder: Responder<Result<(), BpfmanError>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub enum Location {
    Image(BytecodeImage),
    File(String),
}

impl Location {
    async fn get_program_bytes(
        &self,
        image_manager: &mut ImageManager,
    ) -> Result<(Vec<u8>, String), BpfmanError> {
        match self {
            Location::File(l) => Ok((crate::utils::read(l)?, "".to_owned())),
            Location::Image(l) => {
                let (path, bpf_function_name) = image_manager
                    .get_image(
                        &l.image_url,
                        l.image_pull_policy.clone(),
                        l.username.clone(),
                        l.password.clone(),
                    )
                    .await?;
                let bytecode = image_manager.get_bytecode_from_image_store(path)?;

                Ok((bytecode, bpf_function_name))
            }
        }
    }
}

#[derive(Debug, Serialize, Hash, Deserialize, Eq, PartialEq, Copy, Clone)]
pub enum Direction {
    Ingress = 1,
    Egress = 2,
}

impl TryFrom<u32> for Direction {
    type Error = ParseError;

    fn try_from(v: u32) -> Result<Self, Self::Error> {
        match v {
            1 => Ok(Self::Ingress),
            2 => Ok(Self::Egress),
            m => Err(ParseError::InvalidDirection {
                direction: m.to_string(),
            }),
        }
    }
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
            Direction::Ingress => f.write_str("ingress"),
            Direction::Egress => f.write_str("egress"),
        }
    }
}

impl TryFrom<&Program> for V1ProgramInfo {
    type Error = BpfmanError;

    fn try_from(program: &Program) -> Result<Self, Self::Error> {
        let data: &ProgramData = program.get_data();

        let bytecode = match program.location()? {
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
        };

        let attach_info = AttachInfo {
            info: match program.clone() {
                Program::Xdp(p) => Some(Info::XdpAttachInfo(XdpAttachInfo {
                    priority: p.get_priority()?,
                    iface: p.get_iface()?.to_string(),
                    position: p.get_current_position()?.unwrap_or(0) as i32,
                    proceed_on: p.get_proceed_on()?.as_action_vec(),
                })),
                Program::Tc(p) => Some(Info::TcAttachInfo(TcAttachInfo {
                    priority: p.get_priority()?,
                    iface: p.get_iface()?.to_string(),
                    position: p.get_current_position()?.unwrap_or(0) as i32,
                    direction: p.get_direction()?.to_string(),
                    proceed_on: p.get_proceed_on()?.as_action_vec(),
                })),
                Program::Tracepoint(p) => Some(Info::TracepointAttachInfo(TracepointAttachInfo {
                    tracepoint: p.get_tracepoint()?.to_string(),
                })),
                Program::Kprobe(p) => Some(Info::KprobeAttachInfo(KprobeAttachInfo {
                    fn_name: p.get_fn_name()?.to_string(),
                    offset: p.get_offset()?,
                    retprobe: p.get_retprobe()?,
                    container_pid: p.get_container_pid()?,
                })),
                Program::Uprobe(p) => Some(Info::UprobeAttachInfo(UprobeAttachInfo {
                    fn_name: p.get_fn_name()?.map(|v| v.to_string()),
                    offset: p.get_offset()?,
                    target: p.get_target()?.to_string(),
                    retprobe: p.get_retprobe()?,
                    pid: p.get_pid()?,
                    container_pid: p.get_container_pid()?,
                })),
                Program::Fentry(p) => Some(Info::FentryAttachInfo(FentryAttachInfo {
                    fn_name: p.get_fn_name()?.to_string(),
                })),
                Program::Fexit(p) => Some(Info::FexitAttachInfo(FexitAttachInfo {
                    fn_name: p.get_fn_name()?.to_string(),
                })),
                Program::Unsupported(_) => None,
            },
        };

        // Populate the Program Info with bpfman data
        Ok(V1ProgramInfo {
            name: data.get_name()?.to_string(),
            bytecode,
            attach: Some(attach_info),
            global_data: data.get_global_data()?,
            map_owner_id: data.get_map_owner_id()?,
            map_pin_path: data
                .get_map_pin_path()?
                .map_or(String::new(), |v| v.to_str().unwrap().to_string()),
            map_used_by: data
                .get_maps_used_by()?
                .iter()
                .map(|m| m.to_string())
                .collect(),
            metadata: data.get_metadata()?,
        })
    }
}

impl TryFrom<&Program> for V1KernelProgramInfo {
    type Error = BpfmanError;

    fn try_from(program: &Program) -> Result<Self, Self::Error> {
        // Get the Kernel Info.
        let data: &ProgramData = program.get_data();

        // Populate the Kernel Info.
        Ok(V1KernelProgramInfo {
            id: data.get_id()?,
            name: data.get_kernel_name()?.to_string(),
            program_type: program.kind() as u32,
            loaded_at: data.get_kernel_loaded_at()?.to_string(),
            tag: data.get_kernel_tag()?.to_string(),
            gpl_compatible: data.get_kernel_gpl_compatible()?,
            map_ids: data.get_kernel_map_ids()?,
            btf_id: data.get_kernel_btf_id()?,
            bytes_xlated: data.get_kernel_bytes_xlated()?,
            jited: data.get_kernel_jited()?,
            bytes_jited: data.get_kernel_bytes_jited()?,
            bytes_memlock: data.get_kernel_bytes_memlock()?,
            verified_insns: data.get_kernel_verified_insns()?,
        })
    }
}

/// ProgramInfo stores information about bpf programs that are loaded and managed
/// by bpfman.
#[derive(Debug, Clone)]
pub struct ProgramData {
    // Prior to load this will be a temporary Tree with a random ID, following
    // load it will be replaced with the main program database tree.
    db_tree: sled::Tree,
}

impl ProgramData {
    pub fn new(tree: sled::Tree) -> Self {
        Self { db_tree: tree }
    }
    pub fn new_pre_load(
        location: Location,
        name: String,
        metadata: HashMap<String, String>,
        global_data: HashMap<String, Vec<u8>>,
        map_owner_id: Option<u32>,
    ) -> Result<Self, BpfmanError> {
        let mut rng = rand::thread_rng();
        let id_rand = rng.gen::<u32>();

        let db_tree = ROOT_DB
            .open_tree(PROGRAM_PRE_LOAD_PREFIX.to_string() + &id_rand.to_string())
            .expect("Unable to open program database tree");

        let mut pd = Self { db_tree };

        pd.set_id(id_rand)?;
        pd.set_location(location)?;
        pd.set_name(&name)?;
        pd.set_metadata(metadata)?;
        pd.set_global_data(global_data)?;
        if let Some(id) = map_owner_id {
            pd.set_map_owner_id(id)?;
        };

        Ok(pd)
    }

    pub(crate) fn swap_tree(&mut self, new_id: u32) -> Result<(), BpfmanError> {
        let new_tree = ROOT_DB
            .open_tree(PROGRAM_PREFIX.to_string() + &new_id.to_string())
            .expect("Unable to open program database tree");

        // Copy over all key's and values to new tree
        for r in self.db_tree.into_iter() {
            let (k, v) = r.expect("unable to iterate db_tree");
            new_tree.insert(k, v).map_err(|e| {
                BpfmanError::DatabaseError(
                    "unable to insert entry during copy".to_string(),
                    e.to_string(),
                )
            })?;
        }

        ROOT_DB
            .drop_tree(self.db_tree.name())
            .expect("unable to delete temporary program tree");

        self.db_tree = new_tree;
        self.set_id(new_id)?;

        Ok(())
    }

    /*
     * Methods for setting and getting program data for programs managed by
     * bpfman.
     */

    // A programData's kind could be different from the kernel_program_type value
    // since the TC and XDP programs loaded by bpfman will have a ProgramType::Ext
    // rather than ProgramType::Xdp or ProgramType::Tc.
    // Kind should only be set on programs loaded by bpfman.
    fn set_kind(&mut self, kind: ProgramType) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KIND,
            &(Into::<u32>::into(kind)).to_ne_bytes(),
        )
    }

    fn get_kind(&self) -> Result<Option<ProgramType>, BpfmanError> {
        sled_get_option(&self.db_tree, KIND).map(|v| v.map(|v| bytes_to_u32(v).try_into().unwrap()))
    }

    pub(crate) fn set_name(&mut self, name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, NAME, name.as_bytes())
    }

    pub(crate) fn get_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_id(&mut self, id: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, ID, &id.to_ne_bytes())
    }

    pub(crate) fn get_id(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, ID).map(bytes_to_u32)
    }

    pub(crate) fn set_location(&mut self, loc: Location) -> Result<(), BpfmanError> {
        match loc {
            Location::File(l) => sled_insert(&self.db_tree, LOCATION_FILENAME, l.as_bytes()),
            Location::Image(l) => {
                sled_insert(&self.db_tree, LOCATION_IMAGE_URL, l.image_url.as_bytes())?;
                sled_insert(
                    &self.db_tree,
                    LOCATION_IMAGE_PULL_POLICY,
                    l.image_pull_policy.to_string().as_bytes(),
                )?;
                if let Some(u) = l.username {
                    sled_insert(&self.db_tree, LOCATION_USERNAME, u.as_bytes())?;
                };

                if let Some(p) = l.password {
                    sled_insert(&self.db_tree, LOCATION_PASSWORD, p.as_bytes())?;
                };
                Ok(())
            }
        }
        .map_err(|e| {
            BpfmanError::DatabaseError(
                format!(
                    "Unable to insert location database entries into tree {:?}",
                    self.db_tree.name()
                ),
                e.to_string(),
            )
        })
    }

    pub(crate) fn get_location(&self) -> Result<Location, BpfmanError> {
        if let Ok(l) = sled_get(&self.db_tree, LOCATION_FILENAME) {
            Ok(Location::File(bytes_to_string(&l).to_string()))
        } else {
            Ok(Location::Image(BytecodeImage {
                image_url: bytes_to_string(&sled_get(&self.db_tree, LOCATION_IMAGE_URL)?)
                    .to_string(),
                image_pull_policy: bytes_to_string(&sled_get(
                    &self.db_tree,
                    LOCATION_IMAGE_PULL_POLICY,
                )?)
                .as_str()
                .try_into()
                .unwrap(),
                username: sled_get_option(&self.db_tree, LOCATION_USERNAME)?
                    .map(|v| bytes_to_string(&v)),
                password: sled_get_option(&self.db_tree, LOCATION_PASSWORD)?
                    .map(|v| bytes_to_string(&v)),
            }))
        }
    }

    pub(crate) fn set_global_data(
        &mut self,
        data: HashMap<String, Vec<u8>>,
    ) -> Result<(), BpfmanError> {
        data.iter().try_for_each(|(k, v)| {
            sled_insert(
                &self.db_tree,
                format!("{PREFIX_GLOBAL_DATA}{k}").as_str(),
                v,
            )
        })
    }

    pub(crate) fn get_global_data(&self) -> Result<HashMap<String, Vec<u8>>, BpfmanError> {
        self.db_tree
            .scan_prefix(PREFIX_GLOBAL_DATA)
            .map(|n| {
                n.map(|(k, v)| {
                    (
                        bytes_to_string(&k)
                            .strip_prefix(PREFIX_GLOBAL_DATA)
                            .unwrap()
                            .to_string(),
                        v.to_vec(),
                    )
                })
            })
            .map(|n| {
                n.map_err(|e| {
                    BpfmanError::DatabaseError(
                        "Failed to get global data".to_string(),
                        e.to_string(),
                    )
                })
            })
            .collect()
    }

    pub(crate) fn set_metadata(
        &mut self,
        data: HashMap<String, String>,
    ) -> Result<(), BpfmanError> {
        data.iter().try_for_each(|(k, v)| {
            sled_insert(
                &self.db_tree,
                format!("{PREFIX_METADATA}{k}").as_str(),
                v.as_bytes(),
            )
        })
    }

    pub(crate) fn get_metadata(&self) -> Result<HashMap<String, String>, BpfmanError> {
        self.db_tree
            .scan_prefix(PREFIX_METADATA)
            .map(|n| {
                n.map(|(k, v)| {
                    (
                        bytes_to_string(&k)
                            .strip_prefix(PREFIX_METADATA)
                            .unwrap()
                            .to_string(),
                        bytes_to_string(&v).to_string(),
                    )
                })
            })
            .map(|n| {
                n.map_err(|e| {
                    BpfmanError::DatabaseError("Failed to get metadata".to_string(), e.to_string())
                })
            })
            .collect()
    }

    pub(crate) fn set_map_owner_id(&mut self, id: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, MAP_OWNER_ID, &id.to_ne_bytes())
    }

    pub(crate) fn get_map_owner_id(&self) -> Result<Option<u32>, BpfmanError> {
        sled_get_option(&self.db_tree, MAP_OWNER_ID).map(|v| v.map(bytes_to_u32))
    }

    pub(crate) fn set_map_pin_path(&mut self, path: &Path) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            MAP_PIN_PATH,
            path.to_str().unwrap().as_bytes(),
        )
    }

    pub(crate) fn get_map_pin_path(&self) -> Result<Option<PathBuf>, BpfmanError> {
        sled_get_option(&self.db_tree, MAP_PIN_PATH)
            .map(|v| v.map(|f| PathBuf::from(bytes_to_string(&f))))
    }

    // set_maps_used_by differs from other setters in that it's explicitly idempotent.
    pub(crate) fn set_maps_used_by(&mut self, ids: Vec<u32>) -> Result<(), BpfmanError> {
        self.clear_maps_used_by();

        ids.iter().enumerate().try_for_each(|(i, v)| {
            sled_insert(
                &self.db_tree,
                format!("{PREFIX_MAPS_USED_BY}{i}").as_str(),
                &v.to_ne_bytes(),
            )
        })
    }

    pub(crate) fn get_maps_used_by(&self) -> Result<Vec<u32>, BpfmanError> {
        self.db_tree
            .scan_prefix(PREFIX_MAPS_USED_BY)
            .map(|n| n.map(|(_, v)| bytes_to_u32(v.to_vec())))
            .map(|n| {
                n.map_err(|e| {
                    BpfmanError::DatabaseError(
                        "Failed to get maps used by".to_string(),
                        e.to_string(),
                    )
                })
            })
            .collect()
    }

    pub(crate) fn clear_maps_used_by(&self) {
        self.db_tree.scan_prefix(PREFIX_MAPS_USED_BY).for_each(|n| {
            self.db_tree
                .remove(n.unwrap().0)
                .expect("unable to clear maps used by");
        });
    }

    pub(crate) fn get_program_bytes(&self) -> Result<Vec<u8>, BpfmanError> {
        sled_get(&self.db_tree, PROGRAM_BYTES)
    }

    pub(crate) async fn set_program_bytes(
        &mut self,
        image_manager: &mut ImageManager,
    ) -> Result<(), BpfmanError> {
        let loc = self.get_location()?;
        match loc.get_program_bytes(image_manager).await {
            Err(e) => Err(e),
            Ok((v, s)) => {
                match loc {
                    Location::Image(l) => {
                        info!(
                            "Loading program bytecode from container image: {}",
                            l.get_url()
                        );
                        // If program name isn't provided and we're loading from a container
                        // image use the program name provided in the image metadata, otherwise
                        // always use the provided program name.
                        let provided_name = self.get_name()?.clone();

                        if provided_name.is_empty() {
                            self.set_name(&s)?;
                        } else if s != provided_name {
                            return Err(BpfmanError::BytecodeMetaDataMismatch {
                                image_prog_name: s,
                                provided_prog_name: provided_name.to_string(),
                            });
                        }
                    }
                    Location::File(l) => {
                        info!("Loading program bytecode from file: {}", l);
                    }
                }
                sled_insert(&self.db_tree, PROGRAM_BYTES, &v)?;
                Ok(())
            }
        }
    }

    /*
     * End bpfman program info getters/setters.
     */

    /*
     * Methods for setting and getting kernel information.
     */

    pub(crate) fn get_kernel_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_NAME).map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_name(&mut self, name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, KERNEL_NAME, name.as_bytes())
    }

    pub(crate) fn get_kernel_program_type(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_PROGRAM_TYPE).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_program_type(&mut self, program_type: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_PROGRAM_TYPE,
            &program_type.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_loaded_at(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_LOADED_AT).map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_loaded_at(
        &mut self,
        loaded_at: SystemTime,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_LOADED_AT,
            DateTime::<Local>::from(loaded_at)
                .format("%Y-%m-%dT%H:%M:%S%z")
                .to_string()
                .as_bytes(),
        )
    }

    pub(crate) fn get_kernel_tag(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_TAG).map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_tag(&mut self, tag: u64) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_TAG,
            format!("{:x}", tag).as_str().as_bytes(),
        )
    }

    pub(crate) fn set_kernel_gpl_compatible(
        &mut self,
        gpl_compatible: bool,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_GPL_COMPATIBLE,
            &(gpl_compatible as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_gpl_compatible(&self) -> Result<bool, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_GPL_COMPATIBLE).map(bytes_to_bool)
    }

    pub(crate) fn get_kernel_map_ids(&self) -> Result<Vec<u32>, BpfmanError> {
        self.db_tree
            .scan_prefix(PREFIX_KERNEL_MAP_IDS.as_bytes())
            .map(|n| n.map(|(_, v)| bytes_to_u32(v.to_vec())))
            .map(|n| {
                n.map_err(|e| {
                    BpfmanError::DatabaseError("Failed to get map ids".to_string(), e.to_string())
                })
            })
            .collect()
    }

    pub(crate) fn set_kernel_map_ids(&mut self, map_ids: Vec<u32>) -> Result<(), BpfmanError> {
        let map_ids = map_ids.iter().map(|i| i.to_ne_bytes()).collect::<Vec<_>>();

        map_ids.iter().enumerate().try_for_each(|(i, v)| {
            sled_insert(
                &self.db_tree,
                format!("{PREFIX_KERNEL_MAP_IDS}{i}").as_str(),
                v,
            )
        })
    }

    pub(crate) fn get_kernel_btf_id(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BTF_ID).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_btf_id(&mut self, btf_id: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, KERNEL_BTF_ID, &btf_id.to_ne_bytes())
    }

    pub(crate) fn get_kernel_bytes_xlated(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BYTES_XLATED).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_xlated(&mut self, bytes_xlated: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_BYTES_XLATED,
            &bytes_xlated.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_jited(&self) -> Result<bool, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_JITED).map(bytes_to_bool)
    }

    pub(crate) fn set_kernel_jited(&mut self, jited: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_JITED,
            &(jited as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_bytes_jited(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BYTES_JITED).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_jited(&mut self, bytes_jited: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_BYTES_JITED,
            &bytes_jited.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_bytes_memlock(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BYTES_MEMLOCK).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_memlock(
        &mut self,
        bytes_memlock: u32,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_BYTES_MEMLOCK,
            &bytes_memlock.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_verified_insns(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_VERIFIED_INSNS).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_verified_insns(
        &mut self,
        verified_insns: u32,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_VERIFIED_INSNS,
            &verified_insns.to_ne_bytes(),
        )
    }

    pub(crate) fn set_kernel_info(&mut self, prog: &AyaProgInfo) -> Result<(), BpfmanError> {
        self.set_id(prog.id())?;
        self.set_kernel_name(
            prog.name_as_str()
                .expect("Program name is not valid unicode"),
        )?;
        self.set_kernel_program_type(prog.program_type())?;
        self.set_kernel_loaded_at(prog.loaded_at())?;
        self.set_kernel_tag(prog.tag())?;
        self.set_kernel_gpl_compatible(prog.gpl_compatible())?;
        self.set_kernel_btf_id(prog.btf_id().map_or(0, |n| n.into()))?;
        self.set_kernel_bytes_xlated(prog.size_translated())?;
        self.set_kernel_jited(prog.size_jitted() != 0)?;
        self.set_kernel_bytes_jited(prog.size_jitted())?;
        self.set_kernel_verified_insns(prog.verified_instruction_count())?;
        // Ignore errors here since it's possible the program was deleted mid
        // list, causing aya apis which make system calls using the file descriptor
        // to fail.
        if let Ok(ids) = prog.map_ids() {
            self.set_kernel_map_ids(ids)?;
        }
        if let Ok(bytes_memlock) = prog.memory_locked() {
            self.set_kernel_bytes_memlock(bytes_memlock)?;
        }

        Ok(())
    }

    /*
     * End kernel info getters/setters.
     */
}

#[derive(Debug, Clone)]
pub struct XdpProgram {
    data: ProgramData,
}

impl XdpProgram {
    pub fn new(
        data: ProgramData,
        priority: i32,
        iface: String,
        proceed_on: XdpProceedOn,
    ) -> Result<Self, BpfmanError> {
        let mut xdp_prog = Self { data };

        xdp_prog.set_priority(priority)?;
        xdp_prog.set_iface(iface)?;
        xdp_prog.set_proceed_on(proceed_on)?;
        xdp_prog.get_data_mut().set_kind(ProgramType::Xdp)?;

        Ok(xdp_prog)
    }

    pub(crate) fn set_priority(&mut self, priority: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_PRIORITY, &priority.to_ne_bytes())
    }

    pub(crate) fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, XDP_PRIORITY).map(bytes_to_i32)
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_IFACE, iface.as_bytes())
    }

    pub(crate) fn get_iface(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, XDP_IFACE).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_proceed_on(&mut self, proceed_on: XdpProceedOn) -> Result<(), BpfmanError> {
        proceed_on
            .as_action_vec()
            .iter()
            .enumerate()
            .try_for_each(|(i, v)| {
                sled_insert(
                    &self.data.db_tree,
                    format!("{PREFIX_XDP_PROCEED_ON}{i}").as_str(),
                    &v.to_ne_bytes(),
                )
            })
    }

    pub(crate) fn get_proceed_on(&self) -> Result<XdpProceedOn, BpfmanError> {
        self.data
            .db_tree
            .scan_prefix(PREFIX_XDP_PROCEED_ON)
            .map(|n| {
                n.map(|(_, v)| XdpProceedOnEntry::try_from(bytes_to_i32(v.to_vec())))
                    .unwrap()
            })
            .map(|n| {
                n.map_err(|e| {
                    BpfmanError::DatabaseError(
                        "Failed to get proceed on".to_string(),
                        e.to_string(),
                    )
                })
            })
            .collect()
    }

    pub(crate) fn set_current_position(&mut self, pos: usize) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_CURRENT_POSITION, &pos.to_ne_bytes())
    }

    pub(crate) fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_CURRENT_POSITION)?.map(bytes_to_usize))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_IF_INDEX, &if_index.to_ne_bytes())
    }

    pub(crate) fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_IF_INDEX)?.map(bytes_to_u32))
    }

    pub(crate) fn set_attached(&mut self, attached: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            XDP_ATTACHED,
            &(attached as i8).to_ne_bytes(),
        )
    }

    pub(crate) fn get_attached(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_ATTACHED)?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub struct TcProgram {
    pub(crate) data: ProgramData,
}

impl TcProgram {
    pub fn new(
        data: ProgramData,
        priority: i32,
        iface: String,
        proceed_on: TcProceedOn,
        direction: Direction,
    ) -> Result<Self, BpfmanError> {
        let mut tc_prog = Self { data };

        tc_prog.set_priority(priority)?;
        tc_prog.set_iface(iface)?;
        tc_prog.set_proceed_on(proceed_on)?;
        tc_prog.set_direction(direction)?;
        tc_prog.get_data_mut().set_kind(ProgramType::Tc)?;

        Ok(tc_prog)
    }

    pub(crate) fn set_priority(&mut self, priority: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_PRIORITY, &priority.to_ne_bytes())
    }

    pub(crate) fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, TC_PRIORITY).map(bytes_to_i32)
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_IFACE, iface.as_bytes())
    }

    pub(crate) fn get_iface(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, TC_IFACE).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_proceed_on(&mut self, proceed_on: TcProceedOn) -> Result<(), BpfmanError> {
        proceed_on
            .as_action_vec()
            .iter()
            .enumerate()
            .try_for_each(|(i, v)| {
                sled_insert(
                    &self.data.db_tree,
                    format!("{PREFIX_TC_PROCEED_ON}{i}").as_str(),
                    &v.to_ne_bytes(),
                )
            })
    }

    pub(crate) fn get_proceed_on(&self) -> Result<TcProceedOn, BpfmanError> {
        self.data
            .db_tree
            .scan_prefix(PREFIX_TC_PROCEED_ON)
            .map(|n| n.map(|(_, v)| TcProceedOnEntry::try_from(bytes_to_i32(v.to_vec())).unwrap()))
            .map(|n| {
                n.map_err(|e| {
                    BpfmanError::DatabaseError(
                        "Failed to get proceed on".to_string(),
                        e.to_string(),
                    )
                })
            })
            .collect()
    }

    pub(crate) fn set_current_position(&mut self, pos: usize) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_CURRENT_POSITION, &pos.to_ne_bytes())
    }

    pub(crate) fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TC_CURRENT_POSITION)?.map(bytes_to_usize))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_IF_INDEX, &if_index.to_ne_bytes())
    }

    pub(crate) fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TC_IF_INDEX)?.map(bytes_to_u32))
    }

    pub(crate) fn set_attached(&mut self, attached: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            TC_ATTACHED,
            &(attached as i8).to_ne_bytes(),
        )
    }

    pub(crate) fn get_attached(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TC_ATTACHED)?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_direction(&mut self, direction: Direction) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            TC_DIRECTION,
            direction.to_string().as_bytes(),
        )
    }

    pub(crate) fn get_direction(&self) -> Result<Direction, BpfmanError> {
        sled_get(&self.data.db_tree, TC_DIRECTION)
            .map(|v| bytes_to_string(&v).to_string().try_into().unwrap())
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub struct TracepointProgram {
    pub(crate) data: ProgramData,
}

impl TracepointProgram {
    pub fn new(data: ProgramData, tracepoint: String) -> Result<Self, BpfmanError> {
        let mut tp_prog = Self { data };
        tp_prog.set_tracepoint(tracepoint)?;
        tp_prog.get_data_mut().set_kind(ProgramType::Tracepoint)?;

        Ok(tp_prog)
    }

    pub(crate) fn set_tracepoint(&mut self, tracepoint: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TRACEPOINT_NAME, tracepoint.as_bytes())
    }

    pub(crate) fn get_tracepoint(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, TRACEPOINT_NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub struct KprobeProgram {
    pub(crate) data: ProgramData,
}

impl KprobeProgram {
    pub fn new(
        data: ProgramData,
        fn_name: String,
        offset: u64,
        retprobe: bool,
        container_pid: Option<i32>,
    ) -> Result<Self, BpfmanError> {
        let mut kprobe_prog = Self { data };
        kprobe_prog.set_fn_name(fn_name)?;
        kprobe_prog.set_offset(offset)?;
        kprobe_prog.set_retprobe(retprobe)?;
        kprobe_prog.get_data_mut().set_kind(ProgramType::Probe)?;
        if container_pid.is_some() {
            kprobe_prog.set_container_pid(container_pid.unwrap())?;
        }
        Ok(kprobe_prog)
    }

    pub(crate) fn set_fn_name(&mut self, fn_name: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, KPROBE_FN_NAME, fn_name.as_bytes())
    }

    pub(crate) fn get_fn_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, KPROBE_FN_NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_offset(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, KPROBE_OFFSET, &offset.to_ne_bytes())
    }

    pub(crate) fn get_offset(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, KPROBE_OFFSET).map(bytes_to_u64)
    }

    pub(crate) fn set_retprobe(&mut self, retprobe: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            KPROBE_RETPROBE,
            &(retprobe as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_retprobe(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, KPROBE_RETPROBE)?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_container_pid(&mut self, container_pid: i32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            KPROBE_CONTAINER_PID,
            &container_pid.to_ne_bytes(),
        )
    }

    pub(crate) fn get_container_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, KPROBE_CONTAINER_PID)?.map(bytes_to_i32))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub struct UprobeProgram {
    pub(crate) data: ProgramData,
}

impl UprobeProgram {
    pub fn new(
        data: ProgramData,
        fn_name: Option<String>,
        offset: u64,
        target: String,
        retprobe: bool,
        pid: Option<i32>,
        container_pid: Option<i32>,
    ) -> Result<Self, BpfmanError> {
        let mut uprobe_prog = Self { data };

        if fn_name.is_some() {
            uprobe_prog.set_fn_name(fn_name.unwrap())?;
        }

        uprobe_prog.set_offset(offset)?;
        uprobe_prog.set_retprobe(retprobe)?;
        if let Some(p) = container_pid {
            uprobe_prog.set_container_pid(p)?;
        }
        if let Some(p) = pid {
            uprobe_prog.set_pid(p)?;
        }
        uprobe_prog.set_target(target)?;
        uprobe_prog.get_data_mut().set_kind(ProgramType::Probe)?;
        Ok(uprobe_prog)
    }

    pub(crate) fn set_fn_name(&mut self, fn_name: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_FN_NAME, fn_name.as_bytes())
    }

    pub(crate) fn get_fn_name(&self) -> Result<Option<String>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_FN_NAME)?.map(|v| bytes_to_string(&v)))
    }

    pub(crate) fn set_offset(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_OFFSET, &offset.to_ne_bytes())
    }

    pub(crate) fn get_offset(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, UPROBE_OFFSET).map(bytes_to_u64)
    }

    pub(crate) fn set_retprobe(&mut self, retprobe: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            UPROBE_RETPROBE,
            &(retprobe as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_retprobe(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_RETPROBE)?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_container_pid(&mut self, container_pid: i32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            UPROBE_CONTAINER_PID,
            &container_pid.to_ne_bytes(),
        )
    }

    pub(crate) fn get_container_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_CONTAINER_PID)?.map(bytes_to_i32))
    }

    pub(crate) fn set_pid(&mut self, pid: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_PID, &pid.to_ne_bytes())
    }

    pub(crate) fn get_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_PID)?.map(bytes_to_i32))
    }

    pub(crate) fn set_target(&mut self, target: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_TARGET, target.as_bytes())
    }

    pub(crate) fn get_target(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, UPROBE_TARGET).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub struct FentryProgram {
    pub(crate) data: ProgramData,
}

impl FentryProgram {
    pub fn new(data: ProgramData, fn_name: String) -> Result<Self, BpfmanError> {
        let mut fentry_prog = Self { data };
        fentry_prog.set_fn_name(fn_name)?;
        fentry_prog.get_data_mut().set_kind(ProgramType::Tracing)?;

        Ok(fentry_prog)
    }

    pub(crate) fn set_fn_name(&mut self, fn_name: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, FENTRY_FN_NAME, fn_name.as_bytes())
    }

    pub(crate) fn get_fn_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, FENTRY_FN_NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub struct FexitProgram {
    pub(crate) data: ProgramData,
}

impl FexitProgram {
    pub fn new(data: ProgramData, fn_name: String) -> Result<Self, BpfmanError> {
        let mut fexit_prog = Self { data };
        fexit_prog.set_fn_name(fn_name)?;
        fexit_prog.get_data_mut().set_kind(ProgramType::Tracing)?;

        Ok(fexit_prog)
    }

    pub(crate) fn set_fn_name(&mut self, fn_name: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, FEXIT_FN_NAME, fn_name.as_bytes())
    }

    pub(crate) fn get_fn_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, FEXIT_FN_NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
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
            Program::Fentry(_) => ProgramType::Tracing,
            Program::Fexit(_) => ProgramType::Tracing,
            Program::Unsupported(i) => i.get_kernel_program_type().unwrap().try_into().unwrap(),
        }
    }

    pub(crate) fn dispatcher_id(&self) -> Result<Option<DispatcherId>, BpfmanError> {
        Ok(match self {
            Program::Xdp(p) => Some(DispatcherId::Xdp(DispatcherInfo(
                p.get_if_index()?
                    .expect("if_index should be known at this point"),
                None,
            ))),
            Program::Tc(p) => Some(DispatcherId::Tc(DispatcherInfo(
                p.get_if_index()?
                    .expect("if_index should be known at this point"),
                Some(p.get_direction()?),
            ))),
            _ => None,
        })
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        match self {
            Program::Xdp(p) => &mut p.data,
            Program::Tracepoint(p) => &mut p.data,
            Program::Tc(p) => &mut p.data,
            Program::Kprobe(p) => &mut p.data,
            Program::Uprobe(p) => &mut p.data,
            Program::Fentry(p) => &mut p.data,
            Program::Fexit(p) => &mut p.data,
            Program::Unsupported(p) => p,
        }
    }

    pub(crate) fn attached(&self) -> bool {
        match self {
            Program::Xdp(p) => p.get_attached().unwrap(),
            Program::Tc(p) => p.get_attached().unwrap(),
            _ => false,
        }
    }

    pub(crate) fn set_attached(&mut self) {
        match self {
            Program::Xdp(p) => p.set_attached(true).unwrap(),
            Program::Tc(p) => p.set_attached(true).unwrap(),
            _ => (),
        };
    }

    pub(crate) fn set_position(&mut self, pos: usize) -> Result<(), BpfmanError> {
        match self {
            Program::Xdp(p) => p.set_current_position(pos),
            Program::Tc(p) => p.set_current_position(pos),
            _ => Err(BpfmanError::Error(
                "cannot set position on programs other than TC or XDP".to_string(),
            )),
        }
    }

    pub fn delete(&self) -> Result<(), anyhow::Error> {
        let id = self.get_data().get_id()?;
        ROOT_DB.drop_tree(self.get_data().db_tree.name())?;

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

    pub(crate) fn if_index(&self) -> Result<Option<u32>, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_if_index(),
            Program::Tc(p) => p.get_if_index(),
            _ => Err(BpfmanError::Error(
                "cannot get if_index on programs other than TC or XDP".to_string(),
            )),
        }
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        match self {
            Program::Xdp(p) => p.set_if_index(if_index),
            Program::Tc(p) => p.set_if_index(if_index),
            _ => Err(BpfmanError::Error(
                "cannot set if_index on programs other than TC or XDP".to_string(),
            )),
        }
    }

    pub(crate) fn if_name(&self) -> Result<String, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_iface(),
            Program::Tc(p) => p.get_iface(),
            _ => Err(BpfmanError::Error(
                "cannot get interface on programs other than TC or XDP".to_string(),
            )),
        }
    }

    pub(crate) fn priority(&self) -> Result<i32, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_priority(),
            Program::Tc(p) => p.get_priority(),
            _ => Err(BpfmanError::Error(
                "cannot get priority on programs other than TC or XDP".to_string(),
            )),
        }
    }

    pub(crate) fn location(&self) -> Result<Location, BpfmanError> {
        match self {
            Program::Xdp(p) => p.data.get_location(),
            Program::Tracepoint(p) => p.data.get_location(),
            Program::Tc(p) => p.data.get_location(),
            Program::Kprobe(p) => p.data.get_location(),
            Program::Uprobe(p) => p.data.get_location(),
            Program::Fentry(p) => p.data.get_location(),
            Program::Fexit(p) => p.data.get_location(),
            Program::Unsupported(_) => Err(BpfmanError::Error(
                "cannot get location for unsupported programs".to_string(),
            )),
        }
    }

    pub(crate) fn direction(&self) -> Result<Option<Direction>, BpfmanError> {
        match self {
            Program::Tc(p) => Ok(Some(p.get_direction()?)),
            _ => Ok(None),
        }
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        match self {
            Program::Xdp(p) => p.get_data(),
            Program::Tracepoint(p) => p.get_data(),
            Program::Tc(p) => p.get_data(),
            Program::Kprobe(p) => p.get_data(),
            Program::Uprobe(p) => p.get_data(),
            Program::Fentry(p) => p.get_data(),
            Program::Fexit(p) => p.get_data(),
            Program::Unsupported(p) => p,
        }
    }

    pub(crate) fn new_from_db(id: u32, tree: sled::Tree) -> Result<Self, BpfmanError> {
        let data = ProgramData::new(tree);

        if data.get_id()? != id {
            return Err(BpfmanError::Error(
                "Program id does not match database id program isn't fully loaded".to_string(),
            ));
        }
        match data.get_kind()? {
            Some(p) => match p {
                ProgramType::Xdp => Ok(Program::Xdp(XdpProgram { data })),
                ProgramType::Tc => Ok(Program::Tc(TcProgram { data })),
                ProgramType::Tracepoint => Ok(Program::Tracepoint(TracepointProgram { data })),
                // kernel does not distinguish between kprobe and uprobe program types
                ProgramType::Probe => {
                    if data.db_tree.get(UPROBE_OFFSET).unwrap().is_some() {
                        Ok(Program::Uprobe(UprobeProgram { data }))
                    } else {
                        Ok(Program::Kprobe(KprobeProgram { data }))
                    }
                }
                // kernel does not distinguish between fentry and fexit program types
                ProgramType::Tracing => {
                    if data.db_tree.get(FENTRY_FN_NAME).unwrap().is_some() {
                        Ok(Program::Fentry(FentryProgram { data }))
                    } else {
                        Ok(Program::Fexit(FexitProgram { data }))
                    }
                }
                _ => Err(BpfmanError::Error("Unsupported program type".to_string())),
            },
            None => Err(BpfmanError::Error("Unsupported program type".to_string())),
        }
    }
}
