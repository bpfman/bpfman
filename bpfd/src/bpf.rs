// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, convert::TryInto};

use anyhow::anyhow;
use aya::{
    programs::{links::FdLink, trace_point::TracePointLink, TracePoint},
    BpfLoader,
};
use bpfd_api::{config::Config, util::directories::*, ProgramType};
use log::{debug, error, info};
use tokio::{fs, select, sync::mpsc};
use uuid::Uuid;

use crate::errors::BpfdError::Error;
use crate::{
    command::{
        self, Command, Direction,
        Direction::{Egress, Ingress},
        LoadTCArgs, LoadTracepointArgs, LoadUprobeArgs, LoadXDPArgs, Program, ProgramData,
        ProgramInfo, TcProgram, TcProgramInfo, TracepointProgram, TracepointProgramInfo,
        UnloadArgs, XdpProgram, XdpProgramInfo,
    },
    errors::BpfdError,
    multiprog::{Dispatcher, DispatcherId, DispatcherInfo, TcDispatcher, XdpDispatcher},
    oci_utils::image_manager::get_bytecode_from_image_store,
    serve::shutdown_handler,
    utils::{get_ifindex, read, set_dir_permissions},
};

const SUPERUSER: &str = "bpfctl";
const MAPS_MODE: u32 = 0o0660;

pub(crate) struct BpfManager {
    config: Config,
    dispatchers: HashMap<DispatcherId, Dispatcher>,
    programs: HashMap<Uuid, Program>,
    commands: mpsc::Receiver<Command>,
}

impl BpfManager {
    pub(crate) fn new(config: Config, commands: mpsc::Receiver<Command>) -> Self {
        Self {
            config,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
            commands,
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

    pub(crate) async fn add_program(
        &mut self,
        program: Program,
        id: Option<Uuid>,
    ) -> Result<Uuid, BpfdError> {
        debug!("BpfManager::add_program()");

        let uuid = match id {
            Some(id) => {
                debug!("Using provided program UUID: {}", id);
                if self.programs.contains_key(&id) {
                    return Err(BpfdError::PassedUUIDInUse(id));
                }
                id
            }
            None => {
                debug!("Generating new program UUID");
                Uuid::new_v4()
            }
        };

        match program {
            Program::Xdp(_) | Program::Tc(_) => self.add_multi_attach_program(program, uuid).await,
            Program::Tracepoint(_) => self.add_single_attach_program(program, uuid).await,
        }
    }

    pub(crate) async fn add_multi_attach_program(
        &mut self,
        program: Program,
        id: Uuid,
    ) -> Result<Uuid, BpfdError> {
        debug!("BpfManager::add_multi_attach_program()");
        let map_pin_path = format!("{RTDIR_FS_MAPS}/{id}");
        fs::create_dir_all(map_pin_path.clone())
            .await
            .map_err(|e| BpfdError::Error(format!("can't create map dir: {e}")))?;

        let program_bytes = if program
            .data()
            .path
            .clone()
            .contains(BYTECODE_IMAGE_CONTENT_STORE)
        {
            get_bytecode_from_image_store(program.data().path.clone()).await?
        } else {
            read(program.data().path.clone()).await?
        };

        let mut ext_loader = BpfLoader::new()
            .extension(&program.data().section_name)
            .map_pin_path(map_pin_path.clone())
            .load(&program_bytes)?;

        match ext_loader.program_mut(&program.data().section_name) {
            Some(_) => Ok(()),
            None => {
                let _ = fs::remove_dir_all(map_pin_path).await;
                Err(BpfdError::SectionNameNotValid(
                    program.data().section_name.clone(),
                ))
            }
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
        let dispatcher = Dispatcher::new(if_config, &programs, next_revision, old_dispatcher)
            .await
            .or_else(|e| {
                let prog = self.programs.remove(&id).unwrap();
                prog.delete(id).map_err(|_| {
                    BpfdError::Error(
                        "new dispatcher cleanup failed, unable to delete program data".to_string(),
                    )
                })?;
                Err(e)
            })?;
        self.dispatchers.insert(did, dispatcher);
        if let Some(p) = self.programs.get_mut(&id) {
            p.set_attached()
        };
        Ok(id)
    }

    pub(crate) async fn add_single_attach_program(
        &mut self,
        p: Program,
        id: Uuid,
    ) -> Result<Uuid, BpfdError> {
        debug!("BpfManager::add_single_attach_program()");
        if let Program::Tracepoint(ref program) = p {
            let parts: Vec<&str> = program.info.tracepoint.split('/').collect();
            if parts.len() != 2 {
                return Err(BpfdError::InvalidAttach(
                    program.info.tracepoint.to_string(),
                ));
            }
            let category = parts[0].to_owned();
            let name = parts[1].to_owned();

            let map_pin_path = format!("{RTDIR_FS_MAPS}/{id}");
            fs::create_dir_all(map_pin_path.clone())
                .await
                .map_err(|e| BpfdError::Error(format!("can't create map dir: {e}")))?;

            let program_bytes = if program
                .data
                .path
                .clone()
                .contains(BYTECODE_IMAGE_CONTENT_STORE)
            {
                get_bytecode_from_image_store(program.data.path.clone()).await?
            } else {
                read(program.data.path.clone()).await?
            };

            let mut loader = BpfLoader::new();

            for (name, value) in &program.data.global_data {
                loader.set_global(name, value.as_slice());
            }

            let mut loader = loader
                .map_pin_path(map_pin_path.clone())
                .load(&program_bytes)?;

            let tracepoint: &mut TracePoint = match loader.program_mut(&program.data.section_name) {
                Some(p) => p.try_into(),
                None => {
                    let _ = fs::remove_dir_all(map_pin_path).await;
                    return Err(BpfdError::SectionNameNotValid(
                        program.data.section_name.clone(),
                    ));
                }
            }?;

            tracepoint.load()?;
            p.save(id)
                .map_err(|_| BpfdError::Error("unable to persist program data".to_string()))?;
            self.programs.insert(id, p);

            let link_id = tracepoint.attach(&category, &name).or_else(|e| {
                let prog = self.programs.remove(&id).unwrap();
                prog.delete(id).map_err(|_| {
                    BpfdError::Error(
                        "new dispatcher cleanup failed, unable to delete program data".to_string(),
                    )
                })?;
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
                    prog.delete(id).map_err(|_| {
                        BpfdError::Error(
                            "new dispatcher cleanup failed, unable to delete program data"
                                .to_string(),
                        )
                    })?;
                    Err(BpfdError::UnableToPinProgram(e))
                })?;

            Ok(id)
        } else {
            panic!("not a tracepoint program")
        }
    }

    pub(crate) async fn remove_program(
        &mut self,
        id: Uuid,
        owner: String,
    ) -> Result<(), BpfdError> {
        debug!("BpfManager::remove_program() id: {id}");
        if let Some(prog) = self.programs.get(&id) {
            if !(prog.owner() == &owner || owner == SUPERUSER) {
                return Err(BpfdError::NotAuthorized);
            }
        } else {
            debug!("InvalidID: {id}");
            return Err(BpfdError::InvalidID);
        }

        let prog = self.programs.remove(&id).unwrap();

        prog.delete(id)
            .map_err(|_| BpfdError::Error("unable to delete program data".to_string()))?;

        match prog {
            Program::Xdp(_) | Program::Tc(_) => self.remove_multi_attach_program(prog).await,
            Program::Tracepoint(_) => Ok(()),
        }
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
        debug!("next_revision = {next_revision}");
        let dispatcher =
            Dispatcher::new(if_config, &programs, next_revision, old_dispatcher).await?;
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
            let programs = self.collect_programs(program_type, if_index, direction);

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

            let dispatcher =
                Dispatcher::new(if_config, &programs, next_revision, old_dispatcher).await?;
            self.dispatchers.insert(did, dispatcher);
        } else {
            debug!("No dispatcher found in rebuild_multiattach_dispatcher() for {did:?}");
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self) -> Result<Vec<ProgramInfo>, BpfdError> {
        debug!("BpfManager::list_programs()");
        let programs = self
            .programs
            .iter()
            .map(|(id, p)| match p {
                Program::Xdp(p) => ProgramInfo {
                    id: *id,
                    name: p.data.section_name.to_string(),
                    location: p.data.location.clone(),
                    program_type: ProgramType::Xdp as i32,
                    attach_info: crate::command::AttachInfo::Xdp(crate::command::XdpAttachInfo {
                        iface: p.info.if_name.to_string(),
                        priority: p.info.metadata.priority,
                        proceed_on: p.info.proceed_on.clone(),
                        position: p.info.current_position.unwrap_or_default() as i32,
                    }),
                },
                Program::Tracepoint(p) => ProgramInfo {
                    id: *id,
                    name: p.data.section_name.to_string(),
                    location: p.data.location.clone(),
                    program_type: ProgramType::Tracepoint as i32,
                    attach_info: crate::command::AttachInfo::Tracepoint(
                        crate::command::TracepointAttachInfo {
                            tracepoint: p.info.tracepoint.to_string(),
                        },
                    ),
                },
                Program::Tc(p) => ProgramInfo {
                    id: *id,
                    name: p.data.section_name.to_string(),
                    location: p.data.location.clone(),
                    program_type: ProgramType::Tc as i32,
                    attach_info: crate::command::AttachInfo::Tc(crate::command::TcAttachInfo {
                        iface: p.info.if_name.to_string(),
                        priority: p.info.metadata.priority,
                        proceed_on: p.info.proceed_on.clone(),
                        direction: p.direction,
                        position: p.info.current_position.unwrap_or_default() as i32,
                    }),
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
                        Command::LoadXDP(args) => self.load_xdp_command(args).await.unwrap(),
                        Command::LoadTC(args) => self.load_tc_command(args).await.unwrap(),
                        Command::LoadTracepoint(args) => self.load_tracepoint_command(args).await.unwrap(),
                        Command::LoadUprobe(args) => self.load_uprobe_command(args).unwrap(),
                        Command::Unload(args) => self.unload_command(args).await.unwrap(),
                        Command::List { responder } => {
                            let progs = self.list_programs();
                            // Ignore errors as they'll be propagated to caller in the RPC status
                            let _ = responder.send(progs);
                        }
                    }
                }
            }
        }
        info!("Stopping processing commands");
    }

    async fn load_xdp_command(&mut self, args: LoadXDPArgs) -> anyhow::Result<()> {
        let res = if let Ok(if_index) = get_ifindex(&args.iface) {
            match ProgramData::new(
                args.location,
                args.section_name.clone(),
                args.global_data,
                args.username,
            )
            .await
            {
                Ok(prog_data) => {
                    let prog = Program::Xdp(XdpProgram {
                        data: prog_data.clone(),
                        info: XdpProgramInfo {
                            if_index,
                            current_position: None,
                            metadata: command::Metadata {
                                priority: args.priority,
                                // This could have been overridden by image tags
                                name: prog_data.section_name,
                                attached: false,
                            },
                            proceed_on: args.proceed_on,
                            if_name: args.iface,
                        },
                    });
                    self.add_program(prog, args.id).await
                }
                Err(e) => Err(e),
            }
        } else {
            Err(BpfdError::InvalidInterface)
        };

        // If program was successfully loaded, allow map access by bpfd group members.
        if let Ok(uuid) = &res {
            let maps_dir = format!("{RTDIR_FS_MAPS}/{uuid}");
            set_dir_permissions(&maps_dir, MAPS_MODE).await;
        }

        // Ignore errors as they'll be propagated to caller in the RPC status
        let _ = args.responder.send(res);
        Ok(())
    }

    async fn load_tc_command(&mut self, args: LoadTCArgs) -> anyhow::Result<()> {
        let res = if let Ok(if_index) = get_ifindex(&args.iface) {
            match ProgramData::new(
                args.location,
                args.section_name,
                args.global_data,
                args.username,
            )
            .await
            {
                Ok(prog_data) => {
                    let prog = Program::Tc(TcProgram {
                        data: prog_data.clone(),
                        direction: args.direction,
                        info: TcProgramInfo {
                            if_index,
                            current_position: None,
                            metadata: command::Metadata {
                                priority: args.priority,
                                // This could have been overridden by image tags
                                name: prog_data.section_name,
                                attached: false,
                            },
                            proceed_on: args.proceed_on,
                            if_name: args.iface,
                        },
                    });
                    self.add_program(prog, args.id).await
                }
                Err(e) => Err(e),
            }
        } else {
            Err(BpfdError::InvalidInterface)
        };

        // If program was successfully loaded, allow map access by bpfd group members.
        if let Ok(uuid) = &res {
            let maps_dir = format!("{RTDIR_FS_MAPS}/{}", uuid.clone());
            set_dir_permissions(&maps_dir, MAPS_MODE).await;
        }

        // Ignore errors as they'll be propagated to caller in the RPC status
        let _ = args.responder.send(res);
        Ok(())
    }

    async fn load_tracepoint_command(&mut self, args: LoadTracepointArgs) -> anyhow::Result<()> {
        let res = {
            match ProgramData::new(
                args.location,
                args.section_name,
                args.global_data,
                args.username,
            )
            .await
            {
                Ok(prog_data) => {
                    let prog = Program::Tracepoint(TracepointProgram {
                        data: prog_data,
                        info: TracepointProgramInfo {
                            tracepoint: args.tracepoint,
                        },
                    });
                    self.add_program(prog, args.id).await
                }
                Err(e) => Err(e),
            }
        };

        // If program was successfully loaded, allow map access by bpfd group members.
        if let Ok(uuid) = &res {
            let maps_dir = format!("{RTDIR_FS_MAPS}/{uuid}");
            set_dir_permissions(&maps_dir, MAPS_MODE).await;
        }

        // Ignore errors as they'll be propagated to caller in the RPC status
        let _ = args.responder.send(res);
        Ok(())
    }

    fn load_uprobe_command(&mut self, args: LoadUprobeArgs) -> anyhow::Result<()> {
        error!(
            "uprobe programs not supported yet: {:?} {:?} {:?} {:?} {:?} {:?} {:?} {:?} {:?}",
            args.location,
            args.id,
            args.section_name,
            args.global_data,
            args.fn_name,
            args.offset,
            args.target,
            args.pid,
            args.username,
        );
        let res = Err(Error("uprobes not supported yet".to_string()));
        let _ = args.responder.send(res);
        Ok(())
    }

    async fn unload_command(&mut self, args: UnloadArgs) -> anyhow::Result<()> {
        let res = self.remove_program(args.id, args.username).await;
        // Ignore errors as they'll be propagated to caller in the RPC status
        let _ = args.responder.send(res);
        Ok(())
    }
}
