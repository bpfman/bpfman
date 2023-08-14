// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, io::BufReader, mem};

use aya::{
    include_bytes_aligned,
    programs::{
        links::FdLink,
        tc::{self, SchedClassifierLink, TcOptions},
        Extension, Link, SchedClassifier, TcAttachType,
    },
    Bpf, BpfLoader,
};
use bpfd_api::util::directories::*;
use log::debug;
use serde::{Deserialize, Serialize};

use super::Dispatcher;
use crate::{
    bpf::manage_map_pin_path,
    command::{
        Direction,
        Direction::{Egress, Ingress},
        Program, TcProgram,
    },
    dispatcher_config::TcDispatcherConfig,
    errors::BpfdError,
};

const DEFAULT_PRIORITY: u32 = 50; // Default priority for user programs in the dispatcher
const TC_DISPATCHER_PRIORITY: u16 = 50; // Default TC priority for TC Dispatcher
const DISPATCHER_PROGRAM_NAME: &str = "tc_dispatcher";

static DISPATCHER_BYTES: &[u8] = include_bytes_aligned!("../../../.output/tc_dispatcher.bpf.o");

#[derive(Debug, Serialize, Deserialize)]
pub struct TcDispatcher {
    pub(crate) revision: u32,
    if_index: u32,
    if_name: String,
    direction: Direction,
    priority: u16,
    handle: Option<u32>,
    #[serde(skip)]
    loader: Option<Bpf>,
}

impl TcDispatcher {
    pub(crate) async fn new(
        direction: Direction,
        if_index: &u32,
        if_name: String,
        programs: &mut [Program],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
    ) -> Result<TcDispatcher, BpfdError> {
        debug!("TcDispatcher::new() for if_index {if_index}, revision {revision}");
        // Cast program list to TcPrograms.
        // TODO (astoycos) Maybe in a step earlier?
        let mut extensions: Vec<&mut TcProgram> = programs
            .iter_mut()
            .filter_map(|v| match v {
                Program::Tc(p) => Some(p),
                _ => None,
            })
            .collect();

        let mut chain_call_actions = [0; 10];

        for p in extensions.iter() {
            chain_call_actions[p.current_position.unwrap()] = p.proceed_on.mask()
        }

        let config = TcDispatcherConfig {
            num_progs_enabled: extensions.len() as u8,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        debug!("tc dispatcher config: {:?}", config);

        let mut loader = BpfLoader::new()
            .set_global("CONFIG", &config, true)
            .load(DISPATCHER_BYTES)?;

        let dispatcher: &mut SchedClassifier = loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        dispatcher.load()?;

        let base = match direction {
            Ingress => RTDIR_FS_TC_INGRESS,
            Egress => RTDIR_FS_TC_EGRESS,
        };
        let path = format!("{base}/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        let mut dispatcher = TcDispatcher {
            revision,
            if_index: *if_index,
            if_name,
            direction,
            priority: TC_DISPATCHER_PRIORITY,
            handle: None,
            loader: Some(loader),
        };
        dispatcher.attach_extensions(&mut extensions).await?;
        dispatcher.attach(old_dispatcher)?;
        dispatcher.save()?;
        Ok(dispatcher)
    }

    fn attach(&mut self, old_dispatcher: Option<Dispatcher>) -> Result<(), BpfdError> {
        debug!(
            "TcDispatcher::attach() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let iface = self.if_name.clone();
        // Add clsact qdisc to the interface. This is harmless if it has already been added.
        let _ = tc::qdisc_add_clsact(&iface);

        let new_dispatcher: &mut SchedClassifier = self
            .loader
            .as_mut()
            .ok_or(BpfdError::NotLoaded)?
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        let attach_type = match self.direction {
            Direction::Ingress => TcAttachType::Ingress,
            Direction::Egress => TcAttachType::Egress,
        };

        let link_id = new_dispatcher.attach_with_options(
            &iface,
            attach_type,
            TcOptions {
                priority: self.priority,
                ..Default::default()
            },
        )?;

        let link = new_dispatcher.take_link(link_id)?;
        self.handle = Some(link.handle());
        mem::forget(link);

        if let Some(Dispatcher::Tc(mut d)) = old_dispatcher {
            // If the old dispatcher was not attached when the new dispatcher
            // was attached above, the new dispatcher may get the same handle
            // as the old one had.  If this happens, the new dispatcher will get
            // detached if we do a full delete, so don't do it.
            if d.handle != self.handle {
                d.delete(true)?;
            } else {
                d.delete(false)?;
            }
        }

        Ok(())
    }

    async fn attach_extensions(
        &mut self,
        extensions: &mut [&mut TcProgram],
    ) -> Result<(), BpfdError> {
        debug!(
            "TcDispatcher::attach_extensions() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let if_index = self.if_index;
        let dispatcher: &mut SchedClassifier = self
            .loader
            .as_mut()
            .ok_or(BpfdError::NotLoaded)?
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        // astoycos: Already sorted
        //extensions.sort_by(|(_, a), (_, b)| a.info.current_position.cmp(&b.info.current_position));

        for (i, v) in extensions.iter_mut().enumerate() {
            if v.attached {
                // kernel info should already be populated
                let id = v
                    .data
                    .kernel_info
                    .clone()
                    .expect("program is already attached, kernel info must be set")
                    .id;
                let mut ext = Extension::from_pin(format!("{RTDIR_FS}/prog_{id}"))?;
                let target_fn = format!("prog{i}");
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let base = match self.direction {
                    Direction::Ingress => RTDIR_FS_TC_INGRESS,
                    Direction::Egress => RTDIR_FS_TC_EGRESS,
                };
                let path = format!("{base}/dispatcher_{if_index}_{}/link_{id}", self.revision);
                new_link.pin(path).map_err(BpfdError::UnableToPinLink)?;
            } else {
                let program_bytes = v.data.program_bytes().await?;
                let mut bpf = BpfLoader::new();

                match &v.data.global_data {
                    Some(d) => {
                        for (name, value) in d {
                            bpf.set_global(name, value.as_slice(), true);
                        }
                    }
                    None => debug!("no global data to set for tcProgram"),
                }

                let loader = bpf.allow_unsupported_maps().extension(&v.data.name);

                if let Some(p) = v.data.map_pin_path.clone() {
                    loader.map_pin_path(p);
                };

                // Load program
                let mut bpf: Bpf = loader
                    .load(&program_bytes)
                    .map_err(BpfdError::BpfLoadError)?;

                let ext: &mut Extension = bpf
                    .program_mut(&v.data.name.clone())
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.data.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(
                    dispatcher
                        .fd()
                        .expect("tc dispatcher fd should be set")
                        .try_clone()?,
                    &target_fn,
                )?;

                v.data.kernel_info = Some(ext.program_info()?.try_into()?);

                let id: u32 = v
                    .data
                    .kernel_info
                    .clone()
                    .expect("kernel info must be set after load")
                    .id;

                ext.pin(format!("{RTDIR_FS}/prog_{id}"))
                    .map_err(BpfdError::UnableToPinProgram)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                let base = match self.direction {
                    Direction::Ingress => RTDIR_FS_TC_INGRESS,
                    Direction::Egress => RTDIR_FS_TC_EGRESS,
                };
                fd_link
                    .pin(format!(
                        "{base}/dispatcher_{if_index}_{}/link_{id}",
                        self.revision,
                    ))
                    .map_err(BpfdError::UnableToPinLink)?;

                // Pin all maps (except for .rodata and .bss) by name and set map pin path
                if v.data.map_pin_path.is_none() {
                    let path = manage_map_pin_path(id).await?;
                    for (name, map) in bpf.maps_mut() {
                        if name.contains(".rodata") || name.contains(".bss") {
                            continue;
                        }
                        map.pin(name, path.join(name))
                            .map_err(BpfdError::UnableToPinMap)?;
                    }

                    v.data.map_pin_path = Some(path);
                }
            }
        }
        Ok(())
    }

    fn save(&self) -> Result<(), BpfdError> {
        debug!(
            "TcDispatcher::save() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let base = match self.direction {
            Direction::Ingress => RTDIR_TC_INGRESS_DISPATCHER,
            Direction::Egress => RTDIR_TC_EGRESS_DISPATCHER,
        };
        let path = format!("{base}/{}_{}", self.if_index, self.revision);
        serde_json::to_writer(&fs::File::create(path).unwrap(), &self)
            .map_err(|e| BpfdError::Error(format!("can't save state: {e}")))?;
        Ok(())
    }

    pub(crate) fn load(
        if_index: u32,
        direction: Direction,
        revision: u32,
    ) -> Result<Self, anyhow::Error> {
        debug!("TcDispatcher::load() for if_index {if_index}, revision {revision}");
        let dir = match direction {
            Direction::Ingress => RTDIR_TC_INGRESS_DISPATCHER,
            Direction::Egress => RTDIR_TC_EGRESS_DISPATCHER,
        };
        let path = format!("{dir}/{if_index}_{revision}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        // TODO: We should check the bpffs paths here to for pinned links etc...
        Ok(prog)
    }

    pub(crate) fn delete(&mut self, full: bool) -> Result<(), BpfdError> {
        debug!(
            "TcDispatcher::delete() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let base = match self.direction {
            Direction::Ingress => RTDIR_TC_INGRESS_DISPATCHER,
            Direction::Egress => RTDIR_TC_EGRESS_DISPATCHER,
        };
        let path = format!("{base}/{}_{}", self.if_index, self.revision);
        fs::remove_file(path)
            .map_err(|e| BpfdError::Error(format!("unable to cleanup state: {e}")))?;

        let base = match self.direction {
            Direction::Ingress => RTDIR_FS_TC_INGRESS,
            Direction::Egress => RTDIR_FS_TC_EGRESS,
        };
        let path = format!("{base}/dispatcher_{}_{}", self.if_index, self.revision);
        fs::remove_dir_all(path)
            .map_err(|e| BpfdError::Error(format!("unable to cleanup state: {e}")))?;

        if full {
            // Also detach the old dispatcher.
            if let Some(old_handle) = self.handle {
                let attach_type = match self.direction {
                    Direction::Ingress => TcAttachType::Ingress,
                    Direction::Egress => TcAttachType::Egress,
                };
                if let Ok(old_link) = SchedClassifierLink::attached(
                    &self.if_name,
                    attach_type,
                    self.priority,
                    old_handle,
                ) {
                    let detach_result = old_link.detach();
                    match detach_result {
                        Ok(_) => debug!(
                            "TC dispatcher {}, {}, {}, {} sucessfully detached",
                            self.if_name, self.direction, self.priority, old_handle
                        ),
                        Err(_) => debug!(
                            "TC dispatcher {}, {}, {}, {} not attached when detach attempted",
                            self.if_name, self.direction, self.priority, old_handle
                        ),
                    }
                }
            };
        }
        Ok(())
    }

    pub(crate) fn if_name(&self) -> String {
        self.if_name.clone()
    }
}
