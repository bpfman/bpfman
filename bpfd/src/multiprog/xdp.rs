// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, io::BufReader, path::PathBuf};

use aya::{
    programs::{
        links::{FdLink, PinnedLink},
        Extension, Xdp,
    },
    Bpf, BpfLoader,
};
use bpfd_api::{config::XdpMode, util::directories::*, ImagePullPolicy};
use log::debug;
use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc::Sender, oneshot};

use super::Dispatcher;
use crate::{
    bpf::{calc_map_pin_path, create_map_pin_path},
    command::{Program, XdpProgram},
    dispatcher_config::XdpDispatcherConfig,
    errors::BpfdError,
    oci_utils::image_manager::{BytecodeImage, Command as ImageManagerCommand},
};

pub(crate) const DEFAULT_PRIORITY: u32 = 50;

#[derive(Debug, Serialize, Deserialize)]
pub struct XdpDispatcher {
    revision: u32,
    if_index: u32,
    if_name: String,
    mode: XdpMode,
    num_extensions: usize,
    #[serde(skip)]
    loader: Option<Bpf>,
    program_name: Option<String>,
}

impl XdpDispatcher {
    pub(crate) async fn new(
        mode: XdpMode,
        if_index: &u32,
        if_name: String,
        programs: &mut [&mut Program],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
        image_manager: Sender<ImageManagerCommand>,
    ) -> Result<XdpDispatcher, BpfdError> {
        debug!("XdpDispatcher::new() for if_index {if_index}, revision {revision}");
        let mut extensions: Vec<&mut XdpProgram> = programs
            .iter_mut()
            .map(|v| match v {
                Program::Xdp(p) => p,
                _ => panic!("All programs should be of type XDP"),
            })
            .collect();

        let mut chain_call_actions = [0; 10];
        extensions.sort_by(|a, b| a.current_position.cmp(&b.current_position));
        for p in extensions.iter() {
            chain_call_actions[p.current_position.unwrap()] = p.proceed_on.mask();
        }

        let config = XdpDispatcherConfig::new(
            extensions.len() as u8,
            0x0,
            chain_call_actions,
            [DEFAULT_PRIORITY; 10],
            [0; 10],
        );

        debug!("xdp dispatcher config: {:?}", config);
        let image = BytecodeImage::new(
            "quay.io/bpfd/xdp-dispatcher:v2".to_string(),
            ImagePullPolicy::IfNotPresent as i32,
            None,
            None,
        );
        let (tx, rx) = oneshot::channel();
        image_manager
            .send(ImageManagerCommand::Pull {
                image: image.image_url.clone(),
                pull_policy: image.image_pull_policy.clone(),
                username: image.username.clone(),
                password: image.password.clone(),
                resp: tx,
            })
            .await
            .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?;

        let (path, section_name) = rx
            .await
            .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?
            .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?;

        let (tx, rx) = oneshot::channel();
        image_manager
            .send(ImageManagerCommand::GetBytecode { path, resp: tx })
            .await
            .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?;
        let program_bytes = rx
            .await
            .map_err(|e| BpfdError::BpfBytecodeError(e.into()))?
            .map_err(BpfdError::BpfBytecodeError)?;
        let mut loader = BpfLoader::new()
            .set_global("conf", &config, true)
            .load(&program_bytes)?;

        let dispatcher: &mut Xdp = loader.program_mut(&section_name).unwrap().try_into()?;

        dispatcher.load()?;

        let path = format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        let mut dispatcher = XdpDispatcher {
            if_index: *if_index,
            if_name,
            revision,
            mode,
            num_extensions: extensions.len(),
            loader: Some(loader),
            program_name: Some(section_name),
        };
        dispatcher.attach_extensions(&mut extensions).await?;
        dispatcher.attach()?;
        dispatcher.save()?;
        if let Some(mut old) = old_dispatcher {
            old.delete(false)?;
        }
        Ok(dispatcher)
    }

    pub(crate) fn attach(&mut self) -> Result<(), BpfdError> {
        debug!(
            "XdpDispatcher::attach() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let if_index = self.if_index;
        let iface = self.if_name.clone();
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfdError::NotLoaded)?
            .program_mut(self.program_name.clone().unwrap().as_str())
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
                .map_err(BpfdError::UnableToPinLink)?;
        }
        Ok(())
    }

    async fn attach_extensions(
        &mut self,
        extensions: &mut [&mut XdpProgram],
    ) -> Result<(), BpfdError> {
        debug!(
            "XdpDispatcher::attach_extensions() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let if_index = self.if_index;
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfdError::NotLoaded)?
            .program_mut(self.program_name.clone().unwrap().as_str())
            .unwrap()
            .try_into()?;
        extensions.sort_by(|a, b| a.current_position.cmp(&b.current_position));
        for (i, v) in extensions.iter_mut().enumerate() {
            if v.attached {
                let id = v
                    .data
                    .kernel_info()
                    .expect("XdpProgram is loaded kernel_info should be set")
                    .id;
                let mut ext = Extension::from_pin(format!("{RTDIR_FS}/prog_{id}"))?;
                let target_fn = format!("prog{i}");
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let path = format!(
                    "{RTDIR_FS_XDP}/dispatcher_{if_index}_{}/link_{id}",
                    self.revision
                );
                new_link.pin(path).map_err(BpfdError::UnableToPinLink)?;
            } else {
                let name = v.data.name();
                let global_data = v.data.global_data();

                let mut bpf = BpfLoader::new();

                bpf.allow_unsupported_maps().extension(name);

                for (name, value) in global_data {
                    bpf.set_global(name, value.as_slice(), true);
                }

                // If map_pin_path is set already it means we need to use a pin
                // path which should already exist on the system.
                if let Some(map_pin_path) = v.data.map_pin_path() {
                    debug!("xdp program {name} is using maps from {:?}", map_pin_path);
                    bpf.map_pin_path(map_pin_path);
                }

                let mut loader = bpf
                    .load(v.data.program_bytes())
                    .map_err(BpfdError::BpfLoadError)?;

                let ext: &mut Extension = loader
                    .program_mut(name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(name.to_string()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(dispatcher.fd()?.try_clone()?, &target_fn)?;
                v.data
                    .set_kernel_info(Some(ext.program_info()?.try_into()?));

                let id = v
                    .data
                    .kernel_info()
                    .expect("kernel info should be set after load")
                    .id;

                ext.pin(format!("{RTDIR_FS}/prog_{id}"))
                    .map_err(BpfdError::UnableToPinProgram)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                fd_link
                    .pin(format!(
                        "{RTDIR_FS_XDP}/dispatcher_{if_index}_{}/link_{id}",
                        self.revision,
                    ))
                    .map_err(BpfdError::UnableToPinLink)?;

                // If this program is the map(s) owner pin all maps (except for .rodata and .bss) by name.
                if v.data.map_pin_path().is_none() {
                    let map_pin_path = calc_map_pin_path(id);
                    v.data.set_map_pin_path(Some(map_pin_path.clone()));
                    create_map_pin_path(&map_pin_path).await?;

                    for (name, map) in loader.maps_mut() {
                        if name.contains(".rodata") || name.contains(".bss") {
                            continue;
                        }
                        map.pin(map_pin_path.join(name))
                            .map_err(BpfdError::UnableToPinMap)?;
                    }
                }
            }
        }
        Ok(())
    }

    fn save(&self) -> Result<(), BpfdError> {
        debug!(
            "XdpDispatcher::save() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let path = format!("{RTDIR_XDP_DISPATCHER}/{}_{}", self.if_index, self.revision);
        serde_json::to_writer(&fs::File::create(path).unwrap(), &self)
            .map_err(|e| BpfdError::Error(format!("can't save state: {e}")))?;
        Ok(())
    }

    pub fn load(if_index: u32, revision: u32) -> Result<Self, anyhow::Error> {
        debug!("XdpDispatcher::load() for if_index {if_index}, revision {revision}");
        let path = format!("{RTDIR_XDP_DISPATCHER}/{if_index}_{revision}");
        let file = fs::File::open(path)?;
        let reader = BufReader::new(file);
        let prog = serde_json::from_reader(reader)?;
        // TODO: We should check the bpffs paths here to for pinned links etc...
        Ok(prog)
    }

    pub(crate) fn delete(&self, full: bool) -> Result<(), BpfdError> {
        debug!(
            "XdpDispatcher::delete() for if_index {}, revision {}",
            self.if_index, self.revision
        );
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

    pub(crate) fn revision(&self) -> u32 {
        self.revision
    }

    pub(crate) fn num_extensions(&self) -> usize {
        self.num_extensions
    }
}
