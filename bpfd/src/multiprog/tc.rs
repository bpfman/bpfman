// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, io::BufReader};

use aya::{
    include_bytes_aligned,
    programs::{
        links::FdLink,
        tc::{self, SchedClassifierLink},
        Extension, Link, PinnedProgram, SchedClassifier, TcAttachType,
    },
    Bpf, BpfLoader,
};
use bpfd_api::util::directories::*;
use bpfd_common::TcDispatcherConfig;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use super::Dispatcher;
use crate::{
    command::{
        Direction,
        Direction::{Egress, Ingress},
        Program, TcProgram,
    },
    errors::BpfdError,
};

const DEFAULT_PRIORITY: u32 = 50;
const MIN_TC_DISPATCHER_PRIORITY: u16 = 50;
const MAX_TC_DISPATCHER_PRIORITY: u16 = 49;
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";
pub const TC_ACT_PIPE: i32 = 3;

static DISPATCHER_BYTES: &[u8] =
    include_bytes_aligned!("../../../target/bpfel-unknown-none/release/tc_dispatcher.bpf.o");

#[derive(Debug, Serialize, Deserialize)]
pub struct TcDispatcher {
    pub(crate) revision: u32,
    current_pri: u16,
    if_index: u32,
    if_name: String,
    direction: Direction,
    #[serde(skip)]
    loader: Option<Bpf>,
    #[serde(skip)]
    link: Option<SchedClassifierLink>,
}

impl TcDispatcher {
    pub(crate) fn new(
        direction: Direction,
        if_index: &u32,
        if_name: String,
        programs: &[(Uuid, Program)],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
    ) -> Result<TcDispatcher, BpfdError> {
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
            current_pri: 0,
            if_index: *if_index,
            if_name,
            direction,
            loader: Some(loader),
            link: None,
        };
        dispatcher.attach_extensions(&mut extensions)?;
        dispatcher.attach(old_dispatcher)?;
        dispatcher.save()?;
        Ok(dispatcher)
    }

    fn attach(&mut self, old_dispatcher: Option<Dispatcher>) -> Result<(), BpfdError> {
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

        if let Some(Dispatcher::Tc(mut d)) = old_dispatcher {
            self.current_pri = d.current_pri - 1;
            if self.current_pri < MAX_TC_DISPATCHER_PRIORITY {
                self.current_pri = MIN_TC_DISPATCHER_PRIORITY
            }
            let link_id = new_dispatcher.attach(&iface, attach_type, self.current_pri)?;
            let link = new_dispatcher.take_link(link_id)?;
            self.link = Some(link);
            // FIXME: TcLinks should detach on drop
            if let Some(old_link) = d.link.take() {
                old_link.detach()?;
            }
            d.delete(false)?;
        } else {
            // This is the first tc dispatcher on this interface
            self.current_pri = MIN_TC_DISPATCHER_PRIORITY;
            let link_id = new_dispatcher.attach(&iface, attach_type, MIN_TC_DISPATCHER_PRIORITY)?;
            let link = new_dispatcher.take_link(link_id)?;
            self.link = Some(link);
        }
        Ok(())
    }

    fn attach_extensions(
        &mut self,
        extensions: &mut [(&Uuid, &TcProgram)],
    ) -> Result<(), BpfdError> {
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
                let mut prog = PinnedProgram::from_pin(format!("{RTDIR_FS}/prog_{k}"))?;
                let ext: &mut Extension = prog.as_mut().try_into()?;
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
                new_link.pin(path).map_err(|_| BpfdError::UnableToPin)?;
            } else {
                let mut bpf = BpfLoader::new()
                    .map_pin_path(format!("{RTDIR_FS_MAPS}/{k}"))
                    .extension(&v.data.section_name)
                    .load_file(&v.data.path)
                    .map_err(BpfdError::BpfLoadError)?;

                let ext: &mut Extension = bpf
                    .program_mut(&v.info.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.info.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                ext.pin(format!("{RTDIR_FS}/prog_{k}"))
                    .map_err(|_| BpfdError::UnableToPin)?;
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
                    .map_err(|_| BpfdError::UnableToPin)?;
            }
        }
        Ok(())
    }

    fn save(&self) -> Result<(), BpfdError> {
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
}
