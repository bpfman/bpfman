// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, convert::TryInto, fs, io::BufReader};

use aya::{
    programs::{
        links::{FdLink, PinnedLink},
        tc,
        tc::SchedClassifierLinkId,
        Extension, PinnedProgram, SchedClassifier, TcAttachType, Xdp,
    },
    Bpf, BpfLoader,
};

use bpfd_common::*;
use log::info;
use nix::net::if_::if_nametoindex;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::server::{
    config::{Config, XdpMode},
    errors::BpfdError,
};

const DEFAULT_ACTIONS_MAP_XDP: u32 = 1 << 2 | 1 << 31; // Default is Pass and DispatcherReturn
const DEFAULT_ACTIONS_MAP_TC: u32 = 1 << 3; // TC_ACT_PIPE;
const DEFAULT_PRIORITY: u32 = 50;
const MIN_TC_DISPATCHER_PRIORITY: u16 = 50;
const MAX_TC_DISPATCHER_PRIORITY: u16 = 45;
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";
const SUPERUSER: &str = "bpfctl";

#[derive(Debug, Eq, Ord, PartialEq, PartialOrd, Serialize, Deserialize)]
pub(crate) struct Metadata {
    priority: i32,
    name: String,
    attached: bool,
}

#[derive(Serialize, Deserialize)]
pub(crate) struct ExtensionProgram {
    path: String,
    #[serde(skip)]
    current_position: Option<usize>,
    metadata: Metadata,
    owner: String,
    proceed_on: Vec<i32>,
}

impl ExtensionProgram {
    fn proceed_on_mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        if self.proceed_on.is_empty() {
            proceed_on_mask = DEFAULT_ACTIONS_MAP_XDP;
        } else {
            for action in self.proceed_on.clone().into_iter() {
                proceed_on_mask |= 1 << action;
            }
        }
        proceed_on_mask
    }

    fn save(&self, uuid: Uuid) -> Result<(), anyhow::Error> {
        let path = format!("/var/run/bpfd/programs/{uuid}");
        serde_json::to_writer(&fs::File::create(path)?, &self)?;
        Ok(())
    }

    fn delete(&self, uuid: Uuid) -> Result<(), anyhow::Error> {
        let path = format!("/var/run/bpfd/programs/{uuid}");
        fs::remove_file(path)?;
        let path = format!("/var/run/bpfd/fs/prog_{uuid}");
        fs::remove_file(path)?;
        let path = format!("/var/run/bpfd/fs/maps/{uuid}");
        fs::remove_dir_all(path)?;

        Ok(())
    }

    fn load(uuid: Uuid) -> Result<Self, anyhow::Error> {
        let path = format!("/var/run/bpfd/programs/{uuid}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        Ok(prog)
    }
}

#[derive(Serialize, Deserialize)]
pub(crate) struct DispatcherProgram {
    mode: XdpMode,
}

impl DispatcherProgram {
    fn save(&self, if_index: u32) -> Result<(), anyhow::Error> {
        let path = format!("/var/run/bpfd/dispatchers/{if_index}");
        serde_json::to_writer(&fs::File::create(path)?, &self)?;
        Ok(())
    }

    fn _delete(&self, if_index: u32) -> Result<(), anyhow::Error> {
        let path = format!("/var/run/bpfd/dispatchers/{if_index}");
        fs::remove_file(path)?;
        Ok(())
    }

    fn load(if_index: u32) -> Result<Self, anyhow::Error> {
        let path = format!("/var/run/bpfd/dispatchers/{if_index}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        Ok(prog)
    }
}

pub(crate) struct DispatcherProgramTC {
    current_pri: u16,
    loader: Bpf,
    link_id: SchedClassifierLinkId,
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

pub(crate) struct BpfManager<'a> {
    config: &'a Config,
    dispatcher_bytes_xdp: &'a [u8],
    dispatcher_bytes_tc: &'a [u8],
    dispatchers_xdp: HashMap<u32, DispatcherProgram>,
    progs_xdp: HashMap<u32, HashMap<Uuid, ExtensionProgram>>,
    revisions_xdp: HashMap<u32, usize>,
    dispatchers_tc_in: HashMap<u32, DispatcherProgramTC>,
    progs_tc_in: HashMap<u32, HashMap<Uuid, ExtensionProgram>>,
    revisions_tc_in: HashMap<u32, usize>,
    _dispatchers_tc_eg: HashMap<u32, DispatcherProgram>,
    _progs_tc_eg: HashMap<u32, HashMap<Uuid, ExtensionProgram>>,
    _revisions_tc_eg: HashMap<u32, usize>,
}

impl<'a> BpfManager<'a> {
    pub(crate) fn new(
        config: &'a Config,
        dispatcher_bytes_xdp: &'a [u8],
        dispatcher_bytes_tc: &'a [u8],
    ) -> Self {
        Self {
            config,
            dispatcher_bytes_xdp,
            dispatcher_bytes_tc,
            dispatchers_xdp: HashMap::new(),
            progs_xdp: HashMap::new(),
            revisions_xdp: HashMap::new(),
            dispatchers_tc_in: HashMap::new(),
            progs_tc_in: HashMap::new(),
            revisions_tc_in: HashMap::new(),
            _dispatchers_tc_eg: HashMap::new(),
            _progs_tc_eg: HashMap::new(),
            _revisions_tc_eg: HashMap::new(),
        }
    }

    pub(crate) fn rebuild_state(&mut self) -> Result<(), BpfdError> {
        // 1. Check paths on bpffs
        for entry in fs::read_dir("/var/run/bpfd/fs")? {
            let entry = entry?;
            let name = entry.file_name();
            let parts: Vec<&str> = name.to_str().unwrap().split('_').collect();

            // skip files without 3 segments in name
            if parts.len() != 3 {
                continue;
            }
            match parts[2] {
                "link" => {
                    let if_index: u32 = parts[1].parse().unwrap();

                    let prog = DispatcherProgram::load(if_index).unwrap();

                    info!("rebuilding state for dispatcher on if_index {}", if_index);
                    self.dispatchers_xdp.insert(if_index, prog);
                }
                _ => {
                    let path = entry.path();
                    if path.is_dir() {
                        for entry in fs::read_dir(path)? {
                            let entry = entry?;
                            let name = entry.file_name();
                            let parts: Vec<&str> = name.to_str().unwrap().split('_').collect();
                            match parts[0] {
                                // use link to populate program and dispatcher revisions
                                "link" => {
                                    let uuid = parts[1].parse().unwrap();
                                    let mut path = entry.path();
                                    path.pop(); // remove filename
                                    let dir_name = path.file_name().unwrap();
                                    // dispatcher-{ifindex}-{revision}
                                    let dispatcher_parts: Vec<&str> =
                                        dir_name.to_str().unwrap().split('_').collect();
                                    let if_index = dispatcher_parts[1].parse().unwrap();
                                    let revision = dispatcher_parts[2].parse().unwrap();

                                    self.revisions_xdp.entry(if_index).or_insert(revision);

                                    let mut prog = ExtensionProgram::load(uuid).unwrap();
                                    prog.metadata.attached = true;

                                    info!("rebuilding state for program {uuid} on if_index {if_index}");
                                    if let Some(progs) = self.progs_xdp.get_mut(&if_index) {
                                        progs.insert(uuid, prog);
                                    } else {
                                        self.progs_xdp
                                            .insert(if_index, HashMap::from([(uuid, prog)]));
                                    }
                                    self.sort_extensions_xdp(&if_index);
                                }
                                _ => {
                                    // ignore other files on bpffs
                                    continue;
                                }
                            }
                        }
                    }
                }
            }
        }
        Ok(())
    }

    #[allow(clippy::too_many_arguments)]
    pub(crate) fn add_program(
        &mut self,
        program_type: i32,
        iface: String,
        path: String,
        priority: i32,
        section_name: String,
        proceed_on: Vec<i32>,
        owner: String,
    ) -> Result<Uuid, BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        match program_type {
            0 => {
                let id = Uuid::new_v4();
                let map_pin_path = format!("/var/run/bpfd/fs/maps/{}", id);
                fs::create_dir_all(map_pin_path.clone())?;

                let mut ext_loader = BpfLoader::new()
                    .extension(&section_name)
                    .map_pin_path(map_pin_path.clone())
                    .load_file(&path)?;

                ext_loader.program_mut(&section_name).ok_or_else(|| {
                    let _ = fs::remove_dir_all(map_pin_path);
                    BpfdError::SectionNameNotValid(section_name.clone())
                })?;

                // Calculate the next_available_id
                let next_available_id = if let Some(prog) = self.progs_xdp.get(&if_index) {
                    prog.len() + 1
                } else {
                    self.progs_xdp.insert(if_index, HashMap::new());
                    1
                };
                if next_available_id > 10 {
                    return Err(BpfdError::TooManyPrograms);
                }

                // Calculate the dispatcher revision
                let (old_revision, revision) =
                    if let Some(old_revision) = self.revisions_xdp.remove(&if_index) {
                        let r = old_revision.wrapping_add(1);
                        self.revisions_xdp.insert(if_index, r);
                        (Some(old_revision), r)
                    } else {
                        self.revisions_xdp.insert(if_index, 0);
                        (None, 0)
                    };

                let prog = ExtensionProgram {
                    path,
                    current_position: None,
                    metadata: Metadata {
                        priority,
                        name: section_name,
                        attached: false,
                    },
                    owner,
                    proceed_on,
                };
                prog.save(id)
                    .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;

                self.progs_xdp.get_mut(&if_index).unwrap().insert(id, prog);
                self.sort_extensions_xdp(&if_index);

                let mut dispatcher_loader = self.new_xdp_dispatcher(
                    &if_index,
                    next_available_id as u8,
                    self.dispatcher_bytes_xdp,
                    revision,
                )?;

                self.attach_extensions_xdp(&if_index, &mut dispatcher_loader, Some(ext_loader))?;
                self.attach_or_replace_dispatcher_xdp(iface.clone(), if_index, dispatcher_loader)?;

                if let Some(r) = old_revision {
                    self.cleanup_extensions(if_index, r)?;
                }

                info!(
                    "Program added: {} programs attached to {}",
                    self.progs_xdp.get(&if_index).unwrap().len(),
                    &iface,
                );
                Ok(id)
            }
            1 => {
                let id = Uuid::new_v4();
                let map_pin_path = format!("/var/run/bpfd/fs/maps/{}", id);
                fs::create_dir_all(map_pin_path.clone())?;

                // Add clsact qdisc to the interface. This is harmless if it has already been added.
                let _ = tc::qdisc_add_clsact(&iface);

                let mut ext_loader = BpfLoader::new()
                    .extension(&section_name)
                    .map_pin_path(map_pin_path.clone())
                    .load_file(&path)?;

                ext_loader.program_mut(&section_name).ok_or_else(|| {
                    let _ = fs::remove_dir_all(map_pin_path);
                    BpfdError::SectionNameNotValid(section_name.clone())
                })?;

                // Calculate the next_available_id
                let next_available_id = if let Some(prog) = self.progs_tc_in.get(&if_index) {
                    prog.len() + 1
                } else {
                    self.progs_tc_in.insert(if_index, HashMap::new());
                    1
                };
                if next_available_id > 10 {
                    return Err(BpfdError::TooManyPrograms);
                }

                // Calculate the dispatcher revision
                let (old_revision, revision) =
                    if let Some(old_revision) = self.revisions_tc_in.remove(&if_index) {
                        let r = old_revision.wrapping_add(1);
                        self.revisions_tc_in.insert(if_index, r);
                        (Some(old_revision), r)
                    } else {
                        self.revisions_tc_in.insert(if_index, 0);
                        (None, 0)
                    };

                let prog = ExtensionProgram {
                    path,
                    current_position: None,
                    metadata: Metadata {
                        priority,
                        name: section_name,
                        attached: false,
                    },
                    owner,
                    proceed_on,
                };
                prog.save(id)
                    .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;

                self.progs_tc_in
                    .get_mut(&if_index)
                    .unwrap()
                    .insert(id, prog);
                self.sort_extensions_tc(&if_index);

                let mut dispatcher_loader = self.new_tc_dispatcher(
                    &if_index,
                    next_available_id as u8,
                    self.dispatcher_bytes_tc,
                    revision,
                )?;

                self.attach_extensions_tc_in(&if_index, &mut dispatcher_loader, Some(ext_loader))?;

                self.attach_or_replace_dispatcher_tc_in(
                    iface.clone(),
                    if_index,
                    dispatcher_loader,
                )?;

                if let Some(r) = old_revision {
                    self.cleanup_extensions(if_index, r)?;
                }

                info!(
                    "Program added: {} programs attached to {}",
                    self.progs_tc_in.get(&if_index).unwrap().len(),
                    &iface,
                );
                Ok(id)
            }
            2 => Err(BpfdError::UnsuportedProgramType),
            _ => Err(BpfdError::UnsuportedProgramType),
        }
    }

    // ANF-TODO: Fix this to work for multiple program types.
    // ANF-TODO: User may need to pass in the program type to remove, or we may need to
    //           search all "progs" hashmaps for the program.
    pub(crate) fn remove_program(
        &mut self,
        id: Uuid,
        iface: String,
        owner: String,
    ) -> Result<(), BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if let Some(programs) = self.progs_tc_in.get_mut(&if_index) {
            if let Some(prog) = programs.get(&id) {
                if !(prog.owner == owner || owner == SUPERUSER) {
                    return Err(BpfdError::NotAuthorized);
                }
            } else {
                return Err(BpfdError::InvalidID);
            }

            let prog = programs.remove(&id).unwrap();
            prog.delete(id)
                .map_err(|_| BpfdError::Error("unable to delete program data".to_string()))?;

            let old_revision = self.revisions_tc_in.remove(&if_index).unwrap();

            if programs.is_empty() {
                self.progs_tc_in.remove(&if_index);
                self.dispatchers_tc_in.remove(&if_index);
                self.cleanup_extensions(if_index, old_revision)?;
                return Ok(());
            }

            // New dispatcher required: calculate the new dispatcher revision
            let revision = old_revision.wrapping_add(1);
            self.revisions_tc_in.insert(if_index, revision);

            // Cache program length so programs goes out of scope and
            // sort_extensions() can generate its own list.
            let program_len = programs.len() as u8;
            self.sort_extensions_tc(&if_index);

            let mut dispatcher_loader =
                self.new_tc_dispatcher(&if_index, program_len, self.dispatcher_bytes_tc, revision)?;

            self.attach_extensions_tc_in(&if_index, &mut dispatcher_loader, None)?;

            self.attach_or_replace_dispatcher_tc_in(iface, if_index, dispatcher_loader)?;

            self.cleanup_extensions(if_index, old_revision)?;
        }
        Ok(())
    }

    pub(crate) fn _remove_program_xdp(
        &mut self,
        id: Uuid,
        iface: String,
        owner: String,
    ) -> Result<(), BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if let Some(programs) = self.progs_xdp.get_mut(&if_index) {
            if let Some(prog) = programs.get(&id) {
                if !(prog.owner == owner || owner == SUPERUSER) {
                    return Err(BpfdError::NotAuthorized);
                }
            } else {
                return Err(BpfdError::InvalidID);
            }

            let prog = programs.remove(&id).unwrap();
            prog.delete(id)
                .map_err(|_| BpfdError::Error("unable to delete program data".to_string()))?;

            if programs.is_empty() {
                self.progs_xdp.remove(&if_index);
                let old = self.dispatchers_xdp.remove(&if_index);
                if let Some(old) = old {
                    let rev = self.revisions_xdp.remove(&if_index).unwrap();
                    self._delete_link(if_index, rev)?;
                    old._delete(if_index).map_err(|_| {
                        BpfdError::Error("unable to delete persisted dispatcher data".to_string())
                    })?;
                }
                return Ok(());
            }

            // New dispatcher required: calculate the new dispatcher revision
            let old_revision = self.revisions_xdp.remove(&if_index).unwrap();
            let revision = old_revision.wrapping_add(1);
            self.revisions_xdp.insert(if_index, revision);

            // Cache program length so programs goes out of scope and
            // sort_extensions() can generate its own list.
            let program_len = programs.len() as u8;
            self.sort_extensions_xdp(&if_index);

            let mut dispatcher_loader = self.new_xdp_dispatcher(
                &if_index,
                program_len,
                self.dispatcher_bytes_xdp,
                revision,
            )?;

            self.attach_extensions_xdp(&if_index, &mut dispatcher_loader, None)?;

            self.attach_or_replace_dispatcher_xdp(iface, if_index, dispatcher_loader)?;

            self.cleanup_extensions(if_index, old_revision)?;
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self, iface: String) -> Result<InterfaceInfo, BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if !self.dispatchers_tc_in.contains_key(&if_index) {
            return Err(BpfdError::NoProgramsLoaded);
        };
        let mut results = InterfaceInfo {
            xdp_mode: String::from("tc"),
            programs: vec![],
        };
        let mut extensions = self
            .progs_tc_in
            .get(&if_index)
            .unwrap()
            .iter()
            .collect::<Vec<_>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (id, v) in extensions.iter() {
            results.programs.push(ProgramInfo {
                id: id.to_string(),
                name: v.metadata.name.clone(),
                path: v.path.clone(),
                position: v.current_position.unwrap(),
                priority: v.metadata.priority,
                proceed_on: v.proceed_on.clone(),
            })
        }
        Ok(results)
    }

    pub(crate) fn _list_programs_xdp(&mut self, iface: String) -> Result<InterfaceInfo, BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if !self.dispatchers_xdp.contains_key(&if_index) {
            return Err(BpfdError::NoProgramsLoaded);
        };
        let xdp_mode = self
            .dispatchers_xdp
            .get(&if_index)
            .unwrap()
            .mode
            .to_string();
        let mut results = InterfaceInfo {
            xdp_mode,
            programs: vec![],
        };
        let mut extensions = self
            .progs_xdp
            .get(&if_index)
            .unwrap()
            .iter()
            .collect::<Vec<_>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (id, v) in extensions.iter() {
            results.programs.push(ProgramInfo {
                id: id.to_string(),
                name: v.metadata.name.clone(),
                path: v.path.clone(),
                position: v.current_position.unwrap(),
                priority: v.metadata.priority,
                proceed_on: v.proceed_on.clone(),
            })
        }
        Ok(results)
    }

    fn attach_extensions_xdp(
        &mut self,
        if_index: &u32,
        dispatcher_loader: &mut Bpf,
        mut ext_loader: Option<Bpf>,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        let revision = self.revisions_xdp.get(if_index).unwrap();
        let mut extensions = self
            .progs_xdp
            .get_mut(if_index)
            .unwrap()
            .iter_mut()
            .collect::<Vec<(&Uuid, &mut ExtensionProgram)>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (i, (k, v)) in extensions.iter_mut().enumerate() {
            if v.metadata.attached {
                let mut prog = PinnedProgram::from_pin(format!("/var/run/bpfd/fs/prog_{k}"))?;
                let ext: &mut Extension = prog.as_mut().try_into()?;
                let target_fn = format!("prog{}", i);
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_{revision}/link_{k}");
                new_link.pin(path).map_err(|_| BpfdError::UnableToPin)?;
            } else {
                let ext: &mut Extension = ext_loader
                    .as_mut()
                    .unwrap()
                    .program_mut(&v.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{}", i);

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                ext.pin(format!("/var/run/bpfd/fs/prog_{k}"))
                    .map_err(|_| BpfdError::UnableToPin)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                fd_link
                    .pin(format!(
                        "/var/run/bpfd/fs/dispatcher_{if_index}_{revision}/link_{k}"
                    ))
                    .map_err(|_| BpfdError::UnableToPin)?;
                v.metadata.attached = true;
            }
        }
        Ok(())
    }

    fn attach_extensions_tc_in(
        &mut self,
        if_index: &u32,
        dispatcher_loader: &mut Bpf,
        mut ext_loader: Option<Bpf>,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut SchedClassifier = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        let revision = self.revisions_tc_in.get(if_index).unwrap();
        let mut extensions = self
            .progs_tc_in
            .get_mut(if_index)
            .unwrap()
            .iter_mut()
            .collect::<Vec<(&Uuid, &mut ExtensionProgram)>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (i, (k, v)) in extensions.iter_mut().enumerate() {
            if v.metadata.attached {
                let mut prog = PinnedProgram::from_pin(format!("/var/run/bpfd/fs/prog_{k}"))?;
                let ext: &mut Extension = prog.as_mut().try_into()?;
                let target_fn = format!("prog{}", i);
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_{revision}/link_{k}");
                new_link.pin(path).map_err(|_| BpfdError::UnableToPin)?;
            } else {
                let ext: &mut Extension = ext_loader
                    .as_mut()
                    .unwrap()
                    .program_mut(&v.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{}", i);

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                ext.pin(format!("/var/run/bpfd/fs/prog_{k}"))
                    .map_err(|_| BpfdError::UnableToPin)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                fd_link
                    .pin(format!(
                        "/var/run/bpfd/fs/dispatcher_{if_index}_{revision}/link_{k}"
                    ))
                    .map_err(|_| BpfdError::UnableToPin)?;
                v.metadata.attached = true;
            }
        }
        Ok(())
    }

    fn attach_or_replace_dispatcher_xdp(
        &mut self,
        iface: String,
        if_index: u32,
        mut dispatcher_loader: Bpf,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        if let Some(d) = self.dispatchers_xdp.remove(&if_index) {
            let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_link");
            let pinned_link: FdLink = PinnedLink::from_pin(path).unwrap().into();
            dispatcher
                .attach_to_link(pinned_link.try_into().unwrap())
                .unwrap();
            self.dispatchers_xdp
                .insert(if_index, DispatcherProgram { mode: d.mode });
        } else {
            let mode = if let Some(i) = &self.config.interfaces {
                i.get(&iface).map_or(XdpMode::Skb, |i| i.xdp_mode)
            } else {
                XdpMode::Skb
            };
            let flags = mode.as_flags();
            let link = dispatcher.attach(&iface, flags).unwrap();
            let owned_link = dispatcher.take_link(link)?;
            let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_link");
            let _ = TryInto::<FdLink>::try_into(owned_link)
                .unwrap() // TODO: Don't unwrap, although due to minimum kernel version this shouldn't ever panic
                .pin(path)
                .map_err(|_| BpfdError::UnableToPin)?;
            let p = DispatcherProgram { mode };
            p.save(if_index)
                .map_err(|_| BpfdError::Error("unable to persist dispatcher data".to_string()))?;

            self.dispatchers_xdp.insert(if_index, p);
        }
        Ok(())
    }

    fn attach_or_replace_dispatcher_tc_in(
        &mut self,
        iface: String,
        if_index: u32,
        mut new_dispatcher_loader: Bpf,
    ) -> Result<(), BpfdError> {
        let new_dispatcher: &mut SchedClassifier = new_dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        match self.dispatchers_tc_in.remove(&if_index) {
            Some(mut dp) => {
                let new_dispatcher_priority = dp.current_pri - 1;

                let new_link_id = new_dispatcher.attach(
                    &iface,
                    TcAttachType::Ingress,
                    new_dispatcher_priority,
                )?;

                if new_dispatcher_priority > MAX_TC_DISPATCHER_PRIORITY {
                    self.dispatchers_tc_in.insert(
                        if_index,
                        DispatcherProgramTC {
                            current_pri: new_dispatcher_priority,
                            loader: new_dispatcher_loader,
                            link_id: new_link_id,
                        },
                    );
                } else {
                    // We need to wrap priorities while preserving atomic replacement behavior

                    let old_dispatcher: &mut SchedClassifier = dp
                        .loader
                        .program_mut(DISPATCHER_PROGRAM_NAME)
                        .unwrap()
                        .try_into()?;

                    // Manually detach the old dispatcher
                    old_dispatcher.detach(dp.link_id)?;

                    let new_dispatcher_priority = MIN_TC_DISPATCHER_PRIORITY;

                    // Attach the new scheduler at the lowest priority
                    let new_link_id_2 = new_dispatcher.attach(
                        &iface,
                        TcAttachType::Ingress,
                        new_dispatcher_priority,
                    )?;

                    // Manually detach the new dispatcher that's at the highest priority
                    new_dispatcher.detach(new_link_id)?;

                    self.dispatchers_tc_in.insert(
                        if_index,
                        DispatcherProgramTC {
                            current_pri: new_dispatcher_priority,
                            loader: new_dispatcher_loader,
                            link_id: new_link_id_2,
                        },
                    );
                }
            }
            _ => {
                // This is the first tc dispatcher on this interface
                let new_link_id = new_dispatcher.attach(
                    &iface,
                    TcAttachType::Ingress,
                    MIN_TC_DISPATCHER_PRIORITY,
                )?;

                self.dispatchers_tc_in.insert(
                    if_index,
                    DispatcherProgramTC {
                        current_pri: MIN_TC_DISPATCHER_PRIORITY,
                        loader: new_dispatcher_loader,
                        link_id: new_link_id,
                    },
                );
            }
        }
        Ok(())
    }

    fn cleanup_extensions(&self, if_index: u32, revision: usize) -> Result<(), BpfdError> {
        let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_{revision}");
        fs::remove_dir_all(path).map_err(|io_error| BpfdError::UnableToCleanup { io_error })
    }

    fn _delete_link(&self, if_index: u32, revision: usize) -> Result<(), BpfdError> {
        let path_link = format!("/var/run/bpfd/fs/dispatcher_{if_index}_link");
        fs::remove_file(path_link)?;
        let path_link_rev = format!("/var/run/bpfd/fs/dispatcher_{if_index}_{revision}/");
        fs::remove_dir_all(path_link_rev)
            .map_err(|io_error| BpfdError::UnableToCleanup { io_error })
    }

    fn get_ifindex(&mut self, iface: &str) -> Result<u32, BpfdError> {
        match if_nametoindex(iface) {
            Ok(index) => {
                info!("Map {} to {}", iface, index);
                Ok(index)
            }
            Err(_) => {
                info!("Unable to validate interface {}", iface);
                Err(BpfdError::InvalidInterface)
            }
        }
    }

    fn sort_extensions_xdp(&mut self, if_index: &u32) {
        let mut extensions = self
            .progs_xdp
            .get_mut(if_index)
            .unwrap()
            .values_mut()
            .collect::<Vec<&mut ExtensionProgram>>();
        extensions.sort_by(|a, b| a.metadata.cmp(&b.metadata));
        for (i, v) in extensions.iter_mut().enumerate() {
            v.current_position = Some(i);
        }
    }

    fn sort_extensions_tc(&mut self, if_index: &u32) {
        let mut extensions = self
            .progs_tc_in
            .get_mut(if_index)
            .unwrap()
            .values_mut()
            .collect::<Vec<&mut ExtensionProgram>>();
        extensions.sort_by(|a, b| a.metadata.cmp(&b.metadata));
        for (i, v) in extensions.iter_mut().enumerate() {
            v.current_position = Some(i);
        }
    }

    fn new_xdp_dispatcher(
        &mut self,
        if_index: &u32,
        num_progs_enabled: u8,
        bytes: &[u8],
        revision: usize,
    ) -> Result<Bpf, BpfdError> {
        let mut chain_call_actions = [DEFAULT_ACTIONS_MAP_XDP; 10];

        let mut extensions = self
            .progs_xdp
            .get(if_index)
            .unwrap()
            .iter()
            .collect::<Vec<_>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (_, v) in extensions.iter() {
            chain_call_actions[v.current_position.unwrap()] = v.proceed_on_mask();
        }

        let config = XdpDispatcherConfig {
            num_progs_enabled,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        let mut dispatcher_loader = BpfLoader::new().set_global("CONFIG", &config).load(bytes)?;

        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        dispatcher.load()?;

        let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        Ok(dispatcher_loader)
    }

    fn new_tc_dispatcher(
        &mut self,
        if_index: &u32,
        num_progs_enabled: u8,
        bytes: &[u8],
        revision: usize,
    ) -> Result<Bpf, BpfdError> {
        let mut chain_call_actions = [DEFAULT_ACTIONS_MAP_TC; 10];

        let mut extensions = self
            .progs_tc_in
            .get(if_index)
            .unwrap()
            .iter()
            .collect::<Vec<_>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (_, v) in extensions.iter() {
            chain_call_actions[v.current_position.unwrap()] = DEFAULT_ACTIONS_MAP_TC
            // TODO: update v.proceed_on_mask();
        }

        let config = TcDispatcherConfig {
            num_progs_enabled,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        let mut dispatcher_loader = BpfLoader::new()
            .set_global("TC_CONFIG", &config)
            .load(bytes)?;

        let dispatcher: &mut SchedClassifier = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        dispatcher.load()?;

        let path = format!("/var/run/bpfd/fs/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        Ok(dispatcher_loader)
    }
}
