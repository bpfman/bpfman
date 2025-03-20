// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    fs,
    path::{Path, PathBuf},
};

use aya::{
    Ebpf, EbpfLoader,
    programs::{
        Extension, Xdp,
        links::{FdLink, PinnedLink},
    },
};
use aya_obj::programs::XdpAttachType;
use log::{debug, info};
use sled::Db;

use crate::{
    config::{RegistryConfig, XdpMode},
    directories::*,
    dispatcher_config::XdpDispatcherConfig,
    errors::BpfmanError,
    multiprog::Dispatcher,
    oci_utils::image_manager::ImageManager,
    types::{BytecodeImage, ImagePullPolicy, Link, XdpLink},
    utils::{
        bytes_to_string, bytes_to_u32, bytes_to_u64, bytes_to_usize, enter_netns, nsid, sled_get,
        sled_insert, xdp_dispatcher_db_tree_name, xdp_dispatcher_link_id_path,
        xdp_dispatcher_link_path, xdp_dispatcher_rev_path,
    },
};

pub(crate) const DEFAULT_PRIORITY: u32 = 50;
const XDP_DISPATCHER_PROGRAM_NAME: &str = "xdp_dispatcher";

/// These constants define the key of SLED DB
const REVISION: &str = "revision";
const IF_INDEX: &str = "if_index";
const IF_NAME: &str = "if_name";
const MODE: &str = "mode";
const NUM_EXTENSIONS: &str = "num_extension";
const PROGRAM_NAME: &str = "program_name";
const NSID: &str = "nsid";

#[derive(Debug)]
pub struct XdpDispatcher {
    db_tree: sled::Tree,
    loader: Option<Ebpf>,
}

impl XdpDispatcher {
    pub(crate) fn get_test(
        root_db: &Db,
        config: &RegistryConfig,
        image_manager: &mut ImageManager,
    ) -> Result<Xdp, BpfmanError> {
        if Path::new(RTDIR_FS_TEST_XDP_DISPATCHER).exists() {
            return Xdp::from_pin(RTDIR_FS_TEST_XDP_DISPATCHER, XdpAttachType::Interface)
                .map_err(BpfmanError::BpfProgramError);
        }

        let image = BytecodeImage::new(
            config.xdp_dispatcher_image.to_string(),
            ImagePullPolicy::IfNotPresent as i32,
            None,
            None,
        );

        let (path, bpf_program_names) = image_manager.get_image(
            root_db,
            &image.image_url,
            image.image_pull_policy.clone(),
            image.username.clone(),
            image.password.clone(),
        )?;

        if !bpf_program_names.contains(&XDP_DISPATCHER_PROGRAM_NAME.to_string()) {
            return Err(BpfmanError::ProgramNotFoundInBytecode {
                bytecode_image: image.image_url,
                expected_prog_name: XDP_DISPATCHER_PROGRAM_NAME.to_string(),
                program_names: bpf_program_names,
            });
        }

        let program_bytes = image_manager.get_bytecode_from_image_store(root_db, path)?;

        let xdp_config = XdpDispatcherConfig::new(11, 0, [0; 10], [DEFAULT_PRIORITY; 10], [0; 10]);
        let mut loader = EbpfLoader::new()
            .set_global("conf", &xdp_config, true)
            .load(&program_bytes)
            .map_err(|e| BpfmanError::DispatcherLoadError(format!("{e}")))?;

        if let Some(program) = loader.program_mut(XDP_DISPATCHER_PROGRAM_NAME) {
            let dispatcher: &mut Xdp = program.try_into()?;
            dispatcher.load()?;
            dispatcher
                .pin(RTDIR_FS_TEST_XDP_DISPATCHER)
                .map_err(BpfmanError::UnableToPinProgram)?;
            Xdp::from_pin(RTDIR_FS_TEST_XDP_DISPATCHER, XdpAttachType::Interface)
                .map_err(BpfmanError::BpfProgramError)
        } else {
            Err(BpfmanError::DispatcherLoadError(
                "invalid BPF function name".to_string(),
            ))
        }
    }

    pub(crate) fn new(
        root_db: &Db,
        mode: &XdpMode,
        if_index: u32,
        if_name: String,
        nsid: u64,
        revision: u32,
    ) -> Result<Self, BpfmanError> {
        let tree_name = xdp_dispatcher_db_tree_name(nsid, if_index, revision)?;
        info!("XdpDispatcher::new(): tree_path: {}", tree_name);
        let db_tree = root_db
            .open_tree(tree_name)
            .expect("Unable to open xdp dispatcher database tree");

        let mut dp = Self {
            db_tree,
            loader: None,
        };

        dp.set_ifindex(if_index)?;
        dp.set_ifname(&if_name)?;
        dp.set_mode(mode)?;
        dp.set_revision(revision)?;
        dp.set_nsid(nsid)?;
        Ok(dp)
    }

    // TODO(astoycos) check to ensure the expected fs pins are there.
    pub(crate) fn new_from_db(db_tree: sled::Tree) -> Self {
        Self {
            db_tree,
            loader: None,
        }
    }

    pub(crate) fn load(
        &mut self,
        root_db: &Db,
        links: &mut [Link],
        old_dispatcher: Option<Dispatcher>,
        image_manager: &mut ImageManager,
        config: &RegistryConfig,
        netns: Option<PathBuf>,
    ) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        debug!("XdpDispatcher::new() for if_index {if_index}, revision {revision}");
        let mut extensions: Vec<XdpLink> = links
            .iter()
            .map(|v| match v {
                Link::Xdp(p) => p.clone(),
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

        let xdp_config = XdpDispatcherConfig::new(
            extensions.len() as u8,
            0x0,
            chain_call_actions,
            [DEFAULT_PRIORITY; 10],
            [0; 10],
        );

        debug!("xdp dispatcher config: {:?}", xdp_config);
        let image = BytecodeImage::new(
            config.xdp_dispatcher_image.to_string(),
            ImagePullPolicy::IfNotPresent as i32,
            None,
            None,
        );

        let (path, bpf_program_names) = image_manager.get_image(
            root_db,
            &image.image_url.clone(),
            image.image_pull_policy.clone(),
            image.username.clone(),
            image.password.clone(),
        )?;

        if !bpf_program_names.contains(&XDP_DISPATCHER_PROGRAM_NAME.to_string()) {
            return Err(BpfmanError::ProgramNotFoundInBytecode {
                bytecode_image: image.image_url,
                expected_prog_name: XDP_DISPATCHER_PROGRAM_NAME.to_string(),
                program_names: bpf_program_names,
            });
        }

        let program_bytes = image_manager.get_bytecode_from_image_store(root_db, path)?;

        let mut loader = EbpfLoader::new()
            .set_global("conf", &xdp_config, true)
            .load(&program_bytes)
            .map_err(|e| BpfmanError::DispatcherLoadError(format!("{e}")))?;

        if let Some(program) = loader.program_mut(XDP_DISPATCHER_PROGRAM_NAME) {
            let dispatcher: &mut Xdp = program.try_into()?;
            dispatcher.load()?;
        } else {
            return Err(BpfmanError::DispatcherLoadError(
                "invalid BPF function name".to_string(),
            ));
        }

        let path = xdp_dispatcher_rev_path(nsid(netns.clone())?, if_index, revision)?;
        fs::create_dir_all(path).unwrap();

        self.loader = Some(loader);
        self.set_num_extensions(extensions.len())?;
        self.set_program_name(XDP_DISPATCHER_PROGRAM_NAME)?;

        self.attach_extensions(&mut extensions)?;

        if let Some(netns) = netns {
            let _netns_guard = enter_netns(netns)?;
            self.attach()?;
        } else {
            self.attach()?;
        };

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
        let nsid = self.get_nsid()?;

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

        let path = PathBuf::from(xdp_dispatcher_link_path(nsid, if_index)?);
        if path.exists() {
            let pinned_link: FdLink = PinnedLink::from_pin(path).unwrap().into();
            dispatcher
                .attach_to_link(pinned_link.try_into().unwrap())
                .unwrap();
        } else {
            let mut flags = mode.as_flags();
            let mut link = dispatcher.attach(&iface, flags);
            if let Err(e) = link {
                if mode != XdpMode::Skb {
                    info!(
                        "Unable to attach on interface {} mode {}, falling back to Skb and retrying.",
                        iface, mode
                    );
                    flags = XdpMode::Skb.as_flags();
                    link = dispatcher.attach(&iface, flags);
                    if let Err(e) = link {
                        return Err(BpfmanError::Error(format!(
                            "dispatcher attach failed on interface {iface} mode {mode}: {e}"
                        )));
                    }
                } else {
                    return Err(BpfmanError::Error(format!(
                        "dispatcher attach failed on interface {iface} mode {mode}: {e}"
                    )));
                }
            }
            let owned_link = dispatcher.take_link(link.unwrap())?;
            let path = xdp_dispatcher_link_path(nsid, if_index)?;
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

    fn attach_extensions(&mut self, extensions: &mut [XdpLink]) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let program_name = self.get_program_name()?;
        let nsid = self.get_nsid()?;
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
            let id = v.0.get_program_id()?;
            let mut ext = Extension::from_pin(format!("{RTDIR_FS}/prog_{id}"))?;
            let target_fn = format!("prog{i}");
            let new_link_id = ext
                .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                .unwrap();
            let new_link: FdLink = ext.take_link(new_link_id)?.into();
            let path = xdp_dispatcher_link_id_path(nsid, if_index, revision, i as u32)?;
            new_link.pin(path).map_err(BpfmanError::UnableToPinLink)?;
        }
        Ok(())
    }

    pub(crate) fn delete(&self, root_db: &Db, full: bool) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let nsid = self.get_nsid()?;
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

        let path = xdp_dispatcher_rev_path(nsid, if_index, revision)?;
        fs::remove_dir_all(path)
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;
        if full {
            let path_link = xdp_dispatcher_link_path(nsid, if_index)?;
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

    pub(crate) fn set_mode(&mut self, mode: &XdpMode) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, MODE, &(*mode as u32).to_ne_bytes())
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

    pub(crate) fn set_nsid(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, NSID, &offset.to_ne_bytes())
    }

    pub fn get_nsid(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.db_tree, NSID).map(bytes_to_u64)
    }
}
