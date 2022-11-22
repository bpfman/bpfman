// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, io::BufReader, path::PathBuf};

use aya::{
    include_bytes_aligned,
    programs::{
        links::{FdLink, PinnedLink},
        Extension, PinnedProgram, Xdp,
    },
    Bpf, BpfLoader,
};
use bpfd_api::{config::XdpMode, util::directories::*};
use bpfd_common::XdpDispatcherConfig;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use super::Dispatcher;
use crate::{
    command::{Program, XdpProgram},
    errors::BpfdError,
};

const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";
pub(crate) const DEFAULT_PRIORITY: u32 = 50;
pub const XDP_PASS: i32 = 2;
pub const XDP_DISPATCHER_RET: i32 = 31;

static DISPATCHER_BYTES: &[u8] =
    include_bytes_aligned!("../../../target/bpfel-unknown-none/release/xdp_dispatcher.bpf.o");

#[derive(Debug, Serialize, Deserialize)]
pub struct XdpDispatcher {
    pub(crate) revision: u32,
    if_index: u32,
    if_name: String,
    mode: XdpMode,
    #[serde(skip)]
    loader: Option<Bpf>,
}

impl XdpDispatcher {
    pub(crate) fn new(
        mode: XdpMode,
        if_index: &u32,
        if_name: String,
        programs: &[(Uuid, Program)],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
    ) -> Result<XdpDispatcher, BpfdError> {
        let mut extensions: Vec<(&Uuid, &XdpProgram)> = programs
            .iter()
            .filter_map(|(k, v)| match v {
                Program::Xdp(p) => Some((k, p)),
                _ => None,
            })
            .collect();
        let mut chain_call_actions = [0; 10];
        extensions.sort_by(|(_, a), (_, b)| a.info.current_position.cmp(&b.info.current_position));
        for (_, p) in extensions.iter() {
            chain_call_actions[p.info.current_position.unwrap()] = p.info.proceed_on.mask();
        }

        let config = XdpDispatcherConfig {
            num_progs_enabled: extensions.len() as u8,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        let mut loader = BpfLoader::new()
            .set_global("CONFIG", &config)
            .load(DISPATCHER_BYTES)?;

        let dispatcher: &mut Xdp = loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        dispatcher.load()?;

        let path = format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        let mut dispatcher = XdpDispatcher {
            if_index: *if_index,
            if_name,
            revision,
            mode,
            loader: Some(loader),
        };
        dispatcher.attach_extensions(&mut extensions)?;
        dispatcher.attach()?;
        dispatcher.save()?;
        if let Some(mut old) = old_dispatcher {
            old.delete(false)?;
        }
        Ok(dispatcher)
    }

    pub(crate) fn attach(&mut self) -> Result<(), BpfdError> {
        let if_index = self.if_index;
        let iface = self.if_name.clone();
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfdError::NotLoaded)?
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        let path = PathBuf::from(format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_link"));
        if path.exists() {
            let pinned_link: FdLink = PinnedLink::from_pin(path).unwrap().into();
            dispatcher
                .attach_to_link(pinned_link.try_into().unwrap())
                .unwrap();
        } else {
            let flags = self.mode.as_flags();
            let link = dispatcher.attach(&iface, flags).unwrap();
            let owned_link = dispatcher.take_link(link)?;
            let path = format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_link");
            let _ = TryInto::<FdLink>::try_into(owned_link)
                .unwrap() // TODO: Don't unwrap, although due to minimum kernel version this shouldn't ever panic
                .pin(path)
                .map_err(|_| BpfdError::UnableToPin)?;
        }
        Ok(())
    }

    fn attach_extensions(
        &mut self,
        extensions: &mut [(&Uuid, &XdpProgram)],
    ) -> Result<(), BpfdError> {
        let if_index = self.if_index;
        let dispatcher: &mut Xdp = self
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
                let path = format!(
                    "{RTDIR_FS_XDP}/dispatcher_{if_index}_{}/link_{k}",
                    self.revision
                );
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
                fd_link
                    .pin(format!(
                        "{RTDIR_FS_XDP}/dispatcher_{if_index}_{}/link_{k}",
                        self.revision,
                    ))
                    .map_err(|_| BpfdError::UnableToPin)?;
            }
        }
        Ok(())
    }

    fn save(&self) -> Result<(), BpfdError> {
        let path = format!("{RTDIR_XDP_DISPATCHER}/{}_{}", self.if_index, self.revision);
        serde_json::to_writer(&fs::File::create(path).unwrap(), &self)
            .map_err(|e| BpfdError::Error(format!("can't save state: {e}")))?;
        Ok(())
    }

    pub fn load(if_index: u32, revision: u32) -> Result<Self, anyhow::Error> {
        let path = format!("{RTDIR_XDP_DISPATCHER}/{if_index}_{revision}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        // TODO: We should check the bpffs paths here to for pinned links etc...
        Ok(prog)
    }

    pub(crate) fn delete(&self, full: bool) -> Result<(), BpfdError> {
        let path = format!("{RTDIR_XDP_DISPATCHER}/{}_{}", self.if_index, self.revision);
        fs::remove_file(path)
            .map_err(|e| BpfdError::Error(format!("unable to cleanup state: {e}")))?;

        let path = format!(
            "{RTDIR_FS_XDP}/dispatcher_{}_{}",
            self.if_index, self.revision
        );
        fs::remove_dir_all(path)
            .map_err(|e| BpfdError::Error(format!("unable to cleanup state: {e}")))?;
        if full {
            let path_link = format!("{RTDIR_FS_XDP}/dispatcher_{}_link", self.if_index);
            fs::remove_file(path_link)
                .map_err(|e| BpfdError::Error(format!("unable to cleanup state: {e}")))?;
        }
        Ok(())
    }

    pub(crate) fn if_name(&self) -> String {
        self.if_name.clone()
    }
}
