// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{
    collections::HashMap,
    convert::TryInto,
    path::{Path, PathBuf},
};

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
use tokio::{
    fs, select,
    sync::{
        mpsc::{Receiver, Sender},
        oneshot,
    },
};
use uuid::Uuid;

use crate::{
    command::{
        BpfMap, Command, Direction,
        Direction::{Egress, Ingress},
        Program, PullBytecodeArgs, UnloadArgs,
    },
    errors::BpfdError,
    multiprog::{Dispatcher, DispatcherId, DispatcherInfo, TcDispatcher, XdpDispatcher},
    oci_utils::image_manager::Command as ImageManagerCommand,
    serve::shutdown_handler,
    utils::{get_ifindex, set_dir_permissions},
};

const MAPS_MODE: u32 = 0o0660;

pub(crate) struct BpfManager {
    config: Config,
    dispatchers: HashMap<DispatcherId, Dispatcher>,
    programs: HashMap<Uuid, Program>,
    maps: HashMap<Uuid, BpfMap>,
    commands: Receiver<Command>,
    image_manager: Sender<ImageManagerCommand>,
}

impl BpfManager {
    pub(crate) fn new(
        config: Config,
        commands: Receiver<Command>,
        image_manager: Sender<ImageManagerCommand>,
    ) -> Self {
        Self {
            config,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
            maps: HashMap::new(),
            commands,
            image_manager,
        }
    }

    pub(crate) async fn rebuild_state(&mut self) -> Result<(), anyhow::Error> {
        debug!("BpfManager::rebuild_state()");
        let mut programs_dir = fs::read_dir(RTDIR_PROGRAMS).await?;
        while let Some(entry) = programs_dir.next_entry().await? {
            let uuid = entry.file_name().to_string_lossy().parse().unwrap();
            let mut program = Program::load(uuid)
                .map_err(|e| BpfdError::Error(format!("cant read program state {e}")))?;
            // TODO: Should probably check for pinned prog on bpffs rather than assuming they are attached
            program.set_attached();
            debug!("rebuilding state for program {}", uuid);
            self.rebuild_map_entry(uuid, program.data()?.map_owner_id());
            self.programs.insert(uuid, program);
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

    pub(crate) async fn add_program(&mut self, mut program: Program) -> Result<Uuid, BpfdError> {
        debug!("BpfManager::add_program()");

        let uuid = match program.data()?.id() {
            Some(id) => {
                debug!("Using provided program UUID: {}", id);
                if self.programs.contains_key(&id) {
                    return Err(BpfdError::PassedUUIDInUse(id));
                }
                id
            }
            None => {
                debug!("Generating new program UUID");
                let id = Uuid::new_v4();
                program.data_mut()?.set_id(Some(id));
                id
            }
        };

        let map_owner_id = program.data()?.map_owner_id();
        let map_pin_path = self.manage_map_pin_path(uuid, map_owner_id).await?;

        program
            .data_mut()?
            .set_map_pin_path(Some(map_pin_path.clone()));

        let program_bytes = program
            .data_mut()?
            .program_bytes(self.image_manager.clone())
            .await?;
        let result = match program {
            Program::Xdp(_) | Program::Tc(_) => {
                program.set_if_index(get_ifindex(&program.if_name().unwrap())?);

                self.add_multi_attach_program(program, uuid, program_bytes)
                    .await
            }
            Program::Tracepoint(_) | Program::Kprobe(_) | Program::Uprobe(_) => {
                self.add_single_attach_program(program, uuid, program_bytes)
                    .await
            }
            Program::Unsupported(_) => panic!("Cannot add unsupported program"),
        };

        if result.is_ok() {
            // Now that program is successfully loaded, update the uuid, maps hash table,
            // and allow access to all maps by bpfd group members.
            self.save_map(uuid, map_owner_id, &map_pin_path).await?;
        } else {
            let _ = self.cleanup_map_pin_path(uuid, map_owner_id).await;
        }

        result
    }

    pub(crate) async fn add_multi_attach_program(
        &mut self,
        program: Program,
        id: Uuid,
        program_bytes: Vec<u8>,
    ) -> Result<Uuid, BpfdError> {
        debug!("BpfManager::add_multi_attach_program()");
        let name = program.data()?.name();
        let map_pin_path = program.data()?.map_pin_path();

        // This load is just to verify the Section Name is valid.
        // The actual load is performed in the XDP or TC logic.
        let mut ext_loader = BpfLoader::new()
            .allow_unsupported_maps()
            .extension(name)
            .map_pin_path(map_pin_path.expect("map_pin_path should be set"))
            .load(&program_bytes)?;

        match ext_loader.program_mut(name) {
            Some(_) => Ok(()),
            None => Err(BpfdError::SectionNameNotValid(name.to_owned())),
        }?;

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

        debug!("next_available_id={next_available_id}");

        let program_type = program.kind();
        let if_index = program.if_index();
        let if_name = program.if_name().unwrap();
        let direction = program.direction();

        let did = program
            .dispatcher_id()
            .ok_or(BpfdError::DispatcherNotRequired)?;

        self.programs.insert(id, program);
        self.sort_programs(program_type, if_index, direction);
        let mut programs = self.collect_programs(program_type, if_index, direction);
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
        let dispatcher = Dispatcher::new(
            if_config,
            &mut programs,
            next_revision,
            old_dispatcher,
            self.image_manager.clone(),
        )
        .await
        .or_else(|e| {
            let prog = self.programs.remove(&id).unwrap();
            prog.delete(id).map_err(BpfdError::BpfdProgramDeleteError)?;
            Err(e)
        })?;
        self.dispatchers.insert(did, dispatcher);

        // update programs with now populated kernel info
        // TODO this data flow should be optimized so that we don't have
        // to re-iterate through the programs.
        programs.iter().for_each(|(i, p)| {
            self.programs.insert(i.to_owned(), p.to_owned());
        });

        if let Some(p) = self.programs.get_mut(&id) {
            p.set_attached();
            p.save(id)
                .map_err(|e| BpfdError::Error(format!("unable to save program state: {e}")))?;
        };

        Ok(id)
    }

    pub(crate) async fn add_single_attach_program(
        &mut self,
        mut p: Program,
        id: Uuid,
        program_bytes: Vec<u8>,
    ) -> Result<Uuid, BpfdError> {
        debug!("BpfManager::add_single_attach_program()");
        let name = p.data()?.name();
        let map_pin_path = p.data()?.map_pin_path();

        let mut loader = BpfLoader::new();

        for (key, value) in p.data()?.global_data() {
            loader.set_global(key, value.as_slice(), true);
        }

        let mut loader = loader
            .allow_unsupported_maps()
            .map_pin_path(map_pin_path.expect("map_pin_path should be set"))
            .load(&program_bytes)?;

        let raw_program = loader
            .program_mut(name)
            .ok_or(BpfdError::SectionNameNotValid(name.to_owned()))?;

        let res = match p {
            Program::Tracepoint(ref mut program) => {
                let parts: Vec<&str> = program.tracepoint.split('/').collect();
                if parts.len() != 2 {
                    return Err(BpfdError::InvalidAttach(program.tracepoint.to_string()));
                }
                let category = parts[0].to_owned();
                let name = parts[1].to_owned();

                let tracepoint: &mut TracePoint = raw_program.try_into()?;

                tracepoint.load()?;
                program
                    .data
                    .set_kernel_info(Some(tracepoint.program_info()?.try_into()?));

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

                Ok(id)
            }
            Program::Kprobe(ref mut program) => {
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

                // verify that the program loaded was the same type as the
                // user requested
                let loaded_probe_type = ProbeType::from(kprobe.kind());
                if requested_probe_type != loaded_probe_type {
                    return Err(BpfdError::Error(format!(
                        "expected {requested_probe_type}, loaded program is {loaded_probe_type}"
                    )));
                }

                program
                    .data
                    .set_kernel_info(Some(kprobe.program_info()?.try_into()?));

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

                Ok(id)
            }
            Program::Uprobe(ref mut program) => {
                let requested_probe_type = match program.retprobe {
                    true => Uretprobe,
                    false => Uprobe,
                };

                let uprobe: &mut UProbe = raw_program.try_into()?;
                uprobe.load()?;

                // verify that the program loaded was the same type as the
                // user requested
                let loaded_probe_type = ProbeType::from(uprobe.kind());
                if requested_probe_type != loaded_probe_type {
                    return Err(BpfdError::Error(format!(
                        "expected {requested_probe_type}, loaded program is {loaded_probe_type}"
                    )));
                }

                program
                    .data
                    .set_kernel_info(Some(uprobe.program_info()?.try_into()?));

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

                Ok(id)
            }
            _ => panic!("not a supported single attach program"),
        };

        if res.is_ok() {
            self.programs.insert(id, p);
            self.programs
                .get(&id)
                .unwrap()
                .save(id)
                // we might want to log or ignore this error instead of returning here...
                // because otherwise it will hide the original error (from res above)
                .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;
        };

        res
    }

    pub(crate) async fn remove_program(&mut self, id: Uuid) -> Result<(), BpfdError> {
        debug!("BpfManager::remove_program() id: {id}");
        let prog = self.programs.remove(&id).unwrap();

        let map_owner_id = prog.data()?.map_owner_id();

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
                    p.if_index() == program.if_index() && p.direction() == program.direction()
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
        let direction = program.direction();

        self.sort_programs(program_type, if_index, direction);

        let mut programs = self.collect_programs(program_type, if_index, direction);

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
            &mut programs,
            next_revision,
            old_dispatcher,
            self.image_manager.clone(),
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

            self.sort_programs(program_type, if_index, direction);
            let mut programs = self.collect_programs(program_type, if_index, direction);

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
                &mut programs,
                next_revision,
                old_dispatcher,
                self.image_manager.clone(),
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

        let mut bpfd_progs: HashMap<u32, Program> = self
            .programs
            .values()
            .map(|p| {
                let kernel_info = p
                    .data()
                    .expect("All Bpfd programs should have ProgramData")
                    .kernel_info()
                    .expect("Loaded Bpfd programs should have kernel information");

                (kernel_info.id, p.to_owned())
            })
            .collect();

        loaded_programs()
            .map(|p| {
                let prog = p.map_err(BpfdError::BpfProgramError)?;
                let prog_id = prog.id();

                match bpfd_progs.remove(&prog_id) {
                    Some(p) => Ok(p),
                    None => Ok(Program::Unsupported(prog.try_into()?)),
                }
            })
            .collect()
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

        extensions.sort_by_key(|(_, b)| {
            (
                b.priority(),
                b.attached().unwrap(),
                b.data()
                    .expect("All Bpfd programs should have ProgramData")
                    .name()
                    .to_owned(),
            )
        });
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

    async fn pull_bytecode(&self, args: PullBytecodeArgs) -> anyhow::Result<()> {
        let (tx, rx) = oneshot::channel();
        self.image_manager
            .send(ImageManagerCommand::Pull {
                image: args.image.image_url,
                pull_policy: args.image.image_pull_policy.clone(),
                username: args.image.username.clone(),
                password: args.image.password.clone(),
                resp: tx,
            })
            .await?;
        let res = match rx.await? {
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

    // This function returns the map_pin_path, and if this eBPF program is
    // the map owner, creates the directory to store the associate maps.
    async fn manage_map_pin_path(
        &mut self,
        id: Uuid,
        map_owner_uuid: Option<Uuid>,
    ) -> Result<PathBuf, BpfdError> {
        let (map_owner, map_pin_path) = calc_map_pin_path(id, map_owner_uuid);

        // If the user provided a UUID of an eBPF program to share a map with,
        // then use that UUID in the directory to create the maps in
        // (path already exists).
        // Otherwise, use the UUID of this program and create the directory.
        if map_owner {
            fs::create_dir_all(map_pin_path.clone())
                .await
                .map_err(|e| BpfdError::Error(format!("can't create map dir: {e}")))?;

            // Return the map_pin_path
            Ok(map_pin_path)
        } else {
            if self.maps.contains_key(&map_owner_uuid.unwrap()) {
                // Return the map_pin_path
                return Ok(map_pin_path);
            }
            Err(BpfdError::Error(
                "map_owner_uuid does not exists".to_string(),
            ))
        }
    }

    // This function is called if manage_map_pin_path() was already called,
    // but the eBPF program failed to load. save_map() has not been called,
    // so self.maps has not been updated for this program.
    // If the user provided a UUID of program to share a map with,
    // then map the directory is still in use and there is nothing to do.
    // Otherwise, manage_map_pin_path() created the map directory so it must
    // deleted.
    async fn cleanup_map_pin_path(
        &mut self,
        id: Uuid,
        map_owner_uuid: Option<Uuid>,
    ) -> Result<(), BpfdError> {
        let (map_owner, map_pin_path) = calc_map_pin_path(id, map_owner_uuid);

        if map_owner {
            let _ = fs::remove_dir_all(map_pin_path.clone())
                .await
                .map_err(|e| BpfdError::Error(format!("can't delete map dir: {e}")));
            Ok(())
        } else {
            Ok(())
        }
    }

    // This function writes the map to the map hash table. If this eBPF
    // program is the map owner, then a new entry is add to the map hash
    // table and permissions on the directory are updated to grant bpfd
    // user group access to all the maps in the directory. If this eBPF
    // program is not the owner, then the eBPF program UUID is added to
    // the Used-By array.
    async fn save_map(
        &mut self,
        id: Uuid,
        map_owner_uuid: Option<Uuid>,
        map_pin_path: &Path,
    ) -> Result<(), BpfdError> {
        let (map_owner, _) = get_map_index(id, map_owner_uuid);

        if map_owner {
            let program = self
                .programs
                .get_mut(&id)
                .expect("Program should be loaded");

            let map = BpfMap { used_by: vec![id] };

            self.maps.insert(id, map);

            // TODO(astoycos) remove external self.maps, keep all map tracking info in Program
            // update programs map_used_by
            program.data_mut()?.set_maps_used_by(Some(vec![id]));

            set_dir_permissions(map_pin_path.to_str().unwrap(), MAPS_MODE).await;
        } else if let Some(map) = self.maps.get_mut(&map_owner_uuid.unwrap()) {
            map.used_by.push(id);

            // If map owner program still exists update it's maps_used_by_field
            if let Some(program) = self.programs.get_mut(&map_owner_uuid.unwrap()) {
                // TODO(astoycos) remove external self.maps, keep all map tracking info in Program
                // update programs map_used_by
                program
                    .data_mut()?
                    .set_maps_used_by(Some(map.used_by.clone()));
            }
        } else {
            return Err(BpfdError::Error(
                "map_owner_uuid does not exists".to_string(),
            ));
        };
        Ok(())
    }

    // This function cleans up a map entry when an eBPF program is
    // being unloaded. If the eBPF program is the map owner, then
    // the map is removed from the hash table and the associated
    // directory is removed. If this eBPF program is referencing a
    // map from another eBPF program, then this eBPF programs UUID
    // is removed from the UsedBy array.
    async fn delete_map(&mut self, id: Uuid, map_owner_id: Option<Uuid>) -> Result<(), BpfdError> {
        let (_, map_index) = get_map_index(id, map_owner_id);

        if let Some(map) = self.maps.get_mut(&map_index.clone()) {
            if let Some(index) = map.used_by.iter().position(|value| *value == id) {
                map.used_by.swap_remove(index);
            }

            // If map owner program still exists update it's maps_used_by_field
            if let Some(program) = self.programs.get_mut(&map_index) {
                // TODO(astoycos) remove external self.maps, keep all map tracking info in Program
                // update programs map_used_by
                program
                    .data_mut()?
                    .set_maps_used_by(Some(map.used_by.clone()));
            }

            if map.used_by.is_empty() {
                let (_, path) = calc_map_pin_path(id, map_owner_id);
                self.maps.remove(&map_index.clone());
                fs::remove_dir_all(path)
                    .await
                    .map_err(|e| BpfdError::Error(format!("can't delete map dir: {e}")))?;
            }
        } else {
            return Err(BpfdError::Error("map_pin_path does not exists".to_string()));
        }

        Ok(())
    }

    fn rebuild_map_entry(&mut self, id: Uuid, map_owner_uuid: Option<Uuid>) {
        let (_, map_index) = get_map_index(id, map_owner_uuid);

        if let Some(map) = self.maps.get_mut(&map_index) {
            map.used_by.push(id);
        } else {
            let map = BpfMap { used_by: vec![id] };
            self.maps.insert(map_index, map);
        }
    }
}

// map_index is a UUID. It is either the programs UUID, or the UUID
// of another program that map_owner_uuid references.
// This function also returns a bool, which indicates if the input UUID
// is the owner of the map (map_owner_uuid is not set) or not (map_owner_uuid
// is set so the eBPF program is referencing another eBPF programs maps).
fn get_map_index(id: Uuid, map_owner_uuid: Option<Uuid>) -> (bool, Uuid) {
    if let Some(uuid) = map_owner_uuid {
        (false, uuid)
    } else {
        (true, id)
    }
}

// map_pin_path is a the directory the maps are located. Currently, it
// is a fixed bpfd location containing the map_index, which is a UUID.
// The UUID is either the programs UUID, or the UUID of another program
// that map_owner_uuid references.
pub fn calc_map_pin_path(id: Uuid, map_owner_uuid: Option<Uuid>) -> (bool, PathBuf) {
    let (map_owner, map_index) = get_map_index(id, map_owner_uuid);
    (
        map_owner,
        PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", map_index)),
    )
}

#[cfg(test)]
mod tests {
    use uuid::{uuid, Uuid};

    use super::*;

    #[test]
    fn test_map_index() {
        struct Case {
            i_id: Uuid,
            i_map_owner_uuid: Option<Uuid>,
            o_map_owner: bool,
            o_map_index: Uuid,
        }
        const UUID_1: Uuid = uuid!("67e55044-10b1-426f-9247-bb680e5fe0c8");
        const UUID_2: Uuid = uuid!("084282a5-a43f-41c3-8f85-c302dc90e091");
        let tt = vec![
            Case {
                i_id: UUID_1,
                i_map_owner_uuid: None,
                o_map_owner: true,
                o_map_index: UUID_1,
            },
            Case {
                i_id: UUID_2,
                i_map_owner_uuid: Some(UUID_1),
                o_map_owner: false,
                o_map_index: UUID_1,
            },
        ];
        for t in tt {
            let (map_owner, map_index) = get_map_index(t.i_id, t.i_map_owner_uuid);
            assert_eq!(map_owner, t.o_map_owner);
            assert_eq!(map_index, t.o_map_index);
        }
    }

    #[test]
    fn test_calc_map_pin_path() {
        struct Case {
            i_id: Uuid,
            i_map_owner_uuid: Option<Uuid>,
            o_map_owner: bool,
            o_map_pin_path: PathBuf,
        }
        const UUID_1: Uuid = uuid!("67e55044-10b1-426f-9247-bb680e5fe0c8");
        const UUID_2: Uuid = uuid!("084282a5-a43f-41c3-8f85-c302dc90e091");
        let tt = vec![
            Case {
                i_id: UUID_1,
                i_map_owner_uuid: None,
                o_map_owner: true,
                o_map_pin_path: PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", UUID_1)),
            },
            Case {
                i_id: UUID_2,
                i_map_owner_uuid: Some(UUID_1),
                o_map_owner: false,
                o_map_pin_path: PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", UUID_1)),
            },
        ];
        for t in tt {
            let (map_owner, map_pin_path) = calc_map_pin_path(t.i_id, t.i_map_owner_uuid);
            info!("{map_owner} {map_pin_path:?}");
            assert_eq!(map_owner, t.o_map_owner);
            assert_eq!(map_pin_path, t.o_map_pin_path);
        }
    }
}
