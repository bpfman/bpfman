// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, convert::TryInto, fs};

use anyhow::anyhow;
use aya::{programs::TracePoint, BpfLoader};
use bpfd_api::{config::Config, util::directories::*};
use log::{debug, info};
use uuid::Uuid;

use crate::{
    command::{
        Direction,
        Direction::{Egress, Ingress},
        ProceedOn, Program, ProgramInfo, ProgramType,
    },
    errors::BpfdError,
    multiprog::{Dispatcher, DispatcherId, DispatcherInfo, TcDispatcher, XdpDispatcher},
    ProgramType::{Tc, Xdp},
};

const SUPERUSER: &str = "bpfctl";

pub(crate) struct BpfManager<'a> {
    config: &'a Config,
    dispatchers: HashMap<DispatcherId, Dispatcher>,
    programs: HashMap<Uuid, Program>,
}

impl<'a> BpfManager<'a> {
    pub(crate) fn new(config: &'a Config) -> Self {
        Self {
            config,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
        }
    }

    pub(crate) fn rebuild_state(&mut self) -> Result<(), anyhow::Error> {
        if let Ok(programs_dir) = fs::read_dir(RTDIR_PROGRAMS) {
            for entry in programs_dir {
                let entry = entry?;
                let uuid = entry.file_name().to_string_lossy().parse().unwrap();
                let mut program = Program::load(uuid)
                    .map_err(|e| BpfdError::Error(format!("cant read program state {e}")))?;
                // TODO: Should probably check for pinned prog on bpffs rather than assuming they are attached
                program.set_attached();
                debug!("rebuilding state for program {}", uuid);
                self.programs.insert(uuid, program);
            }
        }
        self.rebuild_dispatcher_state(Xdp, None, RTDIR_XDP_DISPATCHER)?;
        self.rebuild_dispatcher_state(Tc, Some(Ingress), RTDIR_TC_INGRESS_DISPATCHER)?;
        self.rebuild_dispatcher_state(Tc, Some(Egress), RTDIR_TC_EGRESS_DISPATCHER)?;

        Ok(())
    }

    pub(crate) fn rebuild_dispatcher_state(
        &mut self,
        program_type: ProgramType,
        direction: Option<Direction>,
        path: &str,
    ) -> Result<(), anyhow::Error> {
        if let Ok(dispatcher_dir) = fs::read_dir(path) {
            for entry in dispatcher_dir {
                let entry = entry?;
                let name = entry.file_name();
                let parts: Vec<&str> = name.to_str().unwrap().split('_').collect();
                if parts.len() != 2 {
                    continue;
                }
                let if_index: u32 = parts[0].parse().unwrap();
                let revision: u32 = parts[1].parse().unwrap();
                info!(
                    "rebuilding state for {program_type} (direction: {direction:?}) dispatcher on if_index {if_index}"
                );
                match program_type {
                    Xdp => {
                        let dispatcher = XdpDispatcher::load(if_index, revision).unwrap();
                        self.dispatchers.insert(
                            DispatcherId::Xdp(DispatcherInfo(if_index, None)),
                            Dispatcher::Xdp(dispatcher),
                        );
                    }
                    Tc => {
                        if let Some(dir) = direction {
                            let dispatcher = TcDispatcher::load(if_index, dir, revision).unwrap();
                            self.dispatchers.insert(
                                DispatcherId::Tc(DispatcherInfo(if_index, direction)),
                                Dispatcher::Tc(dispatcher),
                            );
                        } else {
                            return Err(anyhow!("direction required for tc programs"));
                        }
                        // Rebuild the dispatcher 3 times to clear out the old dispatcher.
                        //TODO: Change this when https://github.com/aya-rs/aya/pull/445 is available.
                        for _ in 0..3 {
                            self.rebuild_multiattach_dispatcher(
                                Tc,
                                if_index,
                                direction,
                                DispatcherId::Tc(DispatcherInfo(if_index, direction)),
                            )?;
                        }
                    }
                    _ => return Err(anyhow!("invalid program type: {}", program_type)),
                }
            }
        }
        Ok(())
    }

    pub(crate) fn add_program(&mut self, program: Program) -> Result<Uuid, BpfdError> {
        match program {
            Program::Xdp(_) | Program::Tc(_) => self.add_multi_attach_program(program),
            Program::Tracepoint(_) => self.add_single_attach_program(program),
        }
    }

    pub(crate) fn add_multi_attach_program(&mut self, program: Program) -> Result<Uuid, BpfdError> {
        let id = Uuid::new_v4();
        let map_pin_path = format!("{RTDIR_FS_MAPS}/{id}");
        fs::create_dir_all(map_pin_path.clone())
            .map_err(|e| BpfdError::Error(format!("can't create map dir: {e}")))?;

        let mut ext_loader = BpfLoader::new()
            .extension(&program.data().section_name)
            .map_pin_path(map_pin_path.clone())
            .load_file(program.data().path.clone())?;

        ext_loader
            .program_mut(&program.data().section_name)
            .ok_or_else(|| {
                let _ = fs::remove_dir_all(map_pin_path);
                BpfdError::SectionNameNotValid(program.data().section_name.clone())
            })?;

        // Calculate the next_available_id
        let next_available_id = self
            .programs
            .iter()
            .filter(|(_, p)| {
                if p.kind() == program.kind() {
                    p.if_index() == program.if_index() && p.direction() == program.direction()
                } else {
                    false
                }
            })
            .collect::<HashMap<_, _>>()
            .len();
        if next_available_id >= 10 {
            return Err(BpfdError::TooManyPrograms);
        }

        let program_type = program.kind();
        let if_index = program.if_index();
        let if_name = program.if_name().unwrap();
        let direction = program.direction();

        let did = program
            .dispatcher_id()
            .ok_or(BpfdError::DispatcherNotRequired)?;
        program
            .save(id)
            .map_err(|e| BpfdError::Error(format!("unable to save program state: {e}")))?;
        self.programs.insert(id, program);
        self.sort_programs(program_type, if_index, direction);
        let programs = self.collect_programs(program_type, if_index, direction);
        let old_dispatcher = self.dispatchers.remove(&did);
        let if_config = if let Some(ref i) = self.config.interfaces {
            i.get(&if_name)
        } else {
            None
        };
        let next_revision = if let Some(ref old) = old_dispatcher {
            old.next_revision()
        } else {
            1
        };
        let dispatcher = Dispatcher::new(if_config, &programs, next_revision, old_dispatcher)?;
        self.dispatchers.insert(did, dispatcher);
        if let Some(p) = self.programs.get_mut(&id) {
            p.set_attached()
        };
        Ok(id)
    }

    pub(crate) fn add_single_attach_program(&mut self, p: Program) -> Result<Uuid, BpfdError> {
        if let Program::Tracepoint(ref program) = p {
            let id = Uuid::new_v4();
            let parts: Vec<&str> = program.info.split('/').collect();
            if parts.len() != 2 {
                return Err(BpfdError::InvalidAttach(program.info.to_string()));
            }
            let category = parts[0].to_owned();
            let name = parts[1].to_owned();

            let map_pin_path = format!("{RTDIR_FS_MAPS}/{id}");
            fs::create_dir_all(map_pin_path.clone())
                .map_err(|e| BpfdError::Error(format!("can't create map dir: {e}")))?;

            let mut loader = BpfLoader::new()
                .map_pin_path(map_pin_path.clone())
                .load_file(&program.data.path)?;

            let tracepoint: &mut TracePoint = loader
                .program_mut(&program.data.section_name)
                .ok_or_else(|| {
                    let _ = fs::remove_dir_all(map_pin_path);
                    BpfdError::SectionNameNotValid(program.data.section_name.clone())
                })?
                .try_into()?;

            tracepoint.load()?;
            p.save(id)
                .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;
            self.programs.insert(id, p);

            tracepoint.attach(&category, &name)?;
            tracepoint
                .pin(format!("{RTDIR_FS}/prog_{id}_link"))
                .map_err(|_| BpfdError::UnableToPin)?;
            Ok(id)
        } else {
            panic!("not a tracepoint program")
        }
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
            Program::Xdp(_) | Program::Tc(_) => self.remove_multi_attach_program(prog),
            Program::Tracepoint(_) => Ok(()),
        }
    }

    pub(crate) fn remove_multi_attach_program(
        &mut self,
        program: Program,
    ) -> Result<(), BpfdError> {
        // Calculate the next_available_id
        let next_available_id = self
            .programs
            .iter()
            .filter(|(_, p)| {
                if p.kind() == program.kind() {
                    p.if_index() == program.if_index() && p.direction() == program.direction()
                } else {
                    false
                }
            })
            .collect::<HashMap<_, _>>()
            .len();

        let did = program
            .dispatcher_id()
            .ok_or(BpfdError::DispatcherNotRequired)?;

        let mut old_dispatcher = self.dispatchers.remove(&did);

        if let Some(ref mut old) = old_dispatcher {
            if next_available_id == 0 {
                // Delete the dispatcher
                return old.delete(true);
            }
        }

        let program_type = program.kind();
        let if_index = program.if_index();
        let if_name = program.if_name().unwrap();
        let direction = program.direction();

        self.sort_programs(program_type, if_index, direction);

        let programs = self.collect_programs(program_type, if_index, direction);

        let if_config = if let Some(ref i) = self.config.interfaces {
            i.get(&if_name)
        } else {
            None
        };
        let next_revision = if let Some(ref old) = old_dispatcher {
            old.next_revision()
        } else {
            1
        };
        let dispatcher = Dispatcher::new(if_config, &programs, next_revision, old_dispatcher)?;
        self.dispatchers.insert(did, dispatcher);
        Ok(())
    }

    pub(crate) fn rebuild_multiattach_dispatcher(
        &mut self,
        program_type: ProgramType,
        if_index: u32,
        direction: Option<Direction>,
        did: DispatcherId,
    ) -> Result<(), BpfdError> {
        let mut old_dispatcher = self.dispatchers.remove(&did);

        if let Some(ref mut old) = old_dispatcher {
            debug!("Rebuild Multiattach Dispatcher for {did:?}");
            let if_index = Some(if_index);

            self.sort_programs(program_type, if_index, direction);
            let programs = self.collect_programs(program_type, if_index, direction);

            // The following checks should have been done when the dispatcher was built, but check again to confirm
            if programs.is_empty() {
                return old.delete(true);
            } else if programs.len() > 10 {
                return Err(BpfdError::TooManyPrograms);
            }

            let if_name = old.if_name();
            let if_config = if let Some(ref i) = self.config.interfaces {
                i.get(&if_name)
            } else {
                None
            };

            let next_revision = if let Some(ref old) = old_dispatcher {
                old.next_revision()
            } else {
                1
            };

            let dispatcher = Dispatcher::new(if_config, &programs, next_revision, old_dispatcher)?;
            self.dispatchers.insert(did, dispatcher);
        } else {
            debug!("No dispatcher found in rebuild_multiattach_dispatcher() for {did:?}");
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self) -> Result<Vec<ProgramInfo>, BpfdError> {
        let programs = self
            .programs
            .iter()
            .map(|(id, p)| match p {
                Program::Xdp(p) => ProgramInfo {
                    id: id.to_string(),
                    name: p.data.section_name.to_string(),
                    path: p.data.path.to_string(),
                    program_type: crate::command::ProgramType::Xdp,
                    direction: None,
                    attach_type: crate::command::AttachType::NetworkMultiAttach(
                        crate::command::NetworkMultiAttach {
                            iface: p.info.if_name.to_string(),
                            priority: p.info.metadata.priority,
                            proceed_on: p.info.proceed_on.clone(),
                            position: p.info.current_position.unwrap_or_default() as i32,
                        },
                    ),
                },
                Program::Tracepoint(p) => ProgramInfo {
                    id: id.to_string(),
                    name: p.data.section_name.to_string(),
                    path: p.data.path.to_string(),
                    program_type: crate::command::ProgramType::Tracepoint,
                    direction: None,
                    attach_type: crate::command::AttachType::SingleAttach(p.info.to_string()),
                },
                Program::Tc(p) => ProgramInfo {
                    id: id.to_string(),
                    name: p.data.section_name.to_string(),
                    path: p.data.path.to_string(),
                    program_type: crate::command::ProgramType::Tc,
                    direction: Some(p.direction),
                    attach_type: crate::command::AttachType::NetworkMultiAttach(
                        crate::command::NetworkMultiAttach {
                            iface: p.info.if_name.to_string(),
                            priority: p.info.metadata.priority,
                            proceed_on: ProceedOn::default_tc(),
                            position: p.info.current_position.unwrap() as i32,
                        },
                    ),
                },
            })
            .collect();
        Ok(programs)
    }

    fn sort_programs(
        &mut self,
        program_type: ProgramType,
        if_index: Option<u32>,
        direction: Option<Direction>,
    ) {
        let mut extensions = self
            .programs
            .iter_mut()
            .filter_map(|(k, v)| {
                if v.kind() == program_type {
                    if v.if_index() == if_index && v.direction() == direction {
                        Some((k, v))
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect::<Vec<(&Uuid, &mut Program)>>();
        extensions.sort_by(|(_, a), (_, b)| a.metadata().cmp(&b.metadata()));
        for (i, (_, v)) in extensions.iter_mut().enumerate() {
            v.set_position(Some(i));
        }
    }

    fn collect_programs(
        &self,
        program_type: ProgramType,
        if_index: Option<u32>,
        direction: Option<Direction>,
    ) -> Vec<(Uuid, Program)> {
        let mut results = vec![];
        for (k, v) in self.programs.iter() {
            if v.kind() == program_type && v.if_index() == if_index && v.direction() == direction {
                results.push((k.to_owned(), v.clone()))
            }
        }
        results
    }
}
