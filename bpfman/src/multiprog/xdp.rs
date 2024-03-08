// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{fs, path::PathBuf};

use aya::{
    programs::{
        links::{FdLink, PinnedLink},
        Extension, Xdp,
    },
    Bpf, BpfLoader,
};
use log::debug;
use sled::Db;

use crate::{
    calc_map_pin_path,
    config::XdpMode,
    create_map_pin_path,
    directories::*,
    dispatcher_config::XdpDispatcherConfig,
    errors::BpfmanError,
    multiprog::{Dispatcher, XDP_DISPATCHER_PREFIX},
    oci_utils::image_manager::{BytecodeImage, ImageManager},
    types::{ImagePullPolicy, Program, XdpProgram},
    utils::{
        bytes_to_string, bytes_to_u32, bytes_to_usize, should_map_be_pinned, sled_get, sled_insert,
    },
};

pub(crate) const DEFAULT_PRIORITY: u32 = 50;

/// These constants define the key of SLED DB
const REVISION: &str = "revision";
const IF_INDEX: &str = "if_index";
const IF_NAME: &str = "if_name";
const MODE: &str = "mode";
const NUM_EXTENSIONS: &str = "num_extension";
const PROGRAM_NAME: &str = "program_name";

#[derive(Debug)]
pub struct XdpDispatcher {
    db_tree: sled::Tree,
    loader: Option<Bpf>,
}

impl XdpDispatcher {
    pub(crate) fn new(
        root_db: &Db,
        mode: XdpMode,
        if_index: u32,
        if_name: String,
        revision: u32,
    ) -> Result<Self, BpfmanError> {
        let db_tree = root_db
            .open_tree(format!(
                "{}_{}_{}",
                XDP_DISPATCHER_PREFIX, if_index, revision
            ))
            .expect("Unable to open xdp dispatcher database tree");

        let mut dp = Self {
            db_tree,
            loader: None,
        };

        dp.set_ifindex(if_index)?;
        dp.set_ifname(&if_name)?;
        dp.set_mode(mode)?;
        dp.set_revision(revision)?;
        Ok(dp)
    }

    // TODO(astoycos) check to ensure the expected fs pins are there.
    pub(crate) fn new_from_db(db_tree: sled::Tree) -> Self {
        Self {
            db_tree,
            loader: None,
        }
    }

    pub(crate) async fn load(
        &mut self,
        root_db: &Db,
        programs: &mut [Program],
        old_dispatcher: Option<Dispatcher>,
        image_manager: &mut ImageManager,
    ) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
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

        let (path, bpf_function_name) = image_manager
            .get_image(
                root_db,
                &image.image_url.clone(),
                image.image_pull_policy.clone(),
                image.username.clone(),
                image.password.clone(),
            )
            .await?;

        let program_bytes = image_manager.get_bytecode_from_image_store(root_db, path)?;

        let mut loader = BpfLoader::new()
            .set_global("conf", &config, true)
            .load(&program_bytes)?;

        let dispatcher: &mut Xdp = loader.program_mut(&bpf_function_name).unwrap().try_into()?;

        dispatcher.load()?;

        let path = format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        self.loader = Some(loader);
        self.set_num_extensions(extensions.len())?;
        self.set_program_name(&bpf_function_name)?;

        self.attach_extensions(&mut extensions)?;
        self.attach()?;
        if let Some(mut old) = old_dispatcher {
            old.delete(root_db, false)?;
        }
        Ok(())
    }

    pub(crate) fn attach(&mut self) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let mode = self.get_mode()?;
        let program_name = self.get_program_name()?;

        debug!(
            "XdpDispatcher::attach() for if_index {}, revision {}",
            if_index, revision
        );
        let iface = self.get_ifname()?;
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfmanError::NotLoaded)?
            .program_mut(program_name.as_str())
            .unwrap()
            .try_into()?;

        let path = PathBuf::from(format!("{RTDIR_FS_XDP}/dispatcher_{if_index}_link"));
        if path.exists() {
            let pinned_link: FdLink = PinnedLink::from_pin(path).unwrap().into();
            dispatcher
                .attach_to_link(pinned_link.try_into().unwrap())
                .unwrap();
        } else {
            let flags = mode.as_flags();
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

    fn attach_extensions(&mut self, extensions: &mut [&mut XdpProgram]) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let program_name = self.get_program_name()?;
        debug!(
            "XdpDispatcher::attach_extensions() for if_index {}, revision {}",
            if_index, revision
        );
        let dispatcher: &mut Xdp = self
            .loader
            .as_mut()
            .ok_or(BpfmanError::NotLoaded)?
            .program_mut(program_name.as_str())
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
                    revision
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
                    .load(&v.get_data().get_program_bytes()?)
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
                        revision,
                    ))
                    .map_err(BpfmanError::UnableToPinLink)?;

                // If this program is the map(s) owner pin all maps (except for .rodata and .bss) by name.
                if v.get_data().get_map_pin_path()?.is_none() {
                    let map_pin_path = calc_map_pin_path(id);
                    v.get_data_mut().set_map_pin_path(&map_pin_path)?;
                    create_map_pin_path(&map_pin_path)?;

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

    pub(crate) fn delete(&self, root_db: &Db, full: bool) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        debug!(
            "XdpDispatcher::delete() for if_index {}, revision {}, full {}",
            if_index, revision, full
        );
        root_db.drop_tree(self.db_tree.name()).map_err(|e| {
            BpfmanError::DatabaseError(
                format!(
                    "unable to drop xdp dispatcher tree {:?}",
                    self.db_tree.name()
                ),
                e.to_string(),
            )
        })?;

        let path = format!("{RTDIR_FS_XDP}/dispatcher_{}_{}", if_index, revision);
        fs::remove_dir_all(path)
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;
        if full {
            let path_link = format!("{RTDIR_FS_XDP}/dispatcher_{}_link", if_index);
            fs::remove_file(path_link)
                .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;
        }
        Ok(())
    }

    pub(crate) fn set_revision(&mut self, revision: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, REVISION, &revision.to_ne_bytes())
    }

    pub(crate) fn get_revision(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, REVISION).map(bytes_to_u32)
    }

    pub(crate) fn set_ifindex(&mut self, if_index: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, IF_INDEX, &if_index.to_ne_bytes())
    }

    pub(crate) fn get_ifindex(&self) -> Result<u32, BpfmanError> {
        sled_get(&self.db_tree, IF_INDEX).map(bytes_to_u32)
    }

    pub(crate) fn set_ifname(&mut self, if_name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, IF_NAME, if_name.as_bytes())
    }

    pub(crate) fn get_ifname(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, IF_NAME).map(|v| bytes_to_string(&v))
    }

    pub(crate) fn set_mode(&mut self, mode: XdpMode) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, MODE, &(mode as u32).to_ne_bytes())
    }

    pub(crate) fn get_mode(&self) -> Result<XdpMode, BpfmanError> {
        sled_get(&self.db_tree, MODE).map(|v| {
            XdpMode::try_from(bytes_to_u32(v)).map_err(|e| BpfmanError::Error(e.to_string()))
        })?
    }

    pub(crate) fn set_num_extensions(&mut self, num_extensions: usize) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, NUM_EXTENSIONS, &num_extensions.to_ne_bytes())
    }

    pub(crate) fn get_num_extensions(&self) -> Result<usize, BpfmanError> {
        sled_get(&self.db_tree, NUM_EXTENSIONS).map(bytes_to_usize)
    }

    pub(crate) fn set_program_name(&mut self, program_name: &str) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, PROGRAM_NAME, program_name.as_bytes())
    }

    pub(crate) fn get_program_name(&self) -> Result<String, BpfmanError> {
        sled_get(&self.db_tree, PROGRAM_NAME).map(|v| bytes_to_string(&v))
    }
}
