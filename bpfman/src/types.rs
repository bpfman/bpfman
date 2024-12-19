// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Commands between the RPC thread and the BPF thread
use std::{
    collections::HashMap,
    ffi::OsStr,
    fmt, fs,
    os::unix::ffi::OsStrExt,
    path::{Path, PathBuf},
    time::SystemTime,
};

use aya::{
    maps::MapType as AyaMapType,
    programs::{
        LinkOrder as AyaLinkOrder, ProgramId, ProgramInfo as AyaProgInfo,
        ProgramType as AyaProgramType,
    },
};
use chrono::{prelude::DateTime, Local};
use clap::ValueEnum;
use log::{info, warn};
use rand::Rng;
use serde::{Deserialize, Serialize};
use sled::Db;

use crate::{
    directories::RTDIR_FS,
    errors::{BpfmanError, ParseError},
    multiprog::{DispatcherId, DispatcherInfo},
    oci_utils::image_manager::ImageManager,
    utils::{
        bytes_to_bool, bytes_to_i32, bytes_to_string, bytes_to_u32, bytes_to_u64, bytes_to_usize,
        nsid, sled_get, sled_get_option, sled_insert,
    },
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
const XDP_NETNS: &str = "xdp_netns";
const XDP_NSID: &str = "xdp_nsid";

const TC_PRIORITY: &str = "tc_priority";
const TC_IFACE: &str = "tc_iface";
const TC_CURRENT_POSITION: &str = "tc_current_position";
const TC_IF_INDEX: &str = "tc_if_index";
const TC_ATTACHED: &str = "tc_attached";
const TC_DIRECTION: &str = "tc_direction";
const PREFIX_TC_PROCEED_ON: &str = "tc_proceed_on_";
const TC_NETNS: &str = "tc_netns";
const TC_NSID: &str = "tc_nsid";

const TCX_PRIORITY: &str = "tcx_priority";
const TCX_IFACE: &str = "tcx_iface";
const TCX_CURRENT_POSITION: &str = "tcx_current_position";
const TCX_IF_INDEX: &str = "tcx_if_index";
const TCX_DIRECTION: &str = "tcx_direction";
const TCX_NETNS: &str = "tcx_netns";
const TCX_NSID: &str = "tcx_nsid";

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

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct BytecodeImage {
    pub image_url: String,
    pub image_pull_policy: ImagePullPolicy,
    pub username: Option<String>,
    pub password: Option<String>,
}

impl BytecodeImage {
    pub fn new(
        image_url: String,
        image_pull_policy: i32,
        username: Option<String>,
        password: Option<String>,
    ) -> Self {
        Self {
            image_url,
            image_pull_policy: image_pull_policy
                .try_into()
                .expect("Unable to parse ImagePullPolicy"),
            username,
            password,
        }
    }

    pub fn get_url(&self) -> &str {
        &self.image_url
    }

    pub fn get_pull_policy(&self) -> &ImagePullPolicy {
        &self.image_pull_policy
    }
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

/// `Program` represents various types of eBPF programs that are
/// supported by bpfman.
#[derive(Debug, Clone)]
pub enum Program {
    /// An XDP (Express Data Path) program.
    ///
    /// XDP programs are attached to network interfaces and can
    /// process packets at a very early stage in the network stack,
    /// providing high-performance packet processing.
    Xdp(XdpProgram),

    /// A TC (Traffic Control) program.
    ///
    /// TC programs are used for controlling network traffic. They can
    /// be attached to various hooks in the Linux Traffic Control (tc)
    /// subsystem.
    Tc(TcProgram),

    /// A TCX (Traffic Control) program.
    ///
    /// TCX programs are similar to TC programs, and are used for controlling
    /// network traffic. They can be attached to the TCX hook point, which
    /// executes before any TC programs on the same hook point.
    Tcx(TcxProgram),

    /// A Tracepoint program.
    ///
    /// Tracepoint programs are used for tracing specific events in
    /// the kernel, providing insights into kernel behaviour and
    /// performance.
    Tracepoint(TracepointProgram),

    /// A Kprobe (Kernel Probe) program.
    ///
    /// Kprobe programs are used to dynamically trace and instrument
    /// kernel functions. They can be attached to almost any function
    /// in the kernel.
    Kprobe(KprobeProgram),

    /// A Uprobe (User-space Probe) program.
    ///
    /// Uprobe programs are similar to Kprobe programs but are used to
    /// trace user-space applications. They can be attached to
    /// functions in user-space binaries.
    Uprobe(UprobeProgram),

    /// An Fentry (Function Entry) program.
    ///
    /// Fentry programs are a type of BPF program that are attached to
    /// the entry points of functions, providing a mechanism to trace
    /// and instrument the beginning of function execution.
    Fentry(FentryProgram),

    /// An Fexit (Function Exit) program.
    ///
    /// Fexit programs are a type of BPF program that are attached to
    /// the exit points of functions, providing a mechanism to trace
    /// and instrument the end of function execution.
    Fexit(FexitProgram),

    /// An unsupported BPF program type.
    ///
    /// This variant is used to represent BPF programs that are not
    /// supported by bpfman. It contains the raw `ProgramData` for the
    /// unsupported program.
    Unsupported(ProgramData),
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub enum Location {
    Image(BytecodeImage),
    File(String),
}

impl Location {
    async fn get_program_bytes(
        &self,
        root_db: &Db,
        image_manager: &mut ImageManager,
    ) -> Result<(Vec<u8>, Vec<String>), BpfmanError> {
        match self {
            Location::File(l) => Ok((crate::utils::read(l)?, Vec::new())),
            Location::Image(l) => {
                let (path, bpf_function_names) = image_manager
                    .get_image(
                        root_db,
                        &l.image_url,
                        l.image_pull_policy.clone(),
                        l.username.clone(),
                        l.password.clone(),
                    )
                    .await?;
                let bytecode = image_manager.get_bytecode_from_image_store(root_db, path)?;

                Ok((bytecode, bpf_function_names))
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

/// ProgramData stores information about bpf programs that are loaded and managed
/// by bpfman.
#[derive(Debug, Clone)]
pub struct ProgramData {
    // Prior to load this will be a temporary Tree with a random ID, following
    // load it will be replaced with the main program database tree.
    db_tree: sled::Tree,
}

impl ProgramData {
    /// Creates a new `ProgramData` instance.
    ///
    /// # Arguments
    ///
    /// * `location` - The location of the BPF program (file or image).
    /// * `name` - The name of the BPF program.
    /// * `metadata` - Metadata associated with the BPF program.
    /// * `global_data` - Global data required by the BPF program.
    /// * `map_owner_id` - Optional owner ID of the map.
    ///
    /// # Returns
    ///
    /// Returns `Result<Self, BpfmanError>` - An instance of `ProgramData` or a `BpfmanError`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - The temporary database cannot be opened.
    /// - The program database tree cannot be opened.
    /// - Any of the subsequent setting operations fail (ID, location, name, metadata, global data, map owner ID).
    ///
    /// # Example
    ///
    /// ```rust
    /// use bpfman::types::{Location, ProgramData};
    /// use bpfman::errors::BpfmanError;
    /// use std::collections::HashMap;
    ///
    /// fn main() -> Result<(), BpfmanError> {
    ///     let location = Location::File(String::from("kprobe.o"));
    ///     let metadata = HashMap::new();
    ///     let global_data = HashMap::new();
    ///     let map_owner_id = None;
    ///     let program_data = ProgramData::new(
    ///         location,
    ///         String::from("kprobe_do_sys_open"),
    ///         metadata,
    ///         global_data,
    ///         map_owner_id
    ///     )?;
    ///     println!("program_data: {:?}", program_data);
    ///     Ok(())
    /// }
    /// ```
    pub fn new(
        location: Location,
        name: String,
        metadata: HashMap<String, String>,
        global_data: HashMap<String, Vec<u8>>,
        map_owner_id: Option<u32>,
    ) -> Result<Self, BpfmanError> {
        let db = sled::Config::default()
            .temporary(true)
            .open()
            .expect("unable to open temporary database");

        let mut rng = rand::thread_rng();
        let id_rand = rng.gen::<u32>();

        let db_tree = db
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

    pub(crate) fn new_empty(tree: sled::Tree) -> Self {
        Self { db_tree: tree }
    }
    pub(crate) fn load(&mut self, root_db: &Db) -> Result<(), BpfmanError> {
        let db_tree = root_db
            .open_tree(self.db_tree.name())
            .expect("Unable to open program database tree");

        // Copy over all key's and values to persistent tree
        for r in self.db_tree.into_iter() {
            let (k, v) = r.expect("unable to iterate db_tree");
            db_tree.insert(k, v).map_err(|e| {
                BpfmanError::DatabaseError(
                    "unable to insert entry during copy".to_string(),
                    e.to_string(),
                )
            })?;
        }

        self.db_tree = db_tree;

        Ok(())
    }

    pub(crate) fn swap_tree(&mut self, root_db: &Db, new_id: u32) -> Result<(), BpfmanError> {
        let new_tree = root_db
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

        root_db
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

    /// Retrieves the kind of program, which is represented by the
    /// [`ProgramType`] structure.
    ///
    /// # Returns
    ///
    /// Returns `Result<Option<ProgramType>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kind from the database.
    pub fn get_kind(&self) -> Result<Option<ProgramType>, BpfmanError> {
        sled_get_option(&self.db_tree, KIND).map(|v| v.map(|v| bytes_to_u32(v).try_into().unwrap()))
    }

    pub(crate) fn set_name(&mut self, name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, NAME, name.as_bytes())
    }

    /// Retrieves the name of the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<String, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the name from the database.
    pub fn get_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_id(&mut self, id: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, ID, &id.to_ne_bytes())
    }

    /// Retrieves the kernel ID of the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the ID from the database.
    pub fn get_id(&self) -> Result<u32, BpfmanError> {
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

    /// Retrieves the location of the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<Location, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the location from the database.
    pub fn get_location(&self) -> Result<Location, BpfmanError> {
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

    /// Retrieves the global data of the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<HashMap<String, Vec<u8>>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the global data from the database.
    pub fn get_global_data(&self) -> Result<HashMap<String, Vec<u8>>, BpfmanError> {
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

    /// Retrieves the metadata of the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<HashMap<String, String>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the metadata from the database.
    pub fn get_metadata(&self) -> Result<HashMap<String, String>, BpfmanError> {
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

    /// Retrieves the owner ID of the map.
    ///
    /// # Returns
    ///
    /// Returns `Result<Option<u32>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the map owner ID from the database.
    pub fn get_map_owner_id(&self) -> Result<Option<u32>, BpfmanError> {
        sled_get_option(&self.db_tree, MAP_OWNER_ID).map(|v| v.map(bytes_to_u32))
    }

    pub(crate) fn set_map_pin_path(&mut self, path: &Path) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            MAP_PIN_PATH,
            path.to_str().unwrap().as_bytes(),
        )
    }

    /// Retrieves the map pin path.
    ///
    /// # Returns
    ///
    /// Returns `Result<Option<PathBuf>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the map pin path from the database.
    pub fn get_map_pin_path(&self) -> Result<Option<PathBuf>, BpfmanError> {
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

    /// Retrieves the IDs of maps used by the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<Vec<u32>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the maps used by from the database.
    pub fn get_maps_used_by(&self) -> Result<Vec<u32>, BpfmanError> {
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
        root_db: &Db,
        image_manager: &mut ImageManager,
    ) -> Result<(), BpfmanError> {
        let loc = self.get_location()?;
        match loc.get_program_bytes(root_db, image_manager).await {
            Err(e) => Err(e),
            Ok((v, s)) => {
                match loc {
                    Location::Image(l) => {
                        info!(
                            "Loading program bytecode from container image: {}",
                            l.get_url()
                        );

                        // Error out if the bytecode image doesn't contain the expected program.
                        let provided_name = self.get_name()?.clone();
                        if s.contains(&provided_name) {
                            self.set_name(&provided_name)?;
                        } else {
                            return Err(BpfmanError::ProgramNotFoundInBytecode {
                                bytecode_image: l.image_url,
                                expected_prog_name: provided_name,
                                program_names: s,
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

    /// Retrieves the name of the program.
    ///
    /// # Returns
    ///
    /// Returns `Result<String, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel name from the database.
    pub fn get_kernel_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_NAME).map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_name(&mut self, name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, KERNEL_NAME, name.as_bytes())
    }

    /// Retrieves the kernel program type.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel program type from the database.
    pub fn get_kernel_program_type(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_PROGRAM_TYPE).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_program_type(&mut self, program_type: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_PROGRAM_TYPE,
            &program_type.to_ne_bytes(),
        )
    }

    /// Retrieves the kernel loaded timestamp.
    ///
    /// # Returns
    ///
    /// Returns `Result<String, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel loaded timestamp from the database.
    pub fn get_kernel_loaded_at(&self) -> Result<String, BpfmanError> {
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

    /// Retrieves the kernel tag.
    ///
    /// # Returns
    ///
    /// Returns `Result<String, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel tag from the database.
    pub fn get_kernel_tag(&self) -> Result<String, BpfmanError> {
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

    /// Retrieves whether the kernel is GPL compatible.
    ///
    /// # Returns
    ///
    /// Returns `Result<bool, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel GPL compatibility status from the database.
    pub fn get_kernel_gpl_compatible(&self) -> Result<bool, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_GPL_COMPATIBLE).map(bytes_to_bool)
    }

    /// Retrieves the IDs of kernel maps.
    ///
    /// # Returns
    ///
    /// Returns `Result<Vec<u32>, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel map IDs from the database.
    pub fn get_kernel_map_ids(&self) -> Result<Vec<u32>, BpfmanError> {
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

    /// Retrieves the BTF ID of the kernel.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel BTF ID from the database.
    pub fn get_kernel_btf_id(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BTF_ID).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_btf_id(&mut self, btf_id: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, KERNEL_BTF_ID, &btf_id.to_ne_bytes())
    }

    /// Retrieves the translated bytes of the kernel program.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel translated bytes from the database.
    pub fn get_kernel_bytes_xlated(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BYTES_XLATED).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_xlated(&mut self, bytes_xlated: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_BYTES_XLATED,
            &bytes_xlated.to_ne_bytes(),
        )
    }

    /// Retrieves whether the kernel program is JIT compiled.
    ///
    /// # Returns
    ///
    /// Returns `Result<bool, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel JIT status from the database.
    pub fn get_kernel_jited(&self) -> Result<bool, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_JITED).map(bytes_to_bool)
    }

    pub(crate) fn set_kernel_jited(&mut self, jited: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_JITED,
            &(jited as i8 % 2).to_ne_bytes(),
        )
    }

    /// Retrieves the JIT compiled bytes of the kernel program.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel JIT compiled bytes from the database.
    pub fn get_kernel_bytes_jited(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, KERNEL_BYTES_JITED).map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_jited(&mut self, bytes_jited: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            KERNEL_BYTES_JITED,
            &bytes_jited.to_ne_bytes(),
        )
    }

    /// Retrieves the memory lock bytes of the kernel program.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel memory lock bytes from the database.
    pub fn get_kernel_bytes_memlock(&self) -> Result<u32, BpfmanError> {
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

    /// Retrieves the number of verified instructions of the kernel program.
    ///
    /// # Returns
    ///
    /// Returns `Result<u32, BpfmanError>`.
    ///
    /// # Errors
    ///
    /// This function will return an error if:
    /// - There is an issue fetching the kernel verified instructions count from the database.
    pub fn get_kernel_verified_insns(&self) -> Result<u32, BpfmanError> {
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
        self.set_kernel_name(prog.name_as_str().unwrap_or("None"))?;
        self.set_kernel_program_type(u32::from(ProgramType::from(
            prog.program_type().unwrap_or(AyaProgramType::Unspecified),
        )))?;
        self.set_kernel_loaded_at(prog.loaded_at().unwrap_or(SystemTime::UNIX_EPOCH))?;
        self.set_kernel_tag(prog.tag())?;
        self.set_kernel_gpl_compatible(prog.gpl_compatible().unwrap_or(false))?;
        self.set_kernel_btf_id(prog.btf_id().unwrap_or(0))?;
        self.set_kernel_bytes_xlated(prog.size_translated().unwrap_or(0))?;
        self.set_kernel_jited(prog.size_jitted() > 0)?;
        self.set_kernel_bytes_jited(prog.size_jitted())?;
        self.set_kernel_verified_insns(prog.verified_instruction_count().unwrap_or(0))?;
        // Ignore errors here since it's possible the program was deleted mid
        // list, causing aya apis which make system calls using the file descriptor
        // to fail.
        self.set_kernel_map_ids(prog.map_ids().unwrap_or(None).unwrap_or_default())?;
        self.set_kernel_bytes_memlock(prog.memory_locked().unwrap_or(0))?;

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
        netns: Option<PathBuf>,
    ) -> Result<Self, BpfmanError> {
        let mut xdp_prog = Self { data };

        xdp_prog.set_priority(priority)?;
        xdp_prog.set_iface(iface)?;
        xdp_prog.set_proceed_on(proceed_on)?;
        xdp_prog.set_nsid(nsid(netns.clone())?)?;
        if let Some(n) = netns {
            xdp_prog.set_netns(n)?;
        }
        xdp_prog.get_data_mut().set_kind(ProgramType::Xdp)?;

        Ok(xdp_prog)
    }

    pub(crate) fn set_priority(&mut self, priority: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_PRIORITY, &priority.to_ne_bytes())
    }

    pub fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, XDP_PRIORITY).map(bytes_to_i32)
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_IFACE, iface.as_bytes())
    }

    pub fn get_iface(&self) -> Result<String, BpfmanError> {
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

    pub fn get_proceed_on(&self) -> Result<XdpProceedOn, BpfmanError> {
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

    pub fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_CURRENT_POSITION)?.map(bytes_to_usize))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_IF_INDEX, &if_index.to_ne_bytes())
    }

    pub fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_IF_INDEX)?.map(bytes_to_u32))
    }

    pub(crate) fn set_attached(&mut self, attached: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            XDP_ATTACHED,
            &(attached as i8).to_ne_bytes(),
        )
    }

    pub fn get_attached(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_ATTACHED)?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_netns(&mut self, netns: PathBuf) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_NETNS, netns.as_os_str().as_bytes())
    }

    pub fn get_netns(&self) -> Result<Option<PathBuf>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, XDP_NETNS)?
            .map(|v| PathBuf::from(OsStr::from_bytes(&v))))
    }

    pub(crate) fn set_nsid(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, XDP_NSID, &offset.to_ne_bytes())
    }

    pub fn get_nsid(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, XDP_NSID).map(bytes_to_u64)
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
        netns: Option<PathBuf>,
    ) -> Result<Self, BpfmanError> {
        let mut tc_prog = Self { data };

        tc_prog.set_priority(priority)?;
        tc_prog.set_iface(iface)?;
        tc_prog.set_proceed_on(proceed_on)?;
        tc_prog.set_direction(direction)?;
        tc_prog.set_nsid(nsid(netns.clone())?)?;
        if let Some(n) = netns {
            tc_prog.set_netns(n)?;
        }
        tc_prog.get_data_mut().set_kind(ProgramType::Tc)?;

        Ok(tc_prog)
    }

    pub(crate) fn set_priority(&mut self, priority: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_PRIORITY, &priority.to_ne_bytes())
    }

    pub fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, TC_PRIORITY).map(bytes_to_i32)
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_IFACE, iface.as_bytes())
    }

    pub fn get_iface(&self) -> Result<String, BpfmanError> {
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

    pub fn get_proceed_on(&self) -> Result<TcProceedOn, BpfmanError> {
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

    pub fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TC_CURRENT_POSITION)?.map(bytes_to_usize))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_IF_INDEX, &if_index.to_ne_bytes())
    }

    pub fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TC_IF_INDEX)?.map(bytes_to_u32))
    }

    pub(crate) fn set_attached(&mut self, attached: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            TC_ATTACHED,
            &(attached as i8).to_ne_bytes(),
        )
    }

    pub fn get_attached(&self) -> Result<bool, BpfmanError> {
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

    pub fn get_direction(&self) -> Result<Direction, BpfmanError> {
        sled_get(&self.data.db_tree, TC_DIRECTION)
            .map(|v| bytes_to_string(&v).to_string().try_into().unwrap())
    }

    pub(crate) fn set_netns(&mut self, netns: PathBuf) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_NETNS, netns.as_os_str().as_bytes())
    }

    pub fn get_netns(&self) -> Result<Option<PathBuf>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TC_NETNS)?
            .map(|v| PathBuf::from(OsStr::from_bytes(&v))))
    }

    pub(crate) fn set_nsid(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TC_NSID, &offset.to_ne_bytes())
    }

    pub fn get_nsid(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, TC_NSID).map(bytes_to_u64)
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug)]
pub enum AttachOrder {
    First,
    Last,
    Before(u32),
    After(u32),
}

impl From<AttachOrder> for AyaLinkOrder {
    fn from(v: AttachOrder) -> Self {
        match v {
            AttachOrder::First => AyaLinkOrder::first(),
            AttachOrder::Last => AyaLinkOrder::last(),
            AttachOrder::Before(id) => {
                AyaLinkOrder::before_program_id(unsafe { ProgramId::new(id) })
            }
            AttachOrder::After(id) => AyaLinkOrder::after_program_id(unsafe { ProgramId::new(id) }),
        }
    }
}

#[derive(Debug, Clone)]
pub struct TcxProgram {
    pub(crate) data: ProgramData,
}

impl TcxProgram {
    pub fn new(
        data: ProgramData,
        priority: i32,
        iface: String,
        direction: Direction,
        netns: Option<PathBuf>,
    ) -> Result<Self, BpfmanError> {
        let mut tcx_prog = Self { data };

        tcx_prog.set_priority(priority)?;
        tcx_prog.set_iface(iface)?;
        tcx_prog.set_direction(direction)?;
        tcx_prog.set_nsid(nsid(netns.clone())?)?;
        if let Some(n) = netns {
            tcx_prog.set_netns(n)?;
        }
        tcx_prog.get_data_mut().set_kind(ProgramType::Tc)?;

        Ok(tcx_prog)
    }

    pub(crate) fn set_priority(&mut self, priority: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TCX_PRIORITY, &priority.to_ne_bytes())
    }

    pub fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, TCX_PRIORITY).map(bytes_to_i32)
    }

    pub(crate) fn set_current_position(&mut self, pos: usize) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TCX_CURRENT_POSITION, &pos.to_ne_bytes())
    }

    pub fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TCX_CURRENT_POSITION)?.map(bytes_to_usize))
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TCX_IFACE, iface.as_bytes())
    }

    pub fn get_iface(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, TCX_IFACE).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TCX_IF_INDEX, &if_index.to_ne_bytes())
    }

    pub fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TCX_IF_INDEX)?.map(bytes_to_u32))
    }

    pub(crate) fn set_direction(&mut self, direction: Direction) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            TCX_DIRECTION,
            direction.to_string().as_bytes(),
        )
    }

    pub fn get_direction(&self) -> Result<Direction, BpfmanError> {
        sled_get(&self.data.db_tree, TCX_DIRECTION).and_then(|v| {
            bytes_to_string(&v)
                .to_string()
                .try_into()
                .map_err(BpfmanError::BpfParseError)
        })
    }

    pub(crate) fn set_netns(&mut self, netns: PathBuf) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TCX_NETNS, netns.as_os_str().as_bytes())
    }

    pub fn get_netns(&self) -> Result<Option<PathBuf>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, TCX_NETNS)?
            .map(|v| PathBuf::from(OsStr::from_bytes(&v))))
    }

    pub(crate) fn set_nsid(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, TCX_NSID, &offset.to_ne_bytes())
    }

    pub fn get_nsid(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, TCX_NSID).map(bytes_to_u64)
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

    pub fn get_tracepoint(&self) -> Result<String, BpfmanError> {
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

    pub fn get_fn_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, KPROBE_FN_NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_offset(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, KPROBE_OFFSET, &offset.to_ne_bytes())
    }

    pub fn get_offset(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, KPROBE_OFFSET).map(bytes_to_u64)
    }

    pub(crate) fn set_retprobe(&mut self, retprobe: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            KPROBE_RETPROBE,
            &(retprobe as i8 % 2).to_ne_bytes(),
        )
    }

    pub fn get_retprobe(&self) -> Result<bool, BpfmanError> {
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

    pub fn get_container_pid(&self) -> Result<Option<i32>, BpfmanError> {
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

    pub fn get_fn_name(&self) -> Result<Option<String>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_FN_NAME)?.map(|v| bytes_to_string(&v)))
    }

    pub(crate) fn set_offset(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_OFFSET, &offset.to_ne_bytes())
    }

    pub fn get_offset(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, UPROBE_OFFSET).map(bytes_to_u64)
    }

    pub(crate) fn set_retprobe(&mut self, retprobe: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            UPROBE_RETPROBE,
            &(retprobe as i8 % 2).to_ne_bytes(),
        )
    }

    pub fn get_retprobe(&self) -> Result<bool, BpfmanError> {
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

    pub fn get_container_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_CONTAINER_PID)?.map(bytes_to_i32))
    }

    pub(crate) fn set_pid(&mut self, pid: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_PID, &pid.to_ne_bytes())
    }

    pub fn get_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, UPROBE_PID)?.map(bytes_to_i32))
    }

    pub(crate) fn set_target(&mut self, target: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, UPROBE_TARGET, target.as_bytes())
    }

    pub fn get_target(&self) -> Result<String, BpfmanError> {
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

    pub fn get_fn_name(&self) -> Result<String, BpfmanError> {
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

    pub fn get_fn_name(&self) -> Result<String, BpfmanError> {
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
    pub fn kind(&self) -> ProgramType {
        match self {
            Program::Xdp(_) => ProgramType::Xdp,
            Program::Tc(_) => ProgramType::Tc,
            Program::Tcx(_) => ProgramType::Tc,
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
                p.get_nsid()?,
                p.get_if_index()?
                    .expect("if_index should be known at this point"),
                None,
            ))),
            Program::Tc(p) => Some(DispatcherId::Tc(DispatcherInfo(
                p.get_nsid()?,
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
            Program::Tcx(p) => &mut p.data,
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
            Program::Tcx(p) => p.set_current_position(pos),
            _ => Err(BpfmanError::Error(
                "cannot set position on programs other than TC or XDP".to_string(),
            )),
        }
    }

    pub(crate) fn delete(&self, root_db: &Db) -> Result<(), anyhow::Error> {
        let id = self.get_data().get_id()?;
        root_db.drop_tree(self.get_data().db_tree.name())?;

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
            Program::Tcx(p) => p.get_if_index(),
            _ => Err(BpfmanError::Error(
                "cannot get if_index on programs other than TC, TCX and XDP".to_string(),
            )),
        }
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        match self {
            Program::Xdp(p) => p.set_if_index(if_index),
            Program::Tc(p) => p.set_if_index(if_index),
            Program::Tcx(p) => p.set_if_index(if_index),
            _ => Err(BpfmanError::Error(
                "cannot set if_index on programs other than TC, TCX and XDP".to_string(),
            )),
        }
    }

    pub(crate) fn if_name(&self) -> Result<String, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_iface(),
            Program::Tc(p) => p.get_iface(),
            Program::Tcx(p) => p.get_iface(),
            _ => Err(BpfmanError::Error(
                "cannot get interface on programs other than TC, TCX and XDP".to_string(),
            )),
        }
    }

    pub(crate) fn priority(&self) -> Result<i32, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_priority(),
            Program::Tc(p) => p.get_priority(),
            Program::Tcx(p) => p.get_priority(),
            _ => Err(BpfmanError::Error(
                "cannot get priority on programs other than TC, TCX and XDP".to_string(),
            )),
        }
    }

    pub(crate) fn direction(&self) -> Result<Option<Direction>, BpfmanError> {
        match self {
            Program::Tc(p) => Ok(Some(p.get_direction()?)),
            Program::Tcx(p) => Ok(Some(p.get_direction()?)),
            _ => Ok(None),
        }
    }

    pub(crate) fn netns(&self) -> Result<Option<PathBuf>, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_netns(),
            Program::Tc(p) => p.get_netns(),
            Program::Tcx(p) => p.get_netns(),
            _ => Err(BpfmanError::Error(
                "cannot get netns on programs other than TC, TCX and XDP".to_string(),
            )),
        }
    }

    pub(crate) fn nsid(&self) -> Result<u64, BpfmanError> {
        match self {
            Program::Xdp(p) => p.get_nsid(),
            Program::Tc(p) => p.get_nsid(),
            Program::Tcx(p) => p.get_nsid(),
            _ => Err(BpfmanError::Error(
                "cannot get netns on programs other than TC, TCX and XDP".to_string(),
            )),
        }
    }

    pub fn get_data(&self) -> &ProgramData {
        match self {
            Program::Xdp(p) => p.get_data(),
            Program::Tracepoint(p) => p.get_data(),
            Program::Tc(p) => p.get_data(),
            Program::Tcx(p) => p.get_data(),
            Program::Kprobe(p) => p.get_data(),
            Program::Uprobe(p) => p.get_data(),
            Program::Fentry(p) => p.get_data(),
            Program::Fexit(p) => p.get_data(),
            Program::Unsupported(p) => p,
        }
    }

    pub(crate) fn new_from_db(id: u32, tree: sled::Tree) -> Result<Self, BpfmanError> {
        let data = ProgramData::new_empty(tree);

        if data.get_id()? != id {
            return Err(BpfmanError::Error(
                "Program id does not match database id program isn't fully loaded".to_string(),
            ));
        }
        match data.get_kind()? {
            Some(p) => match p {
                ProgramType::Xdp => Ok(Program::Xdp(XdpProgram { data })),
                // We save the type that the kernel uses, and the kernel uses
                // the same type for both tc and tcx program types.  As a
                // result, we use the following hack to figure out which one it
                // really is.
                ProgramType::Tc => {
                    let iface = data.db_tree.get(TCX_IFACE).map_err(|e| {
                        BpfmanError::DatabaseError(
                            "Failed to get TCX_IFACE".to_string(),
                            e.to_string(),
                        )
                    })?;
                    if iface.is_some() {
                        Ok(Program::Tcx(TcxProgram { data }))
                    } else {
                        Ok(Program::Tc(TcProgram { data }))
                    }
                }
                ProgramType::Tracepoint => Ok(Program::Tracepoint(TracepointProgram { data })),
                // kernel does not distinguish between kprobe and uprobe program types
                ProgramType::Probe => {
                    let offset = data.db_tree.get(UPROBE_OFFSET).map_err(|e| {
                        BpfmanError::DatabaseError(
                            "Failed to get UPROBE_OFFSET".to_string(),
                            e.to_string(),
                        )
                    })?;
                    if offset.is_some() {
                        Ok(Program::Uprobe(UprobeProgram { data }))
                    } else {
                        Ok(Program::Kprobe(KprobeProgram { data }))
                    }
                }
                // kernel does not distinguish between fentry and fexit program types
                ProgramType::Tracing => {
                    let fn_name = data.db_tree.get(FENTRY_FN_NAME).map_err(|e| {
                        BpfmanError::DatabaseError(
                            "Failed to get FENTRY_FN_NAME".to_string(),
                            e.to_string(),
                        )
                    })?;
                    if fn_name.is_some() {
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

/// MapType must match the the bpf_map_type enum defined in the linux kernel.
/// <https://elixir.bootlin.com/linux/v6.9.5/source/include/uapi/linux/bpf.h#L964>
#[derive(Debug)]
pub enum MapType {
    Unspec,
    Hash,
    Array,
    ProgArray,
    PerfEventArray,
    PerCpuHash,
    PerCpuArray,
    StackTrace,
    CgroupArray,
    LruHash,
    LruPerCpuHash,
    LpmTrie,
    ArrayOfMaps,
    HashOfMaps,
    Devmap,
    Sockmap,
    Cpumap,
    Xskmap,
    Sockhash,
    CgroupStorage,
    ReuseportSockarray,
    PerCpuCgroupStorage,
    Queue,
    Stack,
    SkStorage,
    DevmapHash,
    StructOps,
    Ringbuf,
    InodeStorage,
    TaskStorage,
    BloomFilter,
    UserRingbuf,
    CgrpStorage,
    Arena,
}

/// This function is only used in the oci-utils for taking an object
/// file parsed by aya-obj, pulling out the maps included in it, and
/// presenting it in a user frendly manner, it will panic if it's called
/// with a non-checked integer, only use where pre-processing has occured.
impl From<u32> for MapType {
    fn from(value: u32) -> Self {
        match value {
            0 => MapType::Unspec,
            1 => MapType::Hash,
            2 => MapType::Array,
            3 => MapType::ProgArray,
            4 => MapType::PerfEventArray,
            5 => MapType::PerCpuHash,
            6 => MapType::PerCpuArray,
            7 => MapType::StackTrace,
            8 => MapType::CgroupArray,
            9 => MapType::LruHash,
            10 => MapType::LruPerCpuHash,
            11 => MapType::LpmTrie,
            12 => MapType::ArrayOfMaps,
            13 => MapType::HashOfMaps,
            14 => MapType::Devmap,
            15 => MapType::Sockmap,
            16 => MapType::Cpumap,
            17 => MapType::Xskmap,
            18 => MapType::Sockhash,
            20 => MapType::ReuseportSockarray,
            22 => MapType::Queue,
            23 => MapType::Stack,
            24 => MapType::SkStorage,
            25 => MapType::DevmapHash,
            26 => MapType::StructOps,
            27 => MapType::Ringbuf,
            28 => MapType::InodeStorage,
            29 => MapType::TaskStorage,
            30 => MapType::BloomFilter,
            31 => MapType::UserRingbuf,
            32 => MapType::CgrpStorage,
            33 => MapType::Arena,
            v => panic!("Unknown map type {v}"),
        }
    }
}

impl std::fmt::Display for MapType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            MapType::Unspec => "unspec",
            MapType::Hash => "hash",
            MapType::Array => "array",
            MapType::ProgArray => "prog_array",
            MapType::PerfEventArray => "perf_event_array",
            MapType::PerCpuHash => "per_cpu_hash",
            MapType::PerCpuArray => "per_cpu_array",
            MapType::StackTrace => "stack_trace",
            MapType::CgroupArray => "cgroup_array",
            MapType::LruHash => "lru_hash",
            MapType::LruPerCpuHash => "lru_per_cpu_hash",
            MapType::LpmTrie => "lpm_trie",
            MapType::ArrayOfMaps => "array_of_maps",
            MapType::HashOfMaps => "hash_of_maps",
            MapType::Devmap => "devmap",
            MapType::Sockmap => "sockmap",
            MapType::Cpumap => "cpumap",
            MapType::Xskmap => "xskmap",
            MapType::Sockhash => "sockhash",
            MapType::CgroupStorage => "cgroup_storage",
            MapType::ReuseportSockarray => "reuseport_sockarray",
            MapType::PerCpuCgroupStorage => "per_cpu_cgroup_storage",
            MapType::Queue => "queue",
            MapType::Stack => "stack",
            MapType::SkStorage => "sk_storage",
            MapType::DevmapHash => "devmap_hash",
            MapType::StructOps => "struct_ops",
            MapType::Ringbuf => "ringbuf",
            MapType::InodeStorage => "inode_storage",
            MapType::TaskStorage => "task_storage",
            MapType::BloomFilter => "bloom_filter",
            MapType::UserRingbuf => "user_ringbuf",
            MapType::CgrpStorage => "cgrp_storage",
            MapType::Arena => "arena",
        };
        write!(f, "{}", v)
    }
}

impl From<AyaMapType> for MapType {
    /// Convert from Aya's MapType to our MapType
    fn from(val: AyaMapType) -> Self {
        match val {
            AyaMapType::Unspecified => MapType::Unspec,
            AyaMapType::Hash => MapType::Hash,
            AyaMapType::Array => MapType::Array,
            AyaMapType::ProgramArray => MapType::ProgArray,
            AyaMapType::PerfEventArray => MapType::PerfEventArray,
            AyaMapType::PerCpuHash => MapType::PerCpuHash,
            AyaMapType::PerCpuArray => MapType::PerCpuArray,
            AyaMapType::StackTrace => MapType::StackTrace,
            AyaMapType::CgroupArray => MapType::CgroupArray,
            AyaMapType::LruHash => MapType::LruHash,
            AyaMapType::LruPerCpuHash => MapType::LruPerCpuHash,
            AyaMapType::LpmTrie => MapType::LpmTrie,
            AyaMapType::ArrayOfMaps => MapType::ArrayOfMaps,
            AyaMapType::HashOfMaps => MapType::HashOfMaps,
            AyaMapType::DevMap => MapType::Devmap,
            AyaMapType::SockMap => MapType::Sockmap,
            AyaMapType::CpuMap => MapType::Cpumap,
            AyaMapType::XskMap => MapType::Xskmap,
            AyaMapType::SockHash => MapType::Sockhash,
            AyaMapType::CgroupStorage => MapType::CgroupStorage,
            AyaMapType::ReuseportSockArray => MapType::ReuseportSockarray,
            AyaMapType::PerCpuCgroupStorage => MapType::PerCpuCgroupStorage,
            AyaMapType::Queue => MapType::Queue,
            AyaMapType::Stack => MapType::Stack,
            AyaMapType::SkStorage => MapType::SkStorage,
            AyaMapType::DevMapHash => MapType::DevmapHash,
            AyaMapType::StructOps => MapType::StructOps,
            AyaMapType::RingBuf => MapType::Ringbuf,
            AyaMapType::InodeStorage => MapType::InodeStorage,
            AyaMapType::TaskStorage => MapType::TaskStorage,
            AyaMapType::BloomFilter => MapType::BloomFilter,
            AyaMapType::UserRingBuf => MapType::UserRingbuf,
            AyaMapType::CgrpStorage => MapType::CgrpStorage,
            AyaMapType::Arena => MapType::Arena,
            _ => MapType::Unspec,
        }
    }
}

/// ProgramType must match the the bpf_prog_type enum defined in the linux kernel.
/// <https://elixir.bootlin.com/linux/latest/source/include/uapi/linux/bpf.h#L1024>
#[derive(ValueEnum, Copy, Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
pub enum ProgramType {
    Unspec,
    SocketFilter,
    Probe, // kprobe, kretprobe, uprobe, uretprobe
    Tc,
    SchedAct,
    Tracepoint,
    Xdp,
    PerfEvent,
    CgroupSkb,
    CgroupSock,
    LwtIn,
    LwtOut,
    LwtXmit,
    SockOps,
    SkSkb,
    CgroupDevice,
    SkMsg,
    RawTracepoint,
    CgroupSockAddr,
    LwtSeg6Local,
    LircMode2,
    SkReuseport,
    FlowDissector,
    CgroupSysctl,
    RawTracepointWritable,
    CgroupSockopt,
    Tracing, // fentry, fexit
    StructOps,
    Ext,
    Lsm,
    SkLookup,
    Syscall,
    Netfilter,
}

impl From<aya_obj::ProgramSection> for ProgramType {
    fn from(value: aya_obj::ProgramSection) -> Self {
        match value {
            aya_obj::ProgramSection::KRetProbe => ProgramType::Probe,
            aya_obj::ProgramSection::KProbe => ProgramType::Probe,
            aya_obj::ProgramSection::UProbe { .. } => ProgramType::Probe,
            aya_obj::ProgramSection::URetProbe { .. } => ProgramType::Probe,
            aya_obj::ProgramSection::TracePoint => ProgramType::Tracepoint,
            aya_obj::ProgramSection::SocketFilter => ProgramType::SocketFilter,
            aya_obj::ProgramSection::Xdp { .. } => ProgramType::Xdp,
            aya_obj::ProgramSection::SkMsg => ProgramType::SkMsg,
            aya_obj::ProgramSection::SkSkbStreamParser => ProgramType::SkSkb,
            aya_obj::ProgramSection::SkSkbStreamVerdict => ProgramType::SkSkb,
            aya_obj::ProgramSection::SockOps => ProgramType::SockOps,
            aya_obj::ProgramSection::SchedClassifier => ProgramType::Tc,
            aya_obj::ProgramSection::CgroupSkb => ProgramType::CgroupSkb,
            aya_obj::ProgramSection::CgroupSkbIngress => ProgramType::CgroupSkb,
            aya_obj::ProgramSection::CgroupSkbEgress => ProgramType::CgroupSkb,
            aya_obj::ProgramSection::CgroupSockAddr { .. } => ProgramType::CgroupSockAddr,
            aya_obj::ProgramSection::CgroupSysctl => ProgramType::CgroupSysctl,
            aya_obj::ProgramSection::CgroupSockopt { .. } => ProgramType::CgroupSockopt,
            aya_obj::ProgramSection::LircMode2 => ProgramType::LircMode2,
            aya_obj::ProgramSection::PerfEvent => ProgramType::PerfEvent,
            aya_obj::ProgramSection::RawTracePoint => ProgramType::RawTracepoint,
            aya_obj::ProgramSection::Lsm { .. } => ProgramType::Lsm,
            aya_obj::ProgramSection::BtfTracePoint => ProgramType::Tracepoint,
            aya_obj::ProgramSection::FEntry { .. } => ProgramType::Tracing,
            aya_obj::ProgramSection::FExit { .. } => ProgramType::Tracing,
            aya_obj::ProgramSection::Extension => ProgramType::Ext,
            aya_obj::ProgramSection::SkLookup => ProgramType::SkLookup,
            aya_obj::ProgramSection::CgroupSock { .. } => ProgramType::CgroupSock,
            aya_obj::ProgramSection::CgroupDevice { .. } => ProgramType::CgroupDevice,
        }
    }
}

impl TryFrom<String> for ProgramType {
    type Error = ParseError;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "unspec" => ProgramType::Unspec,
            "socket_filter" => ProgramType::SocketFilter,
            "probe" => ProgramType::Probe,
            "tc" => ProgramType::Tc,
            "sched_act" => ProgramType::SchedAct,
            "tracepoint" => ProgramType::Tracepoint,
            "xdp" => ProgramType::Xdp,
            "perf_event" => ProgramType::PerfEvent,
            "cgroup_skb" => ProgramType::CgroupSkb,
            "cgroup_sock" => ProgramType::CgroupSock,
            "lwt_in" => ProgramType::LwtIn,
            "lwt_out" => ProgramType::LwtOut,
            "lwt_xmit" => ProgramType::LwtXmit,
            "sock_ops" => ProgramType::SockOps,
            "sk_skb" => ProgramType::SkSkb,
            "cgroup_device" => ProgramType::CgroupDevice,
            "sk_msg" => ProgramType::SkMsg,
            "raw_tracepoint" => ProgramType::RawTracepoint,
            "cgroup_sock_addr" => ProgramType::CgroupSockAddr,
            "lwt_seg6local" => ProgramType::LwtSeg6Local,
            "lirc_mode2" => ProgramType::LircMode2,
            "sk_reuseport" => ProgramType::SkReuseport,
            "flow_dissector" => ProgramType::FlowDissector,
            "cgroup_sysctl" => ProgramType::CgroupSysctl,
            "raw_tracepoint_writable" => ProgramType::RawTracepointWritable,
            "cgroup_sockopt" => ProgramType::CgroupSockopt,
            "tracing" => ProgramType::Tracing,
            "struct_ops" => ProgramType::StructOps,
            "ext" => ProgramType::Ext,
            "lsm" => ProgramType::Lsm,
            "sk_lookup" => ProgramType::SkLookup,
            "syscall" => ProgramType::Syscall,
            "netfilter" => ProgramType::Netfilter,
            other => {
                return Err(ParseError::InvalidProgramType {
                    program: other.to_string(),
                })
            }
        })
    }
}

impl TryFrom<u32> for ProgramType {
    type Error = ParseError;

    fn try_from(value: u32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ProgramType::Unspec,
            1 => ProgramType::SocketFilter,
            2 => ProgramType::Probe,
            3 => ProgramType::Tc,
            4 => ProgramType::SchedAct,
            5 => ProgramType::Tracepoint,
            6 => ProgramType::Xdp,
            7 => ProgramType::PerfEvent,
            8 => ProgramType::CgroupSkb,
            9 => ProgramType::CgroupSock,
            10 => ProgramType::LwtIn,
            11 => ProgramType::LwtOut,
            12 => ProgramType::LwtXmit,
            13 => ProgramType::SockOps,
            14 => ProgramType::SkSkb,
            15 => ProgramType::CgroupDevice,
            16 => ProgramType::SkMsg,
            17 => ProgramType::RawTracepoint,
            18 => ProgramType::CgroupSockAddr,
            19 => ProgramType::LwtSeg6Local,
            20 => ProgramType::LircMode2,
            21 => ProgramType::SkReuseport,
            22 => ProgramType::FlowDissector,
            23 => ProgramType::CgroupSysctl,
            24 => ProgramType::RawTracepointWritable,
            25 => ProgramType::CgroupSockopt,
            26 => ProgramType::Tracing,
            27 => ProgramType::StructOps,
            28 => ProgramType::Ext,
            29 => ProgramType::Lsm,
            30 => ProgramType::SkLookup,
            31 => ProgramType::Syscall,
            32 => ProgramType::Netfilter,
            other => {
                return Err(ParseError::InvalidProgramType {
                    program: other.to_string(),
                })
            }
        })
    }
}

impl From<ProgramType> for u32 {
    fn from(val: ProgramType) -> Self {
        match val {
            ProgramType::Unspec => 0,
            ProgramType::SocketFilter => 1,
            ProgramType::Probe => 2,
            ProgramType::Tc => 3,
            ProgramType::SchedAct => 4,
            ProgramType::Tracepoint => 5,
            ProgramType::Xdp => 6,
            ProgramType::PerfEvent => 7,
            ProgramType::CgroupSkb => 8,
            ProgramType::CgroupSock => 9,
            ProgramType::LwtIn => 10,
            ProgramType::LwtOut => 11,
            ProgramType::LwtXmit => 12,
            ProgramType::SockOps => 13,
            ProgramType::SkSkb => 14,
            ProgramType::CgroupDevice => 15,
            ProgramType::SkMsg => 16,
            ProgramType::RawTracepoint => 17,
            ProgramType::CgroupSockAddr => 18,
            ProgramType::LwtSeg6Local => 19,
            ProgramType::LircMode2 => 20,
            ProgramType::SkReuseport => 21,
            ProgramType::FlowDissector => 22,
            ProgramType::CgroupSysctl => 23,
            ProgramType::RawTracepointWritable => 24,
            ProgramType::CgroupSockopt => 25,
            ProgramType::Tracing => 26,
            ProgramType::StructOps => 27,
            ProgramType::Ext => 28,
            ProgramType::Lsm => 29,
            ProgramType::SkLookup => 30,
            ProgramType::Syscall => 31,
            ProgramType::Netfilter => 32,
        }
    }
}

impl std::fmt::Display for ProgramType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            ProgramType::Unspec => "unspec",
            ProgramType::SocketFilter => "socket_filter",
            ProgramType::Probe => "probe",
            ProgramType::Tc => "tc",
            ProgramType::SchedAct => "sched_act",
            ProgramType::Tracepoint => "tracepoint",
            ProgramType::Xdp => "xdp",
            ProgramType::PerfEvent => "perf_event",
            ProgramType::CgroupSkb => "cgroup_skb",
            ProgramType::CgroupSock => "cgroup_sock",
            ProgramType::LwtIn => "lwt_in",
            ProgramType::LwtOut => "lwt_out",
            ProgramType::LwtXmit => "lwt_xmit",
            ProgramType::SockOps => "sock_ops",
            ProgramType::SkSkb => "sk_skb",
            ProgramType::CgroupDevice => "cgroup_device",
            ProgramType::SkMsg => "sk_msg",
            ProgramType::RawTracepoint => "raw_tracepoint",
            ProgramType::CgroupSockAddr => "cgroup_sock_addr",
            ProgramType::LwtSeg6Local => "lwt_seg6local",
            ProgramType::LircMode2 => "lirc_mode2",
            ProgramType::SkReuseport => "sk_reuseport",
            ProgramType::FlowDissector => "flow_dissector",
            ProgramType::CgroupSysctl => "cgroup_sysctl",
            ProgramType::RawTracepointWritable => "raw_tracepoint_writable",
            ProgramType::CgroupSockopt => "cgroup_sockopt",
            ProgramType::Tracing => "tracing",
            ProgramType::StructOps => "struct_ops",
            ProgramType::Ext => "ext",
            ProgramType::Lsm => "lsm",
            ProgramType::SkLookup => "sk_lookup",
            ProgramType::Syscall => "syscall",
            ProgramType::Netfilter => "netfilter",
        };
        write!(f, "{v}")
    }
}

impl From<AyaProgramType> for ProgramType {
    /// Convert from aya's ProgramType to our ProgramType
    fn from(val: AyaProgramType) -> Self {
        match val {
            AyaProgramType::Unspecified => ProgramType::Unspec,
            AyaProgramType::SocketFilter => ProgramType::SocketFilter,
            AyaProgramType::KProbe => ProgramType::Probe,
            AyaProgramType::SchedClassifier => ProgramType::Tc,
            AyaProgramType::SchedAction => ProgramType::SchedAct,
            AyaProgramType::TracePoint => ProgramType::Tracepoint,
            AyaProgramType::Xdp => ProgramType::Xdp,
            AyaProgramType::PerfEvent => ProgramType::PerfEvent,
            AyaProgramType::CgroupSkb => ProgramType::CgroupSkb,
            AyaProgramType::CgroupSock => ProgramType::CgroupSock,
            AyaProgramType::LwtInput => ProgramType::LwtIn,
            AyaProgramType::LwtOutput => ProgramType::LwtOut,
            AyaProgramType::LwtXmit => ProgramType::LwtXmit,
            AyaProgramType::SockOps => ProgramType::SockOps,
            AyaProgramType::SkSkb => ProgramType::SkSkb,
            AyaProgramType::CgroupDevice => ProgramType::CgroupDevice,
            AyaProgramType::SkMsg => ProgramType::SkMsg,
            AyaProgramType::RawTracePoint => ProgramType::RawTracepoint,
            AyaProgramType::CgroupSockAddr => ProgramType::CgroupSockAddr,
            AyaProgramType::LwtSeg6local => ProgramType::LwtSeg6Local,
            AyaProgramType::LircMode2 => ProgramType::LircMode2,
            AyaProgramType::SkReuseport => ProgramType::SkReuseport,
            AyaProgramType::FlowDissector => ProgramType::FlowDissector,
            AyaProgramType::CgroupSysctl => ProgramType::CgroupSysctl,
            AyaProgramType::RawTracePointWritable => ProgramType::RawTracepointWritable,
            AyaProgramType::CgroupSockopt => ProgramType::CgroupSockopt,
            AyaProgramType::Tracing => ProgramType::Tracing,
            AyaProgramType::StructOps => ProgramType::StructOps,
            AyaProgramType::Extension => ProgramType::Ext,
            AyaProgramType::Lsm => ProgramType::Lsm,
            AyaProgramType::SkLookup => ProgramType::SkLookup,
            AyaProgramType::Syscall => ProgramType::Syscall,
            AyaProgramType::Netfilter => ProgramType::Netfilter,
            _ => ProgramType::Unspec,
        }
    }
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
pub enum ProbeType {
    Kprobe,
    Kretprobe,
    Uprobe,
    Uretprobe,
}

impl TryFrom<i32> for ProbeType {
    type Error = ParseError;

    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ProbeType::Kprobe,
            1 => ProbeType::Kretprobe,
            2 => ProbeType::Uprobe,
            3 => ProbeType::Uretprobe,
            other => {
                return Err(ParseError::InvalidProbeType {
                    probe: other.to_string(),
                })
            }
        })
    }
}

impl From<aya::programs::ProbeKind> for ProbeType {
    fn from(value: aya::programs::ProbeKind) -> Self {
        match value {
            aya::programs::ProbeKind::KProbe => ProbeType::Kprobe,
            aya::programs::ProbeKind::KRetProbe => ProbeType::Kretprobe,
            aya::programs::ProbeKind::UProbe => ProbeType::Uprobe,
            aya::programs::ProbeKind::URetProbe => ProbeType::Uretprobe,
        }
    }
}

impl std::fmt::Display for ProbeType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            ProbeType::Kprobe => "kprobe",
            ProbeType::Kretprobe => "kretprobe",
            ProbeType::Uprobe => "uprobe",
            ProbeType::Uretprobe => "uretprobe",
        };
        write!(f, "{v}")
    }
}

#[derive(Serialize, Deserialize, Copy, Clone, Debug)]
pub enum XdpProceedOnEntry {
    Aborted,
    Drop,
    Pass,
    Tx,
    Redirect,
    DispatcherReturn = 31,
}

impl FromIterator<XdpProceedOnEntry> for XdpProceedOn {
    fn from_iter<I: IntoIterator<Item = XdpProceedOnEntry>>(iter: I) -> Self {
        let mut c = Vec::new();

        let mut iter = iter.into_iter().peekable();

        // make sure to default if proceed on is empty
        if iter.peek().is_none() {
            return XdpProceedOn::default();
        };

        for i in iter {
            c.push(i);
        }

        XdpProceedOn(c)
    }
}

impl TryFrom<String> for XdpProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "aborted" => XdpProceedOnEntry::Aborted,
            "drop" => XdpProceedOnEntry::Drop,
            "pass" => XdpProceedOnEntry::Pass,
            "tx" => XdpProceedOnEntry::Tx,
            "redirect" => XdpProceedOnEntry::Redirect,
            "dispatcher_return" => XdpProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl TryFrom<i32> for XdpProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => XdpProceedOnEntry::Aborted,
            1 => XdpProceedOnEntry::Drop,
            2 => XdpProceedOnEntry::Pass,
            3 => XdpProceedOnEntry::Tx,
            4 => XdpProceedOnEntry::Redirect,
            31 => XdpProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl std::fmt::Display for XdpProceedOnEntry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            XdpProceedOnEntry::Aborted => "aborted",
            XdpProceedOnEntry::Drop => "drop",
            XdpProceedOnEntry::Pass => "pass",
            XdpProceedOnEntry::Tx => "tx",
            XdpProceedOnEntry::Redirect => "redirect",
            XdpProceedOnEntry::DispatcherReturn => "dispatcher_return",
        };
        write!(f, "{v}")
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct XdpProceedOn(Vec<XdpProceedOnEntry>);
impl Default for XdpProceedOn {
    fn default() -> Self {
        XdpProceedOn(vec![
            XdpProceedOnEntry::Pass,
            XdpProceedOnEntry::DispatcherReturn,
        ])
    }
}

impl XdpProceedOn {
    pub fn from_strings<T: AsRef<[String]>>(values: T) -> Result<XdpProceedOn, ParseError> {
        let entries = values.as_ref();
        let mut res = vec![];
        for e in entries {
            res.push(e.to_owned().try_into()?)
        }
        Ok(XdpProceedOn(res))
    }

    pub fn from_int32s<T: AsRef<[i32]>>(values: T) -> Result<XdpProceedOn, ParseError> {
        let entries = values.as_ref();
        if entries.is_empty() {
            return Ok(XdpProceedOn::default());
        }
        let mut res = vec![];
        for e in entries {
            res.push((*e).try_into()?)
        }
        Ok(XdpProceedOn(res))
    }

    pub fn mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        for action in self.0.clone().into_iter() {
            proceed_on_mask |= 1 << action as u32;
        }
        proceed_on_mask
    }

    pub fn as_action_vec(&self) -> Vec<i32> {
        let mut res = vec![];
        for entry in &self.0 {
            res.push((*entry) as i32)
        }
        res
    }
}

impl std::fmt::Display for XdpProceedOn {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let res: Vec<String> = self.0.iter().map(|x| x.to_string()).collect();
        write!(f, "{}", res.join(", "))
    }
}

#[derive(Serialize, Deserialize, Copy, Clone, Debug)]
pub enum TcProceedOnEntry {
    Unspec = -1,
    Ok = 0,
    Reclassify,
    Shot,
    Pipe,
    Stolen,
    Queued,
    Repeat,
    Redirect,
    Trap,
    DispatcherReturn = 30,
}

impl TryFrom<String> for TcProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "unspec" => TcProceedOnEntry::Unspec,
            "ok" => TcProceedOnEntry::Ok,
            "reclassify" => TcProceedOnEntry::Reclassify,
            "shot" => TcProceedOnEntry::Shot,
            "pipe" => TcProceedOnEntry::Pipe,
            "stolen" => TcProceedOnEntry::Stolen,
            "queued" => TcProceedOnEntry::Queued,
            "repeat" => TcProceedOnEntry::Repeat,
            "redirect" => TcProceedOnEntry::Redirect,
            "trap" => TcProceedOnEntry::Trap,
            "dispatcher_return" => TcProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl TryFrom<i32> for TcProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            -1 => TcProceedOnEntry::Unspec,
            0 => TcProceedOnEntry::Ok,
            1 => TcProceedOnEntry::Reclassify,
            2 => TcProceedOnEntry::Shot,
            3 => TcProceedOnEntry::Pipe,
            4 => TcProceedOnEntry::Stolen,
            5 => TcProceedOnEntry::Queued,
            6 => TcProceedOnEntry::Repeat,
            7 => TcProceedOnEntry::Redirect,
            8 => TcProceedOnEntry::Trap,
            30 => TcProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl std::fmt::Display for TcProceedOnEntry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            TcProceedOnEntry::Unspec => "unspec",
            TcProceedOnEntry::Ok => "ok",
            TcProceedOnEntry::Reclassify => "reclassify",
            TcProceedOnEntry::Shot => "shot",
            TcProceedOnEntry::Pipe => "pipe",
            TcProceedOnEntry::Stolen => "stolen",
            TcProceedOnEntry::Queued => "queued",
            TcProceedOnEntry::Repeat => "repeat",
            TcProceedOnEntry::Redirect => "redirect",
            TcProceedOnEntry::Trap => "trap",
            TcProceedOnEntry::DispatcherReturn => "dispatcher_return",
        };
        write!(f, "{v}")
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct TcProceedOn(pub(crate) Vec<TcProceedOnEntry>);
impl Default for TcProceedOn {
    fn default() -> Self {
        TcProceedOn(vec![
            TcProceedOnEntry::Pipe,
            TcProceedOnEntry::DispatcherReturn,
        ])
    }
}

impl FromIterator<TcProceedOnEntry> for TcProceedOn {
    fn from_iter<I: IntoIterator<Item = TcProceedOnEntry>>(iter: I) -> Self {
        let mut c = Vec::new();
        let mut iter = iter.into_iter().peekable();

        // make sure to default if proceed on is empty
        if iter.peek().is_none() {
            return TcProceedOn::default();
        };

        for i in iter {
            c.push(i);
        }

        TcProceedOn(c)
    }
}

impl TcProceedOn {
    pub fn from_strings<T: AsRef<[String]>>(values: T) -> Result<TcProceedOn, ParseError> {
        let entries = values.as_ref();
        let mut res = vec![];
        for e in entries {
            res.push(e.to_owned().try_into()?)
        }
        Ok(TcProceedOn(res))
    }

    pub fn from_int32s<T: AsRef<[i32]>>(values: T) -> Result<TcProceedOn, ParseError> {
        let entries = values.as_ref();
        if entries.is_empty() {
            return Ok(TcProceedOn::default());
        }
        let mut res = vec![];
        for e in entries {
            res.push((*e).try_into()?)
        }
        Ok(TcProceedOn(res))
    }

    // Valid TC return values range from -1 to 8.  Since -1 is not a valid shift value,
    // 1 is added to the value to determine the bit to set in the bitmask and,
    // correspondingly, The TC dispatcher adds 1 to the return value from the BPF program
    // before it compares it to the configured bit mask.
    pub fn mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        for action in self.0.clone().into_iter() {
            proceed_on_mask |= 1 << ((action as i32) + 1);
        }
        proceed_on_mask
    }

    pub fn as_action_vec(&self) -> Vec<i32> {
        let mut res = vec![];
        for entry in &self.0 {
            res.push((*entry) as i32)
        }
        res
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

impl std::fmt::Display for TcProceedOn {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let res: Vec<String> = self.0.iter().map(|x| x.to_string()).collect();
        write!(f, "{}", res.join(", "))
    }
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub enum ImagePullPolicy {
    Always,
    IfNotPresent,
    Never,
}

impl std::fmt::Display for ImagePullPolicy {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            ImagePullPolicy::Always => "Always",
            ImagePullPolicy::IfNotPresent => "IfNotPresent",
            ImagePullPolicy::Never => "Never",
        };
        write!(f, "{v}")
    }
}

impl TryFrom<i32> for ImagePullPolicy {
    type Error = ParseError;
    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ImagePullPolicy::Always,
            1 => ImagePullPolicy::IfNotPresent,
            2 => ImagePullPolicy::Never,
            policy => {
                return Err(ParseError::InvalidBytecodeImagePullPolicy {
                    pull_policy: policy.to_string(),
                })
            }
        })
    }
}

impl TryFrom<&str> for ImagePullPolicy {
    type Error = ParseError;
    fn try_from(value: &str) -> Result<Self, Self::Error> {
        Ok(match value {
            "Always" => ImagePullPolicy::Always,
            "IfNotPresent" => ImagePullPolicy::IfNotPresent,
            "Never" => ImagePullPolicy::Never,
            policy => {
                return Err(ParseError::InvalidBytecodeImagePullPolicy {
                    pull_policy: policy.to_string(),
                })
            }
        })
    }
}

impl From<ImagePullPolicy> for i32 {
    fn from(value: ImagePullPolicy) -> Self {
        match value {
            ImagePullPolicy::Always => 0,
            ImagePullPolicy::IfNotPresent => 1,
            ImagePullPolicy::Never => 2,
        }
    }
}

impl std::fmt::Display for Location {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match &self {
            // Cast imagePullPolicy into it's concrete type so we can easily print.
            Location::Image(i) => write!(
                f,
                "image: {{ url: {}, pullpolicy: {} }}",
                i.image_url,
                TryInto::<ImagePullPolicy>::try_into(i.image_pull_policy.clone()).unwrap()
            ),
            Location::File(p) => write!(f, "file: {{ path: {p} }}"),
        }
    }
}
