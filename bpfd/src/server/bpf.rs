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
use bpfd_api::{
    config::{Config, XdpMode},
    util::directories::{RTDIR_DISPATCHER, RTDIR_FS, RTDIR_FS_MAPS},
};
use bpfd_common::*;
use log::info;
use nix::net::if_::if_nametoindex;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::server::{
    command::{
        Direction, InterfaceInfo, NetworkMultiAttachInfo, Program, ProgramData, ProgramType,
        DEFAULT_ACTIONS_MAP_TC, DEFAULT_XDP_ACTIONS_MAP, DEFAULT_XDP_PROCEED_ON_DISPATCHER_RETURN,
        DEFAULT_XDP_PROCEED_ON_PASS,
    },
    errors::BpfdError,
};

const DEFAULT_PRIORITY: u32 = 50;
const MIN_TC_DISPATCHER_PRIORITY: u16 = 50;
const MAX_TC_DISPATCHER_PRIORITY: u16 = 45;
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";
const SUPERUSER: &str = "bpfctl";

#[derive(Debug, Serialize, Deserialize)]
pub(crate) struct XdpDispatcher {
    revision: usize,
    mode: XdpMode,
}

impl XdpDispatcher {
    fn save(&self, if_index: u32) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_DISPATCHER}/{if_index}");
        serde_json::to_writer(&fs::File::create(path)?, &self)?;
        Ok(())
    }

    fn delete(&self, if_index: u32) -> Result<(), anyhow::Error> {
        let path = format!("{RTDIR_DISPATCHER}/{if_index}");
        fs::remove_file(path)?;
        Ok(())
    }

    fn load(if_index: u32) -> Result<Self, anyhow::Error> {
        let path = format!("{RTDIR_DISPATCHER}/{if_index}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        Ok(prog)
    }
}

#[derive(Debug)]
pub(crate) struct TcDispatcher {
    revision: usize,
    current_pri: u16,
    loader: Bpf,
    link_id: SchedClassifierLinkId,
}

pub(crate) enum Dispatcher {
    Xdp(XdpDispatcher),
    Tc(TcDispatcher),
}

pub(crate) struct BpfManager<'a> {
    config: &'a Config,
    dispatcher_bytes_xdp: &'a [u8],
    dispatcher_bytes_tc: &'a [u8],
    dispatchers: HashMap<DispatcherId, Dispatcher>,
    programs: HashMap<Uuid, Program>,
}

#[derive(Debug, Hash, Eq, PartialEq)]
pub enum DispatcherId {
    Xdp(DispatcherInfo),
    Tc(DispatcherInfo),
}

#[derive(Debug, Hash, Eq, PartialEq)]
pub struct DispatcherInfo(u32, Option<Direction>);

impl<'a> BpfManager<'a> {
    pub(crate) fn new(
        config: &'a Config,
        dispatcher_bytes_xdp: &'a [u8],
        dispatcher_bytes_tc: &'a [u8],
    ) -> Self {
        Self {
            config,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
            dispatcher_bytes_xdp,
            dispatcher_bytes_tc,
        }
    }

    pub(crate) fn rebuild_state(&mut self) -> Result<(), BpfdError> {
        // 1. Check paths on bpffs
        for entry in fs::read_dir(RTDIR_FS)? {
            let entry = entry?;
            let name = entry.file_name();
            let parts: Vec<&str> = name.to_str().unwrap().split('_').collect();

            // skip files without 3 segments in name
            if parts.len() != 3 {
                continue;
            }
            match parts[2] {
                "link" => {
                    let ifindex: u32 = parts[1].parse().unwrap();

                    let prog = XdpDispatcher::load(ifindex).unwrap();

                    info!("rebuilding state for dispatcher on if_index {}", ifindex);
                    self.dispatchers.insert(
                        DispatcherId::Xdp(DispatcherInfo(ifindex, None)),
                        Dispatcher::Xdp(prog),
                    );
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
                                    // dispatcher-{if_index}-{revision}
                                    let dispatcher_parts: Vec<&str> =
                                        dir_name.to_str().unwrap().split('_').collect();
                                    let if_index = dispatcher_parts[1].parse().unwrap();

                                    let revision: usize = dispatcher_parts[2].parse().unwrap();
                                    if let Some(Dispatcher::Xdp(d)) = self
                                        .dispatchers
                                        .get_mut(&DispatcherId::Xdp(DispatcherInfo(if_index, None)))
                                    {
                                        d.revision = revision;
                                    }
                                    let mut prog = Program::load(uuid).unwrap();
                                    info!("rebuilding state for program {uuid} on if_index {if_index}");
                                    prog.set_attached();
                                    self.programs.insert(uuid, prog);
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
        program: crate::server::command::Program,
    ) -> Result<Uuid, BpfdError> {
        match program {
            crate::server::command::Program::Xdp(_, _) => self.add_program_xdp(program),
            crate::server::command::Program::Tc(_, _, _) => self.add_program_tc(program),
            _ => Err(BpfdError::UnsuportedProgramType),
        }
    }

    fn add_program_xdp(&mut self, p: Program) -> Result<Uuid, BpfdError> {
        if let Program::Xdp(ref data, ref info) = p {
            let ifindex = info.if_index;
            let iface = info.if_name.clone();
            let id = Uuid::new_v4();
            let map_pin_path = format!("{RTDIR_FS_MAPS}/{id}");
            fs::create_dir_all(map_pin_path.clone())?;

            let mut ext_loader = BpfLoader::new()
                .extension(&data.section_name)
                .map_pin_path(map_pin_path.clone())
                .load_file(&data.path)?;

            ext_loader.program_mut(&data.section_name).ok_or_else(|| {
                let _ = fs::remove_dir_all(map_pin_path);
                BpfdError::SectionNameNotValid(data.section_name.clone())
            })?;

            // Calculate the next_available_id
            let next_available_id = self
                .programs
                .iter()
                .filter(|(_, p)| {
                    if let Program::Xdp(_, i) = p {
                        i.if_index == info.if_index
                    } else {
                        false
                    }
                })
                .collect::<HashMap<_, _>>()
                .len();
            if next_available_id > 10 {
                return Err(BpfdError::TooManyPrograms);
            }

            // Calculate the dispatcher revision
            let (old_revision, revision) = if let Some(Dispatcher::Xdp(d)) = self
                .dispatchers
                .get(&DispatcherId::Xdp(DispatcherInfo(info.if_index, None)))
            {
                (Some(d.revision), d.revision.wrapping_add(1))
            } else {
                (None, 0)
            };

            let proceed_on = if info.proceed_on.is_empty() {
                vec![
                    DEFAULT_XDP_PROCEED_ON_PASS,
                    DEFAULT_XDP_PROCEED_ON_DISPATCHER_RETURN,
                ]
            } else {
                info.proceed_on.clone()
            };

            p.save(id)
                .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;

            self.programs.insert(id, p);
            self.sort_extensions_xdp(&ifindex);

            let mut dispatcher_loader = self.new_xdp_dispatcher(
                &ifindex,
                next_available_id as u8,
                self.dispatcher_bytes_xdp,
                revision,
            )?;

            self.attach_extensions_xdp(
                &ifindex,
                &mut dispatcher_loader,
                Some(ext_loader),
                revision,
            )?;
            self.attach_or_replace_dispatcher_xdp(
                iface.clone(),
                ifindex,
                dispatcher_loader,
                revision,
            )?;

            if let Some(r) = old_revision {
                self.cleanup_extensions_xdp(ifindex, r)?;
            }

            info!(
                "Program added: {} programs attached to {}",
                self.programs
                    .iter()
                    .filter(|(_, p)| if let Program::Xdp(_, info) = p {
                        info.if_index == ifindex
                    } else {
                        false
                    })
                    .collect::<HashMap<_, _>>()
                    .len(),
                &iface,
            );
            Ok(id)
        } else {
            unreachable!()
        }
    }

    #[allow(clippy::too_many_arguments)]
    fn add_program_tc(&mut self, p: Program) -> Result<Uuid, BpfdError> {
        if let Program::Tc(ref data, ref info, ref direction) = p {
            let ifindex = info.if_index;
            let iface = info.if_name.clone();
            let id = Uuid::new_v4();
            let map_pin_path = format!("/var/run/bpfd/fs/maps/{id}");
            fs::create_dir_all(map_pin_path.clone())?;

            // Add clsact qdisc to the interface. This is harmless if it has already been added.
            let _ = tc::qdisc_add_clsact(&info.if_name);

            let mut ext_loader = BpfLoader::new()
                .extension(&data.section_name)
                .map_pin_path(map_pin_path.clone())
                .load_file(&data.path)?;

            ext_loader.program_mut(&data.section_name).ok_or_else(|| {
                let _ = fs::remove_dir_all(map_pin_path);
                BpfdError::SectionNameNotValid(data.section_name.clone())
            })?;

            let direction = info.direction.unwrap();

            // Calculate the next_available_id
            let next_available_id = self
                .programs
                .iter()
                .filter(|(_, p)| {
                    if let Program::Tc(_, i, dir) = p {
                        info.if_index == i.if_index && direction == *dir
                    } else {
                        false
                    }
                })
                .collect::<HashMap<_, _>>()
                .len();
            if next_available_id > 10 {
                return Err(BpfdError::TooManyPrograms);
            }

            // Calculate the dispatcher revision
            let (old_revision, revision) = if let Some(Dispatcher::Tc(d)) = self
                .dispatchers
                .get(&DispatcherId::Tc(DispatcherInfo(ifindex, Some(direction))))
            {
                (Some(d.revision), d.revision.wrapping_add(1))
            } else {
                (None, 0)
            };
            p.save(id)
                .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;
            self.programs.insert(id, p);
            self.sort_extensions_tc(direction, &ifindex);

            let mut dispatcher_loader = self.new_tc_dispatcher(
                direction,
                &ifindex,
                next_available_id as u8,
                self.dispatcher_bytes_tc,
                revision,
            )?;

            self.attach_extensions_tc(
                direction,
                &ifindex,
                &mut dispatcher_loader,
                Some(ext_loader),
                revision,
            )?;

            self.attach_or_replace_dispatcher_tc(
                direction,
                iface.clone(),
                ifindex,
                dispatcher_loader,
                revision,
            )?;

            if let Some(r) = old_revision {
                self.cleanup_extensions_tc(direction, ifindex, r)?;
            }

            info!(
                "Program added: {} programs attached to {}",
                next_available_id, &iface,
            );
            Ok(id)
        } else {
            unreachable!()
        }
    }

    pub(crate) fn add_single_attach_program(
        &self,
        _path: String,
        _program_type: ProgramType,
        _section_name: String,
        _attach: String,
        _username: String,
    ) -> Result<Uuid, BpfdError> {
        unimplemented!("todo")
    }

    pub(crate) fn remove_program(&mut self, id: Uuid, owner: String) -> Result<(), BpfdError> {
        if let Some(prog) = self.programs.get(&id) {
            if !(prog.owner() == &owner || owner == SUPERUSER) {
                return Err(BpfdError::NotAuthorized);
            }
        } else {
            return Err(BpfdError::InvalidID);
        }

        let prog = self.programs.remove(&id).unwrap();

        prog.delete(id)
            .map_err(|_| BpfdError::Error("unable to delete program data".to_string()))?;

        match prog {
            Program::Xdp(_, _) => self.remove_program_xdp(&prog),
            Program::Tc(_, _, _) => self.remove_program_tc(&prog),
            Program::Tracepoint(_, _) => Ok(()),
        }?;
        Ok(())
    }

    fn remove_program_xdp(&mut self, program: &Program) -> Result<(), BpfdError> {
        if let Program::Xdp(data, info) = program {
            let if_index = info.if_index;
            let iface = info.if_name.clone();
            let attached_programs = self
                .programs
                .iter()
                .filter(|(_, p)| {
                    if let Program::Xdp(_, i) = p {
                        i.if_index == info.if_index
                    } else {
                        false
                    }
                })
                .collect::<HashMap<_, _>>()
                .len();

            if attached_programs == 0 {
                let old = self
                    .dispatchers
                    .remove(&DispatcherId::Xdp(DispatcherInfo(if_index, None)));
                if let Some(Dispatcher::Xdp(old)) = old {
                    self.delete_link_xdp(if_index, old.revision)?;
                    old.delete(if_index).map_err(|_| {
                        BpfdError::Error("unable to delete persisted dispatcher data".to_string())
                    })?;
                }
                return Ok(());
            }

            // New dispatcher required: calculate the new dispatcher revision
            if let Dispatcher::Xdp(old_dispatcher) = self
                .dispatchers
                .get(&DispatcherId::Xdp(DispatcherInfo(if_index, None)))
                .unwrap()
            {
                let old_revision = old_dispatcher.revision;
                let revision = old_revision.wrapping_add(1);

                self.sort_extensions_xdp(&if_index);

                let mut dispatcher_loader = self.new_xdp_dispatcher(
                    &if_index,
                    attached_programs as u8,
                    self.dispatcher_bytes_xdp,
                    revision,
                )?;

                self.attach_extensions_xdp(&if_index, &mut dispatcher_loader, None, revision)?;

                self.attach_or_replace_dispatcher_xdp(
                    iface,
                    if_index,
                    dispatcher_loader,
                    revision,
                )?;

                self.cleanup_extensions_xdp(if_index, old_revision)?;
            } else {
                unreachable!()
            }
            Ok(())
        } else {
            unreachable!()
        }
    }

    fn remove_program_tc(&mut self, program: &Program) -> Result<(), BpfdError> {
        if let Program::Tc(data, info, direction) = program {
            let if_index = info.if_index;
            let iface = info.if_name.clone();
            let direction = *direction;

            let attached_programs = self
                .programs
                .iter()
                .filter(|(_, p)| {
                    if let Program::Tc(_, i, dir) = p {
                        i.if_index == if_index && dir == &direction
                    } else {
                        false
                    }
                })
                .collect::<HashMap<_, _>>()
                .len();

            let old = self
                .dispatchers
                .remove(&DispatcherId::Xdp(DispatcherInfo(if_index, None)));
            if let Some(Dispatcher::Tc(old)) = old {
                self.cleanup_extensions_tc(direction, if_index, old.revision)?;
                return Ok(());
            }

            // New dispatcher required: calculate the new dispatcher revision
            if let Dispatcher::Tc(old_dispatcher) = self
                .dispatchers
                .get(&DispatcherId::Tc(DispatcherInfo(if_index, Some(direction))))
                .unwrap()
            {
                let old_revision = old_dispatcher.revision;
                let revision = old_revision.wrapping_add(1);

                // Cache program length so programs goes out of scope and
                // sort_extensions() can generate its own list.
                self.sort_extensions_tc(direction, &if_index);

                let mut dispatcher_loader = self.new_tc_dispatcher(
                    direction,
                    &if_index,
                    attached_programs as u8,
                    self.dispatcher_bytes_tc,
                    revision,
                )?;

                self.attach_extensions_tc(
                    direction,
                    &if_index,
                    &mut dispatcher_loader,
                    None,
                    revision,
                )?;
                self.attach_or_replace_dispatcher_tc(
                    direction,
                    iface,
                    if_index,
                    dispatcher_loader,
                    revision,
                )?;
                self.cleanup_extensions_tc(direction, if_index, old_revision)?;
            } else {
                unreachable!()
            }
            Ok(())
        } else {
            unreachable!()
        }
    }

    pub(crate) fn list_programs(&mut self, _iface: String) -> Result<InterfaceInfo, BpfdError> {
        todo!("will replace in a later patchset");
    }

    fn list_programs_xdp(&mut self, _if_index: u32) -> Result<InterfaceInfo, BpfdError> {
        todo!()
    }

    fn list_programs_tc(
        &mut self,
        prog_type: ProgramType,
        if_index: u32,
    ) -> Result<InterfaceInfo, BpfdError> {
        todo!()
    }

    fn attach_extensions_xdp(
        &mut self,
        if_index: &u32,
        dispatcher_loader: &mut Bpf,
        mut ext_loader: Option<Bpf>,
        revision: usize,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        let mut extensions = self
            .programs
            .iter_mut()
            .filter_map(|(k, v)| {
                if let Program::Xdp(data, info) = v {
                    if info.if_index == *if_index {
                        Some((k, (data, info)))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&Uuid, (&mut ProgramData, &mut NetworkMultiAttachInfo))>>();
        extensions.sort_by(|(_, (_, a)), (_, (_, b))| a.current_position.cmp(&b.current_position));
        for (i, (k, (_, v))) in extensions.iter_mut().enumerate() {
            if v.metadata.attached {
                let mut prog = PinnedProgram::from_pin(format!("{RTDIR_FS}/prog_{k}"))?;
                let ext: &mut Extension = prog.as_mut().try_into()?;
                let target_fn = format!("prog{i}");
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let path = format!("{RTDIR_FS}/dispatcher_{if_index}_{revision}/link_{k}");
                new_link.pin(path).map_err(|_| BpfdError::UnableToPin)?;
            } else {
                let ext: &mut Extension = ext_loader
                    .as_mut()
                    .unwrap()
                    .program_mut(&v.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                ext.pin(format!("{RTDIR_FS}/prog_{k}"))
                    .map_err(|_| BpfdError::UnableToPin)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                fd_link
                    .pin(format!(
                        "{RTDIR_FS}/dispatcher_{if_index}_{revision}/link_{k}"
                    ))
                    .map_err(|_| BpfdError::UnableToPin)?;
                v.metadata.attached = true;
            }
        }
        Ok(())
    }

    fn attach_extensions_tc(
        &mut self,
        direction: Direction,
        if_index: &u32,
        dispatcher_loader: &mut Bpf,
        mut ext_loader: Option<Bpf>,
        revision: usize,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut SchedClassifier = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        let mut extensions = self
            .programs
            .iter_mut()
            .filter_map(|(k, v)| {
                if let Program::Tc(data, info, dir) = v {
                    if info.if_index == *if_index && *dir == direction {
                        Some((k, (data, info)))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&Uuid, (&mut ProgramData, &mut NetworkMultiAttachInfo))>>();
        extensions.sort_by(|(_, (_, a)), (_, (_, b))| a.current_position.cmp(&b.current_position));

        for (i, (k, (_, v))) in extensions.iter_mut().enumerate() {
            if v.metadata.attached {
                let mut prog = PinnedProgram::from_pin(format!("/var/run/bpfd/fs/prog_{k}"))?;
                let ext: &mut Extension = prog.as_mut().try_into()?;
                let target_fn = format!("prog{i}");
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let path = format!(
                    "/var/run/bpfd/fs/dispatcher_tc_{direction}_{if_index}_{revision}/link_{k}"
                );
                new_link.pin(path).map_err(|_| BpfdError::UnableToPin)?;
            } else {
                let ext: &mut Extension = ext_loader
                    .as_mut()
                    .unwrap()
                    .program_mut(&v.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                ext.pin(format!("/var/run/bpfd/fs/prog_{k}"))
                    .map_err(|_| BpfdError::UnableToPin)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                fd_link
                    .pin(format!(
                        "/var/run/bpfd/fs/dispatcher_tc_{direction}_{if_index}_{revision}/link_{k}"
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
        revision: usize,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        if let Some(Dispatcher::Xdp(d)) = self
            .dispatchers
            .remove(&DispatcherId::Xdp(DispatcherInfo(if_index, None)))
        {
            let path = format!("{RTDIR_FS}/dispatcher_{if_index}_link");
            let pinned_link: FdLink = PinnedLink::from_pin(path).unwrap().into();
            dispatcher
                .attach_to_link(pinned_link.try_into().unwrap())
                .unwrap();
            self.dispatchers.insert(
                DispatcherId::Xdp(DispatcherInfo(if_index, None)),
                Dispatcher::Xdp(XdpDispatcher {
                    revision,
                    mode: d.mode,
                }),
            );
        } else {
            let mode = if let Some(i) = &self.config.interfaces {
                i.get(&iface).map_or(XdpMode::Skb, |i| i.xdp_mode)
            } else {
                XdpMode::Skb
            };
            let flags = mode.as_flags();
            let link = dispatcher.attach(&iface, flags).unwrap();
            let owned_link = dispatcher.take_link(link)?;
            let path = format!("{RTDIR_FS}/dispatcher_{if_index}_link");
            let _ = TryInto::<FdLink>::try_into(owned_link)
                .unwrap() // TODO: Don't unwrap, although due to minimum kernel version this shouldn't ever panic
                .pin(path)
                .map_err(|_| BpfdError::UnableToPin)?;
            let p = XdpDispatcher { revision, mode };
            p.save(if_index)
                .map_err(|_| BpfdError::Error("unable to persist dispatcher data".to_string()))?;

            self.dispatchers.insert(
                DispatcherId::Xdp(DispatcherInfo(if_index, None)),
                Dispatcher::Xdp(p),
            );
        }
        Ok(())
    }

    fn attach_or_replace_dispatcher_tc(
        &mut self,
        direction: Direction,
        iface: String,
        if_index: u32,
        mut new_dispatcher_loader: Bpf,
        revision: usize,
    ) -> Result<(), BpfdError> {
        let new_dispatcher: &mut SchedClassifier = new_dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        let attach_type = match direction {
            Direction::Ingress => TcAttachType::Ingress,
            Direction::Egress => TcAttachType::Egress,
        };

        if let Some(Dispatcher::Tc(mut d)) = self
            .dispatchers
            .remove(&DispatcherId::Tc(DispatcherInfo(if_index, Some(direction))))
        {
            let new_priority = d.current_pri - 1;
            let new_link_id = new_dispatcher.attach(&iface, attach_type, new_priority)?;
            if new_priority > MAX_TC_DISPATCHER_PRIORITY {
                self.dispatchers.insert(
                    DispatcherId::Tc(DispatcherInfo(if_index, Some(direction))),
                    Dispatcher::Tc(TcDispatcher {
                        revision,
                        current_pri: new_priority,
                        loader: new_dispatcher_loader,
                        link_id: new_link_id,
                    }),
                );
                return Ok(());
            }

            let old_dispatcher: &mut SchedClassifier = d
                .loader
                .program_mut(DISPATCHER_PROGRAM_NAME)
                .unwrap()
                .try_into()?;

            // Manually detach the old dispatcher
            old_dispatcher.detach(d.link_id)?;

            let new_priority = MIN_TC_DISPATCHER_PRIORITY;

            // Attach the new scheduler at the lowest priority
            let new_link_id_2 = new_dispatcher.attach(&iface, attach_type, new_priority)?;

            // Manually detach the new dispatcher that's at the highest priority
            new_dispatcher.detach(new_link_id)?;

            self.dispatchers.insert(
                DispatcherId::Tc(DispatcherInfo(if_index, Some(direction))),
                Dispatcher::Tc(TcDispatcher {
                    revision,
                    current_pri: new_priority,
                    loader: new_dispatcher_loader,
                    link_id: new_link_id_2,
                }),
            );
        } else {
            // This is the first tc dispatcher on this interface
            let new_link_id =
                new_dispatcher.attach(&iface, attach_type, MIN_TC_DISPATCHER_PRIORITY)?;
            self.dispatchers.insert(
                DispatcherId::Tc(DispatcherInfo(if_index, Some(direction))),
                Dispatcher::Tc(TcDispatcher {
                    revision,
                    current_pri: MIN_TC_DISPATCHER_PRIORITY,
                    loader: new_dispatcher_loader,
                    link_id: new_link_id,
                }),
            );
        }
        Ok(())
    }

    fn cleanup_extensions_xdp(&self, if_index: u32, revision: usize) -> Result<(), BpfdError> {
        let path = format!("{RTDIR_FS}/dispatcher_{if_index}_{revision}");
        fs::remove_dir_all(path).map_err(|io_error| BpfdError::UnableToCleanup { io_error })
    }

    fn delete_link_xdp(&self, if_index: u32, revision: usize) -> Result<(), BpfdError> {
        let path_link = format!("{RTDIR_FS}/dispatcher_{if_index}_link");
        fs::remove_file(path_link)?;
        let path_link_rev = format!("{RTDIR_FS}/dispatcher_{if_index}_{revision}/");
        fs::remove_dir_all(path_link_rev)
            .map_err(|io_error| BpfdError::UnableToCleanup { io_error })
    }

    fn cleanup_extensions_tc(
        &self,
        direction: Direction,
        if_index: u32,
        revision: usize,
    ) -> Result<(), BpfdError> {
        let path = format!("{RTDIR_FS}/dispatcher_tc_{direction}_{if_index}_{revision}");
        fs::remove_dir_all(path).map_err(|io_error| BpfdError::UnableToCleanup { io_error })
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
            .programs
            .values_mut()
            .filter_map(|v| {
                if let Program::Xdp(data, info) = v {
                    if info.if_index == *if_index {
                        Some((data, info))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&mut ProgramData, &mut NetworkMultiAttachInfo)>>();
        extensions.sort_by(|(_, a), (_, b)| a.metadata.cmp(&b.metadata));
        for (i, (_, v)) in extensions.iter_mut().enumerate() {
            v.current_position = Some(i);
        }
    }

    fn sort_extensions_tc(&mut self, direction: Direction, if_index: &u32) {
        let mut extensions = self
            .programs
            .values_mut()
            .filter_map(|v| {
                if let Program::Tc(data, info, dir) = v {
                    if info.if_index == *if_index && *dir == direction {
                        Some((data, info))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&mut ProgramData, &mut NetworkMultiAttachInfo)>>();
        extensions.sort_by(|(_, a), (_, b)| a.metadata.cmp(&b.metadata));
        for (i, (_, v)) in extensions.iter_mut().enumerate() {
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
        let mut chain_call_actions = [DEFAULT_XDP_ACTIONS_MAP; 10];

        let mut extensions = self
            .programs
            .iter()
            .filter_map(|(k, v)| {
                if let Program::Xdp(data, info) = v {
                    if info.if_index == *if_index {
                        Some((k, (data, info)))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&Uuid, (&ProgramData, &NetworkMultiAttachInfo))>>();
        extensions.sort_by(|(_, (_, a)), (_, (_, b))| a.current_position.cmp(&b.current_position));
        for (_, (_, v)) in extensions.iter() {
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

        let path = format!("{RTDIR_FS}/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        Ok(dispatcher_loader)
    }

    fn new_tc_dispatcher(
        &mut self,
        direction: Direction,
        if_index: &u32,
        num_progs_enabled: u8,
        bytes: &[u8],
        revision: usize,
    ) -> Result<Bpf, BpfdError> {
        let mut chain_call_actions = [DEFAULT_ACTIONS_MAP_TC; 10];

        let mut extensions = self
            .programs
            .iter()
            .filter_map(|(k, v)| {
                if let Program::Tc(data, info, dir) = v {
                    if info.if_index == *if_index && direction == *dir {
                        Some((k, (data, info)))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&Uuid, (&ProgramData, &NetworkMultiAttachInfo))>>();
        extensions.sort_by(|(_, (_, a)), (_, (_, b))| a.current_position.cmp(&b.current_position));

        for (_, (_, v)) in extensions.iter() {
            chain_call_actions[v.current_position.unwrap()] = DEFAULT_ACTIONS_MAP_TC
            // TODO: update v.proceed_on_mask();
        }

        let config = TcDispatcherConfig {
            num_progs_enabled,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        let mut dispatcher_loader = BpfLoader::new().set_global("CONFIG", &config).load(bytes)?;

        let dispatcher: &mut SchedClassifier = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        dispatcher.load()?;

        let path = format!("/var/run/bpfd/fs/dispatcher_tc_{direction}_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        Ok(dispatcher_loader)
    }
}
