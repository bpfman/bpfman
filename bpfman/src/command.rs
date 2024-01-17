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
        KernelProgramInfo as V1KernelProgramInfo, KprobeAttachInfo, ProgramInfo as V1ProgramInfo,
        TcAttachInfo, TracepointAttachInfo, UprobeAttachInfo, XdpAttachInfo,
    },
    ParseError, ProgramType, TcProceedOn, TcProceedOnEntry, XdpProceedOn, XdpProceedOnEntry,
};
use chrono::{prelude::DateTime, Local};
use log::info;
use rand::Rng;
use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc::Sender, oneshot};

use crate::{
    errors::BpfmanError,
    multiprog::{DispatcherId, DispatcherInfo},
    oci_utils::image_manager::{BytecodeImage, Command as ImageManagerCommand},
    utils::{
        bytes_to_bool, bytes_to_i32, bytes_to_string, bytes_to_u32, bytes_to_u64, bytes_to_usize,
        sled_get, sled_get_option, sled_insert,
    },
    ROOT_DB,
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

#[derive(Debug, Clone)]
pub(crate) enum Program {
    Xdp(XdpProgram),
    Tc(TcProgram),
    Tracepoint(TracepointProgram),
    Kprobe(KprobeProgram),
    Uprobe(UprobeProgram),
    Unsupported(ProgramData),
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
pub(crate) struct ProgramData {
    // Prior to load this will be a temporary Tree with a random ID, following
    // load it will be replaced with the main program database tree.
    db_tree: sled::Tree,

    // populated after load, randomly generated prior to load.
    id: u32,

    // program_bytes is used to temporarily cache the raw program data during
    // the loading process.  It MUST be cleared following a load so that there
    // is not a long lived copy of the program data living on the heap.
    program_bytes: Vec<u8>,
}

impl ProgramData {
    pub(crate) fn new(tree: sled::Tree, id: u32) -> Self {
        Self {
            db_tree: tree,
            id,
            program_bytes: Vec::new(),
        }
    }
    pub(crate) fn new_pre_load(
        location: Location,
        name: String,
        metadata: HashMap<String, String>,
        global_data: HashMap<String, Vec<u8>>,
        map_owner_id: Option<u32>,
    ) -> Result<Self, BpfmanError> {
        let mut rng = rand::thread_rng();
        let id_rand = rng.gen::<u32>();

        let db_tree = ROOT_DB
            .open_tree(id_rand.to_string())
            .expect("Unable to open program database tree");

        let mut pd = Self {
            db_tree,
            id: id_rand,
            program_bytes: Vec::new(),
        };

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
            .open_tree(new_id.to_string())
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
        self.id = new_id;

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
            "kind",
            &(Into::<u32>::into(kind)).to_ne_bytes(),
        )
    }

    fn get_kind(&self) -> Result<Option<ProgramType>, BpfmanError> {
        sled_get_option(&self.db_tree, "kind")
            .map(|v| v.map(|v| bytes_to_u32(v).try_into().unwrap()))
    }

    pub(crate) fn set_name(&mut self, name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, "name", name.as_bytes())
    }

    pub(crate) fn get_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, "name").map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_id(&mut self, id: u32) -> Result<(), BpfmanError> {
        // set db and local cache
        self.id = id;
        sled_insert(&self.db_tree, "id", &id.to_ne_bytes())
    }

    pub(crate) fn get_id(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "id").map(bytes_to_u32)
    }

    pub(crate) fn set_location(&mut self, loc: Location) -> Result<(), BpfmanError> {
        match loc {
            Location::File(l) => sled_insert(&self.db_tree, "location_filename", l.as_bytes()),
            Location::Image(l) => {
                sled_insert(&self.db_tree, "location_image_url", l.image_url.as_bytes())?;
                sled_insert(
                    &self.db_tree,
                    "location_image_pull_policy",
                    l.image_pull_policy.to_string().as_bytes(),
                )?;
                if let Some(u) = l.username {
                    sled_insert(&self.db_tree, "location_username", u.as_bytes())?;
                };

                if let Some(p) = l.password {
                    sled_insert(&self.db_tree, "location_password", p.as_bytes())?;
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
        if let Ok(l) = sled_get(&self.db_tree, "location_filename") {
            Ok(Location::File(bytes_to_string(&l).to_string()))
        } else {
            Ok(Location::Image(BytecodeImage {
                image_url: bytes_to_string(&sled_get(&self.db_tree, "location_image_url")?)
                    .to_string(),
                image_pull_policy: bytes_to_string(&sled_get(
                    &self.db_tree,
                    "location_image_pull_policy",
                )?)
                .as_str()
                .try_into()
                .unwrap(),
                username: sled_get_option(&self.db_tree, "location_username")?
                    .map(|v| bytes_to_string(&v)),
                password: sled_get_option(&self.db_tree, "location_password")?
                    .map(|v| bytes_to_string(&v)),
            }))
        }
    }

    pub(crate) fn set_global_data(
        &mut self,
        data: HashMap<String, Vec<u8>>,
    ) -> Result<(), BpfmanError> {
        data.iter().try_for_each(|(k, v)| {
            sled_insert(&self.db_tree, format!("global_data_{k}").as_str(), v)
        })
    }

    pub(crate) fn get_global_data(&self) -> Result<HashMap<String, Vec<u8>>, BpfmanError> {
        self.db_tree
            .scan_prefix("global_data_")
            .map(|n| {
                n.map(|(k, v)| {
                    (
                        bytes_to_string(&k)
                            .strip_prefix("global_data_")
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
                format!("metadata_{k}").as_str(),
                v.as_bytes(),
            )
        })
    }

    pub(crate) fn get_metadata(&self) -> Result<HashMap<String, String>, BpfmanError> {
        self.db_tree
            .scan_prefix("metadata_")
            .map(|n| {
                n.map(|(k, v)| {
                    (
                        bytes_to_string(&k)
                            .strip_prefix("metadata_")
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
        sled_insert(&self.db_tree, "map_owner_id", &id.to_ne_bytes())
    }

    pub(crate) fn get_map_owner_id(&self) -> Result<Option<u32>, BpfmanError> {
        sled_get_option(&self.db_tree, "map_owner_id").map(|v| v.map(bytes_to_u32))
    }

    pub(crate) fn set_map_pin_path(&mut self, path: &Path) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "map_pin_path",
            path.to_str().unwrap().as_bytes(),
        )
    }

    pub(crate) fn get_map_pin_path(&self) -> Result<Option<PathBuf>, BpfmanError> {
        sled_get_option(&self.db_tree, "map_pin_path")
            .map(|v| v.map(|f| PathBuf::from(bytes_to_string(&f))))
    }

    // set_maps_used_by differs from other setters in that it's explicitly idempotent.
    pub(crate) fn set_maps_used_by(&mut self, ids: Vec<u32>) -> Result<(), BpfmanError> {
        self.clear_maps_used_by();

        ids.iter().enumerate().try_for_each(|(i, v)| {
            sled_insert(
                &self.db_tree,
                format!("maps_used_by_{i}").as_str(),
                &v.to_ne_bytes(),
            )
        })
    }

    pub(crate) fn get_maps_used_by(&self) -> Result<Vec<u32>, BpfmanError> {
        self.db_tree
            .scan_prefix("maps_used_by_")
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
        self.db_tree.scan_prefix("maps_used_by_").for_each(|n| {
            self.db_tree
                .remove(n.unwrap().0)
                .expect("unable to clear maps used by");
        });
    }

    /*
     * End bpfman program info getters/setters.
     */

    /*
     * Methods for setting and getting kernel information.
     */

    pub(crate) fn get_kernel_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, "kernel_name").map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_name(&mut self, name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, "kernel_name", name.as_bytes())
    }

    pub(crate) fn get_kernel_program_type(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "kernel_program_type").map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_program_type(&mut self, program_type: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_program_type",
            &program_type.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_loaded_at(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, "kernel_loaded_at").map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_loaded_at(
        &mut self,
        loaded_at: SystemTime,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_loaded_at",
            DateTime::<Local>::from(loaded_at)
                .format("%Y-%m-%dT%H:%M:%S%z")
                .to_string()
                .as_bytes(),
        )
    }

    pub(crate) fn get_kernel_tag(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, "kernel_tag").map(|n| bytes_to_string(&n))
    }

    pub(crate) fn set_kernel_tag(&mut self, tag: u64) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_tag",
            format!("{:x}", tag).as_str().as_bytes(),
        )
    }

    pub(crate) fn set_kernel_gpl_compatible(
        &mut self,
        gpl_compatible: bool,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_gpl_compatible",
            &(gpl_compatible as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_gpl_compatible(&self) -> Result<bool, BpfmanError> {
        sled_get(&self.db_tree, "kernel_gpl_compatible").map(bytes_to_bool)
    }

    pub(crate) fn get_kernel_map_ids(&self) -> Result<Vec<u32>, BpfmanError> {
        self.db_tree
            .scan_prefix("kernel_map_ids_".as_bytes())
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
            sled_insert(&self.db_tree, format!("kernel_map_ids_{i}").as_str(), v)
        })
    }

    pub(crate) fn get_kernel_btf_id(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "kernel_btf_id").map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_btf_id(&mut self, btf_id: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, "kernel_btf_id", &btf_id.to_ne_bytes())
    }

    pub(crate) fn get_kernel_bytes_xlated(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "kernel_bytes_xlated").map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_xlated(&mut self, bytes_xlated: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_bytes_xlated",
            &bytes_xlated.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_jited(&self) -> Result<bool, BpfmanError> {
        sled_get(&self.db_tree, "kernel_jited").map(bytes_to_bool)
    }

    pub(crate) fn set_kernel_jited(&mut self, jited: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_jited",
            &(jited as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_bytes_jited(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "kernel_bytes_jited").map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_jited(&mut self, bytes_jited: u32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_bytes_jited",
            &bytes_jited.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_bytes_memlock(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "kernel_bytes_memlock").map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_bytes_memlock(
        &mut self,
        bytes_memlock: u32,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_bytes_memlock",
            &bytes_memlock.to_ne_bytes(),
        )
    }

    pub(crate) fn get_kernel_verified_insns(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, "kernel_verified_insns").map(bytes_to_u32)
    }

    pub(crate) fn set_kernel_verified_insns(
        &mut self,
        verified_insns: u32,
    ) -> Result<(), BpfmanError> {
        sled_insert(
            &self.db_tree,
            "kernel_verified_insns",
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
                self.program_bytes = v;
                Ok(())
            }
        }
    }
}

#[derive(Debug, Clone)]
pub(crate) struct XdpProgram {
    data: ProgramData,
}

impl XdpProgram {
    pub(crate) fn new(
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
        sled_insert(&self.data.db_tree, "xdp_priority", &priority.to_ne_bytes())
    }

    pub(crate) fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, "xdp_priority").map(bytes_to_i32)
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "xdp_iface", iface.as_bytes())
    }

    pub(crate) fn get_iface(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, "xdp_iface").map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_proceed_on(&mut self, proceed_on: XdpProceedOn) -> Result<(), BpfmanError> {
        proceed_on
            .as_action_vec()
            .iter()
            .enumerate()
            .try_for_each(|(i, v)| {
                sled_insert(
                    &self.data.db_tree,
                    format!("xdp_proceed_on_{i}").as_str(),
                    &v.to_ne_bytes(),
                )
            })
    }

    pub(crate) fn get_proceed_on(&self) -> Result<XdpProceedOn, BpfmanError> {
        self.data
            .db_tree
            .scan_prefix("xdp_proceed_on_")
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
        sled_insert(
            &self.data.db_tree,
            "xdp_current_position",
            &pos.to_ne_bytes(),
        )
    }

    pub(crate) fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "xdp_current_position")?.map(bytes_to_usize))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "xdp_if_index", &if_index.to_ne_bytes())
    }

    pub(crate) fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "xdp_if_index")?.map(bytes_to_u32))
    }

    pub(crate) fn set_attached(&mut self, attached: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "xdp_attached",
            &(attached as i8).to_ne_bytes(),
        )
    }

    pub(crate) fn get_attached(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "xdp_attached")?
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
pub(crate) struct TcProgram {
    pub(crate) data: ProgramData,
}

impl TcProgram {
    pub(crate) fn new(
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
        sled_insert(&self.data.db_tree, "tc_priority", &priority.to_ne_bytes())
    }

    pub(crate) fn get_priority(&self) -> Result<i32, BpfmanError> {
        sled_get(&self.data.db_tree, "tc_priority").map(bytes_to_i32)
    }

    pub(crate) fn set_iface(&mut self, iface: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "tc_iface", iface.as_bytes())
    }

    pub(crate) fn get_iface(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, "tc_iface").map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_proceed_on(&mut self, proceed_on: TcProceedOn) -> Result<(), BpfmanError> {
        proceed_on
            .as_action_vec()
            .iter()
            .enumerate()
            .try_for_each(|(i, v)| {
                sled_insert(
                    &self.data.db_tree,
                    format!("tc_proceed_on_{i}").as_str(),
                    &v.to_ne_bytes(),
                )
            })
    }

    pub(crate) fn get_proceed_on(&self) -> Result<TcProceedOn, BpfmanError> {
        self.data
            .db_tree
            .scan_prefix("tc_proceed_on_")
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
        sled_insert(
            &self.data.db_tree,
            "tc_current_position",
            &pos.to_ne_bytes(),
        )
    }

    pub(crate) fn get_current_position(&self) -> Result<Option<usize>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "tc_current_position")?.map(bytes_to_usize))
    }

    pub(crate) fn set_if_index(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "tc_if_index", &if_index.to_ne_bytes())
    }

    pub(crate) fn get_if_index(&self) -> Result<Option<u32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "tc_if_index")?.map(bytes_to_u32))
    }

    pub(crate) fn set_attached(&mut self, attached: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "tc_attached",
            &(attached as i8).to_ne_bytes(),
        )
    }

    pub(crate) fn get_attached(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "tc_attached")?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_direction(&mut self, direction: Direction) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "tc_direction",
            direction.to_string().as_bytes(),
        )
    }

    pub(crate) fn get_direction(&self) -> Result<Direction, BpfmanError> {
        sled_get(&self.data.db_tree, "tc_direction")
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
pub(crate) struct TracepointProgram {
    pub(crate) data: ProgramData,
}

impl TracepointProgram {
    pub(crate) fn new(data: ProgramData, tracepoint: String) -> Result<Self, BpfmanError> {
        let mut tp_prog = Self { data };
        tp_prog.set_tracepoint(tracepoint)?;
        tp_prog.get_data_mut().set_kind(ProgramType::Tracepoint)?;

        Ok(tp_prog)
    }

    pub(crate) fn set_tracepoint(&mut self, tracepoint: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "tracepoint_name", tracepoint.as_bytes())
    }

    pub(crate) fn get_tracepoint(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, "tracepoint_name").map(|v| bytes_to_string(&v))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub(crate) struct KprobeProgram {
    pub(crate) data: ProgramData,
}

impl KprobeProgram {
    pub(crate) fn new(
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
        sled_insert(&self.data.db_tree, "kprobe_fn_name", fn_name.as_bytes())
    }

    pub(crate) fn get_fn_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, "kprobe_fn_name").map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_offset(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "kprobe_offset", &offset.to_ne_bytes())
    }

    pub(crate) fn get_offset(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, "kprobe_offset").map(bytes_to_u64)
    }

    pub(crate) fn set_retprobe(&mut self, retprobe: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "kprobe_retprobe",
            &(retprobe as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_retprobe(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "kprobe_retprobe")?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_container_pid(&mut self, container_pid: i32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "kprobe_container_pid",
            &container_pid.to_ne_bytes(),
        )
    }

    pub(crate) fn get_container_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "kprobe_container_pid")?.map(bytes_to_i32))
    }

    pub(crate) fn get_data(&self) -> &ProgramData {
        &self.data
    }

    pub(crate) fn get_data_mut(&mut self) -> &mut ProgramData {
        &mut self.data
    }
}

#[derive(Debug, Clone)]
pub(crate) struct UprobeProgram {
    pub(crate) data: ProgramData,
}

impl UprobeProgram {
    pub(crate) fn new(
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
        sled_insert(&self.data.db_tree, "uprobe_fn_name", fn_name.as_bytes())
    }

    pub(crate) fn get_fn_name(&self) -> Result<Option<String>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "uprobe_fn_name")?.map(|v| bytes_to_string(&v)))
    }

    pub(crate) fn set_offset(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "uprobe_offset", &offset.to_ne_bytes())
    }

    pub(crate) fn get_offset(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.data.db_tree, "uprobe_offset").map(bytes_to_u64)
    }

    pub(crate) fn set_retprobe(&mut self, retprobe: bool) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "uprobe_retprobe",
            &(retprobe as i8 % 2).to_ne_bytes(),
        )
    }

    pub(crate) fn get_retprobe(&self) -> Result<bool, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "uprobe_retprobe")?
            .map(bytes_to_bool)
            .unwrap_or(false))
    }

    pub(crate) fn set_container_pid(&mut self, container_pid: i32) -> Result<(), BpfmanError> {
        sled_insert(
            &self.data.db_tree,
            "uprobe_container_pid",
            &container_pid.to_ne_bytes(),
        )
    }

    pub(crate) fn get_container_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "uprobe_container_pid")?.map(bytes_to_i32))
    }

    pub(crate) fn set_pid(&mut self, pid: i32) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "uprobe_pid", &pid.to_ne_bytes())
    }

    pub(crate) fn get_pid(&self) -> Result<Option<i32>, BpfmanError> {
        Ok(sled_get_option(&self.data.db_tree, "uprobe_pid")?.map(bytes_to_i32))
    }

    pub(crate) fn set_target(&mut self, target: String) -> Result<(), BpfmanError> {
        sled_insert(&self.data.db_tree, "uprobe_target", target.as_bytes())
    }

    pub(crate) fn get_target(&self) -> Result<String, BpfmanError> {
        sled_get(&self.data.db_tree, "uprobe_target").map(|v| bytes_to_string(&v))
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

    pub(crate) fn delete(&self) -> Result<(), anyhow::Error> {
        let id = self.get_data().get_id()?;
        ROOT_DB.drop_tree(id.to_string())?;

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
            Program::Unsupported(p) => p,
        }
    }

    pub(crate) fn new_from_db(id: u32, tree: sled::Tree) -> Result<Self, BpfmanError> {
        let data = ProgramData::new(tree, id);

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
                    if data.db_tree.get("uprobe_offset").unwrap().is_some() {
                        Ok(Program::Uprobe(UprobeProgram { data }))
                    } else {
                        Ok(Program::Kprobe(KprobeProgram { data }))
                    }
                }
                _ => Err(BpfmanError::Error("Unsupported program type".to_string())),
            },
            None => Err(BpfmanError::Error("Unsupported program type".to_string())),
        }
    }
}

// BpfMap represents a single map pin path used by a Program.  It has to be a
// separate object because it's lifetime is slightly different from a Program.
// More specifically a BpfMap can outlive a Program if other Programs are using
// it.
#[derive(Debug, Clone)]
pub(crate) struct BpfMap {
    pub(crate) used_by: Vec<u32>,
}
