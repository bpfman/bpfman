// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{fs, io::BufReader, path::PathBuf};

use aya::{
    programs::{
        links::{FdLink, PinnedLink},
        Extension, Xdp,
    },
    Bpf, BpfLoader,
};
use bpfman_api::{config::XdpMode, util::directories::*, ImagePullPolicy};
use log::debug;
use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc::Sender, oneshot};

use crate::{
    bpf::{calc_map_pin_path, create_map_pin_path},
    command::{Program, XdpProgram},
    dispatcher_config::XdpDispatcherConfig,
    errors::BpfmanError,
    multiprog::Dispatcher,
    oci_utils::image_manager::{BytecodeImage, Command as ImageManagerCommand},
    utils::should_map_be_pinned,
};

pub(crate) const DEFAULT_PRIORITY: u32 = 50;

#[derive(Debug, Serialize, Deserialize)]
pub struct XdpDispatcher {
    revision: u32,
    if_index: u32,
    if_name: String,
    mode: XdpMode,
    num_extensions: usize,
    program_name: Option<String>,

    #[serde(skip)]
    loader: Option<Bpf>,
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
    ) -> Result<XdpDispatcher, BpfmanError> {
        debug!("XdpDispatcher::new() for if_index {if_index}, revision {revision}");
        let mut extensions: Vec<&mut XdpProgram> = programs
            .iter_mut()
            .map(|v| match v {
                Program::Xdp(p) => p,
                _ => panic!("All programs should be of type XDP"),
            })
            .collect();

        let mut chain_call_actions = [0; 10];
        extensions.sort_by(|a, b| {
            a.get_current_position()
                .unwrap()
                .cmp(&b.get_current_position().unwrap())
        });
        for p in extensions.iter() {
            chain_call_actions[p.get_current_position()?.unwrap()] = p.get_proceed_on()?.mask();
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
            "quay.io/bpfman/xdp-dispatcher:v2".to_string(),
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
            .map_err(|e| BpfmanError::RpcSendError(e.into()))?;

        let (path, bpf_function_name) = rx
            .await
            .map_err(BpfmanError::RpcRecvError)?
            .map_err(BpfmanError::BpfBytecodeError)?;

        let (tx, rx) = oneshot::channel();
        image_manager
            .send(ImageManagerCommand::GetBytecode { path, resp: tx })
            .await
            .map_err(|e| BpfmanError::RpcSendError(e.into()))?;
        let program_bytes = rx
            .await
            .map_err(BpfmanError::RpcRecvError)?
            .map_err(BpfmanError::BpfBytecodeError)?;
        let mut loader = BpfLoader::new()
            .set_global("conf", &config, true)
            .load(&program_bytes)?;

        let dispatcher: &mut Xdp = loader.program_mut(&bpf_function_name).unwrap().try_into()?;

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
            program_name: Some(bpf_function_name),
        };
        dispatcher.attach_extensions(&mut extensions).await?;
        dispatcher.attach()?;
        dispatcher.save()?;
        if let Some(mut old) = old_dispatcher {
            old.delete(false)?;
        }
        Ok(dispatcher)
    }

    pub(crate) fn attach(&mut self) -> Result<(), BpfmanError> {
        debug!(
            "XdpDispatcher::attach() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let if_index = self.if_index;
        let iface = self.if_name.clone();
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfmanError::NotLoaded)?
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
            let link = dispatcher.attach(&iface, flags).map_err(|e| {
                BpfmanError::Error(format!(
                    "dispatcher attach failed on interface {iface}: {e}"
                ))
            })?;
            let owned_link = dispatcher.take_link(link)?;
            let path = format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_link");
            let _ = TryInto::<FdLink>::try_into(owned_link)
                .map_err(|e| {
                    BpfmanError::Error(format!(
                        "FdLink conversion failed on interface {iface}: {e}"
                    ))
                })?
                .pin(path)
                .map_err(BpfmanError::UnableToPinLink)?;
        }
        Ok(())
    }

    async fn attach_extensions(
        &mut self,
        extensions: &mut [&mut XdpProgram],
    ) -> Result<(), BpfmanError> {
        debug!(
            "XdpDispatcher::attach_extensions() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let if_index = self.if_index;
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfmanError::NotLoaded)?
            .program_mut(self.program_name.clone().unwrap().as_str())
            .unwrap()
            .try_into()?;
        extensions.sort_by(|a, b| {
            a.get_current_position()
                .unwrap()
                .cmp(&b.get_current_position().unwrap())
        });
        for (i, v) in extensions.iter_mut().enumerate() {
            if v.get_attached()? {
                let id = v.get_data().get_id()?;
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
                new_link.pin(path).map_err(BpfmanError::UnableToPinLink)?;
            } else {
                let name = &v.get_data().get_name()?;
                let global_data = &v.get_data().get_global_data()?;

                let mut bpf = BpfLoader::new();

                bpf.allow_unsupported_maps().extension(name);

                for (name, value) in global_data {
                    bpf.set_global(name, value.as_slice(), true);
                }

                // If map_pin_path is set already it means we need to use a pin
                // path which should already exist on the system.
                if let Some(map_pin_path) = v.get_data().get_map_pin_path()? {
                    debug!("xdp program {name} is using maps from {:?}", map_pin_path);
                    bpf.map_pin_path(map_pin_path);
                }

                let mut loader = bpf
                    .load(v.get_data().program_bytes())
                    .map_err(BpfmanError::BpfLoadError)?;

                let ext: &mut Extension = loader
                    .program_mut(name)
                    .ok_or_else(|| BpfmanError::BpfFunctionNameNotValid(name.to_string()))?
                    .try_into()?;

                let target_fn = format!("prog{i}");

                ext.load(dispatcher.fd()?.try_clone()?, &target_fn)?;
                v.get_data_mut().set_kernel_info(&ext.info()?)?;

                let id = v.get_data().get_id()?;

                ext.pin(format!("{RTDIR_FS}/prog_{id}"))
                    .map_err(BpfmanError::UnableToPinProgram)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                fd_link
                    .pin(format!(
                        "{RTDIR_FS_XDP}/dispatcher_{if_index}_{}/link_{id}",
                        self.revision,
                    ))
                    .map_err(BpfmanError::UnableToPinLink)?;

                // If this program is the map(s) owner pin all maps (except for .rodata and .bss) by name.
                if v.get_data().get_map_pin_path()?.is_none() {
                    let map_pin_path = calc_map_pin_path(id);
                    v.get_data_mut().set_map_pin_path(&map_pin_path)?;
                    create_map_pin_path(&map_pin_path).await?;

                    for (name, map) in loader.maps_mut() {
                        if !should_map_be_pinned(name) {
                            continue;
                        }
                        debug!(
                            "Pinning map: {name} to path: {}",
                            map_pin_path.join(name).display()
                        );
                        map.pin(map_pin_path.join(name))
                            .map_err(BpfmanError::UnableToPinMap)?;
                    }
                }
            }
        }
        Ok(())
    }

    fn save(&self) -> Result<(), BpfmanError> {
        debug!(
            "XdpDispatcher::save() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let path = format!("{RTDIR_XDP_DISPATCHER}/{}_{}", self.if_index, self.revision);
        serde_json::to_writer(&fs::File::create(path).unwrap(), &self)
            .map_err(|e| BpfmanError::Error(format!("can't save state: {e}")))?;
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

    pub(crate) fn delete(&self, full: bool) -> Result<(), BpfmanError> {
        debug!(
            "XdpDispatcher::delete() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let path = format!("{RTDIR_XDP_DISPATCHER}/{}_{}", self.if_index, self.revision);
        fs::remove_file(path)
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;

        let path = format!(
            "{RTDIR_FS_XDP}/dispatcher_{}_{}",
            self.if_index, self.revision
        );
        fs::remove_dir_all(path)
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;
        if full {
            let path_link = format!("{RTDIR_FS_XDP}/dispatcher_{}_link", self.if_index);
            fs::remove_file(path_link)
                .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;
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
