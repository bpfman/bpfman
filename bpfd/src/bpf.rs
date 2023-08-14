// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, convert::TryInto, path::PathBuf};

use anyhow::anyhow;
use aya::{
    programs::{
        kprobe::KProbeLink, links::FdLink, loaded_programs, trace_point::TracePointLink,
        uprobe::UProbeLink, KProbe, TracePoint, UProbe,
    },
    BpfLoader,
};
use bpfd_api::{
    config::Config,
    util::directories::*,
    ProbeType::{self, *},
    ProgramType,
};
use log::{debug, info};
use tokio::{fs, select, sync::mpsc};

use crate::{
    command::{
        BpfMap, Command, Direction,
        Direction::{Egress, Ingress},
        Program, PullBytecodeArgs, UnloadArgs,
    },
    errors::BpfdError,
    multiprog::{Dispatcher, DispatcherId, DispatcherInfo, TcDispatcher, XdpDispatcher},
    serve::shutdown_handler,
    utils::{get_ifindex, set_dir_permissions},
};

const MAPS_MODE: u32 = 0o0660;

pub(crate) struct BpfManager {
    config: Config,
    dispatchers: HashMap<DispatcherId, Dispatcher>,
    programs: HashMap<u32, Program>,
    maps: HashMap<u32, BpfMap>,
    commands: mpsc::Receiver<Command>,
}

impl BpfManager {
    pub(crate) fn new(config: Config, commands: mpsc::Receiver<Command>) -> Self {
        Self {
            config,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
            maps: HashMap::new(),
            commands,
        }
    }

    pub(crate) async fn rebuild_state(&mut self) -> Result<(), anyhow::Error> {
        debug!("BpfManager::rebuild_state()");
        let mut programs_dir = fs::read_dir(RTDIR_PROGRAMS).await?;
        while let Some(entry) = programs_dir.next_entry().await? {
            let id = entry.file_name().to_string_lossy().parse().unwrap();
            let mut program = Program::load(id)
                .map_err(|e| BpfdError::Error(format!("cant read program state {e}")))?;
            // TODO: Should probably check for pinned prog on bpffs rather than assuming they are attached
            program.set_attached();
            debug!("rebuilding state for program {}", id);
            self.rebuild_map_entry(id, program.data().map_owner_id);
            self.programs.insert(id, program);
        }
        self.rebuild_dispatcher_state(ProgramType::Xdp, None, RTDIR_XDP_DISPATCHER)
            .await?;
        self.rebuild_dispatcher_state(ProgramType::Tc, Some(Ingress), RTDIR_TC_INGRESS_DISPATCHER)
            .await?;
        self.rebuild_dispatcher_state(ProgramType::Tc, Some(Egress), RTDIR_TC_EGRESS_DISPATCHER)
            .await?;

        Ok(())
    }

    pub(crate) async fn rebuild_dispatcher_state(
        &mut self,
        program_type: ProgramType,
        direction: Option<Direction>,
        path: &str,
    ) -> Result<(), anyhow::Error> {
        let mut dispatcher_dir = fs::read_dir(path).await?;
        while let Some(entry) = dispatcher_dir.next_entry().await? {
            let name = entry.file_name();
            let parts: Vec<&str> = name.to_str().unwrap().split('_').collect();
            if parts.len() != 2 {
                continue;
            }
            let if_index: u32 = parts[0].parse().unwrap();
            let revision: u32 = parts[1].parse().unwrap();
            match program_type {
                ProgramType::Xdp => {
                    let dispatcher = XdpDispatcher::load(if_index, revision).unwrap();
                    self.dispatchers.insert(
                        DispatcherId::Xdp(DispatcherInfo(if_index, None)),
                        Dispatcher::Xdp(dispatcher),
                    );
                }
                ProgramType::Tc => {
                    if let Some(dir) = direction {
                        let dispatcher = TcDispatcher::load(if_index, dir, revision).unwrap();
                        self.dispatchers.insert(
                            DispatcherId::Tc(DispatcherInfo(if_index, direction)),
                            Dispatcher::Tc(dispatcher),
                        );
                    } else {
                        return Err(anyhow!("direction required for tc programs"));
                    }

                    self.rebuild_multiattach_dispatcher(
                        program_type,
                        if_index,
                        direction,
                        DispatcherId::Tc(DispatcherInfo(if_index, direction)),
                    )
                    .await?;
                }
                _ => return Err(anyhow!("invalid program type {:?}", program_type)),
            }
        }

        Ok(())
    }

    pub(crate) async fn add_program(&mut self, mut program: Program) -> Result<u32, BpfdError> {
        debug!("BpfManager::add_program()");

        let map_owner_id = program.data().map_owner_id;

        program.data_mut().map_pin_path = self.parse_map_owner_id(map_owner_id).await?;

        let result = match program {
            Program::Xdp(_) | Program::Tc(_) => {
                program.set_if_index(get_ifindex(&program.if_name().unwrap())?);

                self.add_multi_attach_program(program.clone()).await
            }
            Program::Tracepoint(_) | Program::Kprobe(_) | Program::Uprobe(_) => {
                self.add_single_attach_program(program.clone()).await
            }
            Program::Unsupported(_) => panic!("Cannot add unsupported program"),
        };

        if let Ok(id) = result {
            let loaded_program = self
                .programs
                .get(&id)
                .expect("program was not loaded even though id was returned");
            // Now that program is successfully loaded, update the maps hash table
            // and allow access to all maps by bpfd group members.
            self.save_map(
                id,
                map_owner_id,
                loaded_program
                    .data()
                    .clone()
                    .map_pin_path
                    .expect("map_pin_path should be set after load"),
            )
            .await?;
        };

        //     Err(_) => {
        //         // TODO(astoycos) push this cleanup deeper
        //         // On failure if we aren't using another program's map cleanup the map dir that was created in the
        //         // failed load attempt.
        //         if let Some(p) = program.data().clone().map_pin_path {
        //             if p.exists() && program.data().map_owner_id.is_none() {
        //                 let _ = fs::remove_dir_all(program.data().clone().map_pin_path.unwrap())
        //                     .await
        //                     .map_err(|e| BpfdError::Error(format!("can't delete map dir: {e}")));
        //             }
        //         }

        //         if program.data().map_owner_id.is_none()
        //             && program
        //                 .data()
        //                 .clone()
        //                 .map_pin_path
        //                 .is_some_and(|p| p.exists())
        //         {
        //             let _ = fs::remove_dir_all(program.data().map_pin_path.clone().unwrap())
        //                 .await
        //                 .map_err(|e| BpfdError::Error(format!("can't delete map dir: {e}")));
        //         };
        //     }
        // }

        result
    }

    pub(crate) async fn add_multi_attach_program(
        &mut self,
        mut program: Program,
    ) -> Result<u32, BpfdError> {
        debug!("BpfManager::add_multi_attach_program()");

        let program_bytes = program.data_mut().program_bytes().await?;

        // This load is just to verify the Section Name is valid.
        // The actual load is performed in the XDP or TC logic.
        let mut ext_loader = BpfLoader::new()
            .allow_unsupported_maps()
            .extension(&program.data().name)
            .load(&program_bytes)?;

        match ext_loader.program_mut(&program.data().name) {
            Some(_) => Ok(()),
            None => Err(BpfdError::SectionNameNotValid(program.data().name.clone())),
        }?;

        // Calculate the next_available_id
        let next_available_id = self
            .programs
            .iter()
            .filter(|(_, p)| {
                if p.kind() == program.kind() {
                    p.if_index() == program.if_index() && {
                        if let (Program::Tc(a), Program::Tc(b)) = (p, program.clone()) {
                            a.direction == b.direction
                        } else {
                            true
                        }
                    }
                } else {
                    false
                }
            })
            .collect::<HashMap<_, _>>()
            .len();
        if next_available_id >= 10 {
            return Err(BpfdError::TooManyPrograms);
        }

        debug!("next_available_id={next_available_id}");

        let did = program
            .dispatcher_id()
            .ok_or(BpfdError::DispatcherNotRequired)?;

        let old_dispatcher = self.dispatchers.remove(&did);

        let program_type = program.kind();
        let if_index = program.if_index();
        let if_name = program.if_name().unwrap();
        let direction = if let Program::Tc(a) = program.clone() {
            Some(a.direction)
        } else {
            None
        };

        let mut programs = self.get_programs(program_type, if_index, direction);

        // add new program
        programs.push(program.clone());

        let mut sorted_programs = sort_dispatcher_programs(programs);

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

        // pass in a new program which will be in programs AND populated with kernel info after load, programs here is
        // already sorted and should only need to be transformed into a list of concrete programs.
        let dispatcher = Dispatcher::new(
            if_config,
            &mut sorted_programs,
            next_revision,
            old_dispatcher,
        )
        .await?;
        //.or_else(|e| {
        //warn!("{}", e);
        //TODO(astoycos) make sure we clean up in the event of an error
        // this will probably need to be moved internally
        // let id = program
        //     .data()
        //     .clone()
        //     .kernel_info
        //     .expect("kernel info should be set after load")
        //     .id;
        // let prog = self.programs.remove(&id).unwrap();
        // prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
        //Err(e)
        //})?;

        self.dispatchers.insert(did, dispatcher);

        // make sure we populate kernel_info since it contains the program's kernel ID
        sorted_programs
            .iter()
            .find(|p| **p == program)
            .map(|p| {
                match p.data().kernel_info.clone() {
                    Some(i) => program.set_kernel_info(i),
                    None => {
                        return Err(BpfdError::Error(format!(
                            "Program {}'s kernel info is unset",
                            p.data().name
                        )))
                    }
                };

                match p.data().map_pin_path.clone() {
                    Some(i) => program.data_mut().map_pin_path = Some(i),
                    None => {
                        return Err(BpfdError::Error(format!(
                            "Program {}'s map_pin_path is unset",
                            p.data().name
                        )))
                    }
                };

                Ok(())
            })
            .transpose()?
            .ok_or(BpfdError::Error(format!(
                "Program {} not found in manager after load",
                program.data().name
            )))?;

        let id = &program
            .data()
            .kernel_info
            .clone()
            .expect("kernel info should be set after load")
            .id;

        program.set_attached();
        program
            .save(*id)
            .map_err(|e| BpfdError::Error(format!("unable to save program state: {e}")))?;

        self.programs.insert(id.to_owned(), program.to_owned());

        Ok(*id)
    }

    pub(crate) async fn add_single_attach_program(
        &mut self,
        mut p: Program,
    ) -> Result<u32, BpfdError> {
        debug!("BpfManager::add_single_attach_program()");
        let program_bytes = p.data_mut().program_bytes().await?;

        let mut loader = BpfLoader::new();

        match &p.data().global_data {
            Some(d) => {
                for (name, value) in d {
                    loader.set_global(name, value.as_slice(), true);
                }
            }
            None => debug!("no global data to set for Program"),
        }

        if let Some(p) = p.data().map_pin_path.clone() {
            loader.map_pin_path(p);
        };

        let mut loader = loader.allow_unsupported_maps().load(&program_bytes)?;

        let raw_program = loader
            .program_mut(&p.data().name)
            .ok_or(BpfdError::SectionNameNotValid(p.data().name.clone()))?;

        let id = match p.clone() {
            Program::Tracepoint(program) => {
                let parts: Vec<&str> = program.tracepoint.split('/').collect();
                if parts.len() != 2 {
                    return Err(BpfdError::InvalidAttach(program.tracepoint.to_string()));
                }
                let category = parts[0].to_owned();
                let name = parts[1].to_owned();

                let tracepoint: &mut TracePoint = raw_program.try_into()?;

                tracepoint.load()?;

                p.set_kernel_info(tracepoint.program_info()?.try_into()?);

                let id: u32 = p
                    .data()
                    .kernel_info
                    .clone()
                    .expect("kernel info must be set after load")
                    .id;

                p.save(id)
                    .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;

                let link_id = tracepoint.attach(&category, &name).or_else(|e| {
                    let prog = self.programs.remove(&id).unwrap();
                    prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
                    Err(BpfdError::BpfProgramError(e))
                })?;

                let owned_link: TracePointLink = tracepoint.take_link(link_id)?;
                let fd_link: FdLink = owned_link
                    .try_into()
                    .expect("unable to get owned tracepoint attach link");
                fd_link
                    .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                    .map_err(BpfdError::UnableToPinLink)?;

                tracepoint
                    .pin(format!("{RTDIR_FS}/prog_{id}"))
                    .or_else(|e| {
                        let prog = self.programs.remove(&id).unwrap();
                        prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
                        Err(BpfdError::UnableToPinProgram(e))
                    })?;

                id
            }
            Program::Kprobe(program) => {
                let requested_probe_type = match program.retprobe {
                    true => Kretprobe,
                    false => Kprobe,
                };

                if requested_probe_type == Kretprobe && program.offset != 0 {
                    return Err(BpfdError::Error(format!(
                        "offset not allowed for {Kretprobe}"
                    )));
                }

                let kprobe: &mut KProbe = raw_program.try_into()?;
                kprobe.load()?;

                p.set_kernel_info(kprobe.program_info()?.try_into()?);

                let id: u32 = p
                    .data()
                    .kernel_info
                    .clone()
                    .expect("kernel info must be set after load")
                    .id;

                // verify that the program loaded was the same type as the
                // user requested
                let loaded_probe_type = ProbeType::from(kprobe.kind());
                if requested_probe_type != loaded_probe_type {
                    return Err(BpfdError::Error(format!(
                        "expected {requested_probe_type}, loaded program is {loaded_probe_type}"
                    )));
                }

                p.set_kernel_info(kprobe.program_info()?.try_into()?);
                p.save(id)
                    .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;

                let link_id = kprobe
                    .attach(program.fn_name.as_str(), program.offset)
                    .or_else(|e| {
                        p.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
                        Err(BpfdError::BpfProgramError(e))
                    })?;

                let owned_link: KProbeLink = kprobe.take_link(link_id)?;
                let fd_link: FdLink = owned_link
                    .try_into()
                    .expect("unable to get owned kprobe attach link");
                fd_link
                    .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                    .map_err(BpfdError::UnableToPinLink)?;

                kprobe.pin(format!("{RTDIR_FS}/prog_{id}")).or_else(|e| {
                    let prog = self.programs.remove(&id).unwrap();
                    prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
                    Err(BpfdError::UnableToPinProgram(e))
                })?;

                id
            }
            Program::Uprobe(ref program) => {
                let requested_probe_type = match program.retprobe {
                    true => Uretprobe,
                    false => Uprobe,
                };

                let uprobe: &mut UProbe = raw_program.try_into()?;
                uprobe.load()?;

                p.set_kernel_info(uprobe.program_info()?.try_into()?);

                let id: u32 = p
                    .data()
                    .kernel_info
                    .clone()
                    .expect("kernel info must be set after load")
                    .id;

                // verify that the program loaded was the same type as the
                // user requested
                let loaded_probe_type = ProbeType::from(uprobe.kind());
                if requested_probe_type != loaded_probe_type {
                    return Err(BpfdError::Error(format!(
                        "expected {requested_probe_type}, loaded program is {loaded_probe_type}"
                    )));
                }

                p.set_kernel_info(uprobe.program_info()?.try_into()?);
                p.save(id)
                    .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;

                let link_id = uprobe
                    .attach(
                        program.fn_name.as_deref(),
                        program.offset,
                        program.target.clone(),
                        program.pid,
                    )
                    .or_else(|e| {
                        p.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
                        Err(BpfdError::BpfProgramError(e))
                    })?;

                let owned_link: UProbeLink = uprobe.take_link(link_id)?;
                let fd_link: FdLink = owned_link
                    .try_into()
                    .expect("unable to get owned uprobe attach link");
                fd_link
                    .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                    .map_err(BpfdError::UnableToPinLink)?;

                uprobe.pin(format!("{RTDIR_FS}/prog_{id}")).or_else(|e| {
                    let prog = self.programs.remove(&id).unwrap();
                    prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
                    Err(BpfdError::UnableToPinProgram(e))
                })?;

                id
            }
            _ => panic!("not a supported single attach program"),
        };

        // Pin all maps (except for .rodata and .bss) by name and set map pin path
        if p.data().map_pin_path.is_none() {
            let path = manage_map_pin_path(id).await?;
            for (name, map) in loader.maps_mut() {
                if name.contains(".rodata") || name.contains(".bss") {
                    continue;
                }
                map.pin(name, path.join(name))
                    .map_err(BpfdError::UnableToPinMap)?;
            }

            p.data_mut().map_pin_path = Some(path);
        }

        self.programs.insert(id, p.clone());

        Ok(id)
    }

    pub(crate) async fn remove_program(&mut self, id: u32) -> Result<(), BpfdError> {
        debug!("BpfManager::remove_program() id: {id}");
        let prog = self
            .programs
            .remove(&id)
            .ok_or(BpfdError::InvalidID(id.to_string()))?;

        let map_owner_id = prog.data().map_owner_id;

        prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;

        match prog {
            Program::Xdp(_) | Program::Tc(_) => self.remove_multi_attach_program(prog).await?,
            Program::Tracepoint(_)
            | Program::Kprobe(_)
            | Program::Uprobe(_)
            | Program::Unsupported(_) => (),
        }

        self.delete_map(id, map_owner_id).await?;
        Ok(())
    }

    pub(crate) async fn remove_multi_attach_program(
        &mut self,
        program: Program,
    ) -> Result<(), BpfdError> {
        debug!("BpfManager::remove_multi_attach_program()");
        // Calculate the next_available_id
        let next_available_id = self
            .programs
            .iter()
            .filter(|(_, p)| {
                if p.kind() == program.kind() {
                    p.if_index() == program.if_index() && {
                        if let (Program::Tc(a), Program::Tc(b)) = (p, program.clone()) {
                            a.direction == b.direction
                        } else {
                            true
                        }
                    }
                } else {
                    false
                }
            })
            .collect::<HashMap<_, _>>()
            .len();
        debug!("next_available_id = {next_available_id}");

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
        let direction = if let Program::Tc(a) = program.clone() {
            Some(a.direction)
        } else {
            None
        };

        let programs = self.get_programs(program_type, if_index, direction);

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
        debug!("next_revision = {next_revision}");
        let dispatcher = Dispatcher::new(
            if_config,
            &mut sort_dispatcher_programs(programs),
            next_revision,
            old_dispatcher,
        )
        .await?;
        self.dispatchers.insert(did, dispatcher);
        Ok(())
    }

    pub(crate) async fn rebuild_multiattach_dispatcher(
        &mut self,
        program_type: ProgramType,
        if_index: u32,
        direction: Option<Direction>,
        did: DispatcherId,
    ) -> Result<(), BpfdError> {
        debug!("BpfManager::rebuild_multiattach_dispatcher() for program type {program_type} on if_index {if_index}");
        let mut old_dispatcher = self.dispatchers.remove(&did);

        if let Some(ref mut old) = old_dispatcher {
            debug!("Rebuild Multiattach Dispatcher for {did:?}");
            let if_index = Some(if_index);

            let programs = self.get_programs(program_type, if_index, direction);

            debug!("programs loaded: {}", programs.len());

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

            let dispatcher = Dispatcher::new(
                if_config,
                &mut sort_dispatcher_programs(programs),
                next_revision,
                old_dispatcher,
            )
            .await?;
            self.dispatchers.insert(did, dispatcher);
        } else {
            debug!("No dispatcher found in rebuild_multiattach_dispatcher() for {did:?}");
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self) -> Result<Vec<Program>, BpfdError> {
        debug!("BpfManager::list_programs()");

        loaded_programs()
            .map(|p| {
                let prog = p.map_err(BpfdError::BpfProgramError)?;
                let prog_id = prog.id();

                match self.programs.get(&prog_id) {
                    Some(p) => {
                        let map_used_by = self.get_map_used_by(prog_id, p.data().map_owner_id)?;
                        p.clone().data_mut().map_used_by = Some(map_used_by);
                        Ok(p.to_owned())
                    }
                    None => Ok(Program::Unsupported(prog.try_into()?)),
                }
            })
            .collect()
    }

    fn get_programs(
        &self,
        program_type: ProgramType,
        if_index: Option<u32>,
        direction: Option<Direction>,
    ) -> Vec<Program> {
        // add programs with overlapping attachments
        self.programs
            .iter()
            .filter_map(|(_, v)| {
                let d = if let Program::Tc(a) = v.clone() {
                    Some(a.direction)
                } else {
                    None
                };
                if v.kind() == program_type {
                    if v.if_index() == if_index && d == direction {
                        Some(v.to_owned())
                    } else {
                        None
                    }
                } else {
                    None
                }
            })
            .collect()
    }

    async fn pull_bytecode(&self, args: PullBytecodeArgs) -> anyhow::Result<()> {
        let res = match args.image.get_image(None).await {
            Ok(_) => {
                info!("Successfully pulled bytecode");
                Ok(())
            }
            Err(e) => Err(e).map_err(|e| BpfdError::BpfBytecodeError(e.into())),
        };

        let _ = args.responder.send(res);
        Ok(())
    }

    pub(crate) async fn process_commands(&mut self) {
        loop {
            // Start receiving messages
            select! {
                biased;
                _ = shutdown_handler() => {
                    info!("Signal received to stop command processing");
                    break;
                }
                Some(cmd) = self.commands.recv() => {
                    match cmd {
                        Command::Load(args) => {
                            let res = self.add_program(args.program).await;
                            // Ignore errors as they'll be propagated to caller in the RPC status
                            let _ = args.responder.send(res);
                        },
                        Command::Unload(args) => self.unload_command(args).await.unwrap(),
                        Command::List { responder } => {
                            let progs = self.list_programs();
                            // Ignore errors as they'll be propagated to caller in the RPC status
                            let _ = responder.send(progs);
                        }
                        Command::PullBytecode (args) => self.pull_bytecode(args).await.unwrap(),
                    }
                }
            }
        }
        info!("Stopping processing commands");
    }

    async fn unload_command(&mut self, args: UnloadArgs) -> anyhow::Result<()> {
        let res = self.remove_program(args.id).await;
        // Ignore errors as they'll be propagated to caller in the RPC status
        let _ = args.responder.send(res);
        Ok(())
    }

    // // This function reads the map_pin_path from the map hash table. If there
    // // is not an entry for the given input, an error is returned.
    // fn get_map_pin_path(&self, id: u32, map_owner_id: Option<u32>) -> Result<PathBuf, BpfdError> {
    //     let (_, map_index) = get_map_index(id, map_owner_id);

    //     if let Some(map) = self.maps.get(&map_index) {
    //         Ok(map.map_pin_path.clone())
    //     } else {
    //         Err(BpfdError::Error("map does not exists".to_string()))
    //     }
    // }

    // This function reads the map.used_by from the map hash table. If there
    // is not an entry for the given input, an error is returned.
    fn get_map_used_by(
        &self,
        prog_id: u32,
        map_owner_id: Option<u32>,
    ) -> Result<Vec<u32>, BpfdError> {
        let id = map_owner_id.unwrap_or(prog_id);

        if let Some(map) = self.maps.get(&id) {
            Ok(map.used_by.clone())
        } else {
            Err(BpfdError::Error("map does not exists".to_string()))
        }
    }

    // This function returns the map_pin_path, if `map_owner_id` is set with a
    // valid id.
    async fn parse_map_owner_id(
        &self,
        map_owner_id: Option<u32>,
    ) -> Result<Option<PathBuf>, BpfdError> {
        match map_owner_id {
            Some(id) => {
                if let Some(map) = self.maps.get(&id) {
                    let path = map.clone().map_pin_path;
                    if !path.exists() {
                        return Err(BpfdError::Error(
                            "map_owner_id map pin path does not exist".to_string(),
                        ));
                    }
                    Ok(Some(path))
                } else {
                    Err(BpfdError::Error(
                        "map_owner_id map entry not exist".to_string(),
                    ))
                }
            }
            None => Ok(None),
        }
    }

    // // This function is called if manage_map_pin_path() was already called,
    // // but the eBPF program failed to load. save_map() has not been called,
    // // so self.maps has not been updated for this program.
    // // If the user provided a UUID of program to share a map with,
    // // then map the directory is still in use and there is nothing to do.
    // // Otherwise, manage_map_pin_path() created the map directory so it must
    // // deleted.
    // async fn cleanup_map_pin_path(
    //     &mut self,
    //     prog_id: u32,
    //     map_owner_id: Option<u32>,
    // ) -> Result<(), BpfdError> {
    //     let id = map_owner_id.unwrap_or(prog_id);
    //     let path = PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", id));

    //     if map_owner_id.is_none() {
    //         let _ = fs::remove_dir_all(path.clone())
    //             .await
    //             .map_err(|e| BpfdError::Error(format!("can't delete map dir: {e}")));
    //     };

    //     Ok(())
    // }

    // This function writes the map to the map hash table. If this eBPF
    // program is the map owner, then a new entry is add to the map hash
    // table and permissions on the directory are updated to grant bpfd
    // user group access to all the maps in the directory. If this eBPF
    // program is not the owner, then the eBPF program UUID is added to
    // the Used-By array.
    async fn save_map(
        &mut self,
        prog_id: u32,
        map_owner_id: Option<u32>,
        map_pin_path: PathBuf,
    ) -> Result<(), BpfdError> {
        match map_owner_id {
            Some(id) => {
                if let Some(map) = self.maps.get_mut(&id) {
                    map.used_by.push(prog_id);
                } else {
                    return Err(BpfdError::Error("map_owner_id does not exist".to_string()));
                }
            }
            None => {
                let map = BpfMap {
                    map_pin_path: map_pin_path.clone(),
                    used_by: vec![prog_id],
                };

                self.maps.insert(prog_id, map);

                set_dir_permissions(map_pin_path.to_str().unwrap(), MAPS_MODE).await;
            }
        }

        Ok(())
    }

    // This function cleans up a map entry when an eBPF program is
    // being unloaded. If the eBPF program is the map owner, then
    // the map is removed from the hash table and the associated
    // directory is removed. If this eBPF program is referencing a
    // map from another eBPF program, then this eBPF programs UUID
    // is removed from the UsedBy array.
    async fn delete_map(
        &mut self,
        prog_id: u32,
        map_owner_id: Option<u32>,
    ) -> Result<(), BpfdError> {
        // returns the id to be used for maps, it could be the program id OR another program's id.
        let id = map_owner_id.unwrap_or(prog_id);
        // let path = PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", id));
        if let Some(mut map) = self.maps.remove(&id) {
            map.used_by.retain(|value| *value != prog_id);

            if map.used_by.is_empty() {
                fs::remove_dir_all(map.clone().map_pin_path)
                    .await
                    .map_err(|e| BpfdError::Error(format!("can't delete map dir: {e}")))?;
            } else {
                self.maps.insert(id, map);
            }
        } else {
            return Err(BpfdError::Error(
                "map_owner_id does not exist cannot delete".to_string(),
            ));
        }

        Ok(())
    }

    fn rebuild_map_entry(&mut self, prog_id: u32, map_owner_id: Option<u32>) {
        let id = map_owner_id.unwrap_or(prog_id);
        let path = PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", id));

        if let Some(map) = self.maps.get_mut(&id) {
            map.used_by.push(prog_id);
        } else {
            let map = BpfMap {
                map_pin_path: path.clone(),
                used_by: vec![prog_id],
            };
            self.maps.insert(id, map);
        }
    }
}

fn sort_dispatcher_programs(mut programs: Vec<Program>) -> Vec<Program> {
    programs.sort_by_key(|a| (a.priority(), a.attached()));
    for (i, v) in programs.iter_mut().enumerate() {
        v.set_position(Some(i));
    }
    programs
}

// This function returns the map_pin_path for a loaded program, creating it if needed
pub(crate) async fn manage_map_pin_path(id: u32) -> Result<PathBuf, BpfdError> {
    let path = PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", id));

    // if the path already doesn't exist create the maps directory
    if !path.exists() {
        fs::create_dir_all(path.clone())
            .await
            .map_err(|e| BpfdError::Error(format!("can't create map dir: {e}")))?;
    }

    Ok(path)
}

// #[cfg(test)]
// mod tests {
//     use super::*;

//     #[test]
//     fn test_map_index() {
//         struct Case {
//             i_id: u32,
//             i_map_owner_id: Option<u32>,
//             o_map_owner: bool,
//             o_map_index: u32,
//         }
//         const ID_1: u32 = 5049;
//         const ID_2: u32 = 5000;
//         let tt = vec![
//             Case {
//                 i_id: ID_1,
//                 i_map_owner_id: None,
//                 o_map_owner: true,
//                 o_map_index: ID_1,
//             },
//             Case {
//                 i_id: ID_2,
//                 i_map_owner_id: Some(ID_1),
//                 o_map_owner: false,
//                 o_map_index: ID_1,
//             },
//         ];
//         for t in tt {
//             let (map_owner, map_index) = get_map_index(t.i_id, t.i_map_owner_id);
//             assert_eq!(map_owner, t.o_map_owner);
//             assert_eq!(map_index, t.o_map_index);
//         }
//     }

//     #[test]
//     fn test_calc_map_pin_path() {
//         struct Case {
//             i_id: u32,
//             i_map_owner_id: Option<u32>,
//             o_map_owner: bool,
//             o_map_pin_path: PathBuf,
//         }
//         const ID_1: u32 = 5049;
//         const ID_2: u32 = 5000;
//         let tt = vec![
//             Case {
//                 i_id: ID_1,
//                 i_map_owner_id: None,
//                 o_map_owner: true,
//                 o_map_pin_path: PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", ID_1.to_string())),
//             },
//             Case {
//                 i_id: ID_2,
//                 i_map_owner_id: Some(ID_1),
//                 o_map_owner: false,
//                 o_map_pin_path: PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", ID_1.to_string())),
//             },
//         ];
//         for t in tt {
//             let (map_owner, map_pin_path) = calc_map_pin_path(t.i_id, t.i_map_owner_id);
//             info!("{map_owner} {:?}", map_pin_path);
//             assert_eq!(map_owner, t.o_map_owner);
//             assert_eq!(map_pin_path, t.o_map_pin_path);
//         }
//     }
// }
