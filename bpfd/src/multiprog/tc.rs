// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, io::BufReader};

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
use uuid::Uuid;

use super::Dispatcher;
use crate::{
    command::{
        Direction,
        Direction::{Egress, Ingress},
        Program, TcProgram,
    },
    dispatcher_config::TcDispatcherConfig,
    errors::BpfdError,
    oci_utils::image_manager::get_bytecode_from_image_store,
    utils::read,
};

const DEFAULT_PRIORITY: u32 = 50; // Default priority for user programs in the dispatcher
const TC_DISPATCHER_PRIORITY: u16 = 50; // Default TC priority for TC Dispatcher
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";

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
    #[serde(skip)]
    link: Option<SchedClassifierLink>,
}

impl TcDispatcher {
    pub(crate) async fn new(
        direction: Direction,
        if_index: &u32,
        if_name: String,
        programs: &[(Uuid, Program)],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
    ) -> Result<TcDispatcher, BpfdError> {
        debug!("TcDispatcher::new() for if_index {if_index}, revision {revision}");
        let mut extensions: Vec<(&Uuid, &TcProgram)> = programs
            .iter()
            .filter_map(|(k, v)| match v {
                Program::Tc(p) => Some((k, p)),
                _ => None,
            })
            .collect();
        let mut chain_call_actions = [0; 10];
        for (_, v) in extensions.iter() {
            chain_call_actions[v.info.current_position.unwrap()] = v.info.proceed_on.mask()
        }

        let config = TcDispatcherConfig {
            num_progs_enabled: extensions.len() as u8,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        debug!("tc dispatcher config: {:?}", config);

        let mut loader = BpfLoader::new()
            .set_global("CONFIG", &config)
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
            link: None,
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
        self.link = Some(link);

        if let Some(Dispatcher::Tc(mut d)) = old_dispatcher {
            // FIXME: TcLinks should detach on drop
            if let Some(old_link) = d.link.take() {
                old_link.detach()?;
            }
            d.delete(false)?;
        }

        Ok(())
    }

    async fn attach_extensions(
        &mut self,
        extensions: &mut [(&Uuid, &TcProgram)],
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

        extensions.sort_by(|(_, a), (_, b)| a.info.current_position.cmp(&b.info.current_position));

        for (i, (k, v)) in extensions.iter_mut().enumerate() {
            if v.info.metadata.attached {
                let mut ext = Extension::from_pin(format!("{RTDIR_FS}/prog_{k}"))?;
                let target_fn = format!("prog{i}");
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let base = match self.direction {
                    Direction::Ingress => RTDIR_FS_TC_INGRESS,
                    Direction::Egress => RTDIR_FS_TC_EGRESS,
                };
                let path = format!("{base}/dispatcher_{if_index}_{}/link_{k}", self.revision);
                new_link.pin(path).map_err(BpfdError::UnableToPinLink)?;
            } else {
                let mut bpf = BpfLoader::new();

                for (name, value) in &v.data.global_data {
                    bpf.set_global(name, value.as_slice());
                }

                let program_bytes = if v.data.path.clone().contains(BYTECODE_IMAGE_CONTENT_STORE) {
                    get_bytecode_from_image_store(v.data.path.clone()).await?
                } else {
                    read(v.data.path.clone()).await.map_err(|e| {
                        BpfdError::Error(format!("can't read bytecode file from disk {e}"))
                    })?
                };

                let mut bpf = BpfLoader::new()
                    .map_pin_path(format!("{RTDIR_FS_MAPS}/{k}"))
                    .extension(&v.data.section_name)
                    .load(&program_bytes)
                    .map_err(BpfdError::BpfLoadError)?;

                let ext: &mut Extension = bpf
                    .program_mut(&v.info.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.info.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                ext.pin(format!("{RTDIR_FS}/prog_{k}"))
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
                        "{base}/dispatcher_{if_index}_{}/link_{k}",
                        self.revision,
                    ))
                    .map_err(BpfdError::UnableToPinLink)?;
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

    pub(crate) fn delete(&mut self, _full: bool) -> Result<(), BpfdError> {
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
        // FIXME: Dispatcher *SHOULD* be detached when this object is dropped
        if let Some(link) = self.link.take() {
            link.detach()?;
        }
        Ok(())
    }

    pub(crate) fn if_name(&self) -> String {
        self.if_name.clone()
    }

    pub(crate) fn set_link(&mut self) {
        let iface = self.if_name.clone();

        let attach_type = match self.direction {
            Direction::Ingress => TcAttachType::Ingress,
            Direction::Egress => TcAttachType::Egress,
        };

        if let Some(handle) = self.handle {
            if let Ok(link) =
                SchedClassifierLink::attached(&iface, attach_type, self.priority, handle)
            {
                self.link = Some(link);
            }
        };
    }
}
