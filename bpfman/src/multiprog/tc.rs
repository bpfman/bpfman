// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{fs, mem, path::PathBuf};

use aya::{
    programs::{
        links::FdLink,
        tc::{self, NlOptions, SchedClassifierLink, TcAttachOptions},
        Extension, Link, SchedClassifier, TcAttachType,
    },
    Ebpf, EbpfLoader,
};
use futures::stream::TryStreamExt;
use log::debug;
use netlink_packet_route::tc::TcAttribute;
use sled::Db;

use crate::{
    calc_map_pin_path,
    config::RegistryConfig,
    create_map_pin_path,
    directories::*,
    dispatcher_config::TcDispatcherConfig,
    errors::BpfmanError,
    multiprog::Dispatcher,
    oci_utils::image_manager::ImageManager,
    types::{
        BytecodeImage,
        Direction::{self},
        ImagePullPolicy, Program, TcProgram,
    },
    utils::{
        bytes_to_string, bytes_to_u16, bytes_to_u32, bytes_to_u64, bytes_to_usize, enter_netns,
        nsid, should_map_be_pinned, sled_get, sled_get_option, sled_insert,
        tc_dispatcher_db_tree_name, tc_dispatcher_link_id_path, tc_dispatcher_rev_path,
    },
};

const DEFAULT_PRIORITY: u32 = 50; // Default priority for user programs in the dispatcher
const TC_DISPATCHER_PRIORITY: u16 = 50; // Default TC priority for TC Dispatcher
const TC_DISPATCHER_PROGRAM_NAME: &str = "tc_dispatcher";

/// These constants define the key of SLED DB
const REVISION: &str = "revision";
const IF_INDEX: &str = "if_index";
const IF_NAME: &str = "if_name";
const PRIORITY: &str = "priority";
const DIRECTION: &str = "direction";
const NUM_EXTENSIONS: &str = "num_extension";
const PROGRAM_NAME: &str = "program_name";
const HANDLE: &str = "handle";
const NSID: &str = "nsid";

#[derive(Debug)]
pub struct TcDispatcher {
    db_tree: sled::Tree,
    loader: Option<Ebpf>,
}

impl TcDispatcher {
    pub(crate) fn new(
        root_db: &Db,
        direction: Direction,
        if_index: u32,
        if_name: String,
        nsid: u64,
        revision: u32,
    ) -> Result<Self, BpfmanError> {
        let db_tree = root_db
            .open_tree(tc_dispatcher_db_tree_name(
                nsid, if_index, direction, revision,
            )?)
            .expect("Unable to open tc dispatcher database tree");

        let mut dp = Self {
            db_tree,
            loader: None,
        };

        dp.set_ifindex(if_index)?;
        dp.set_ifname(&if_name)?;
        dp.set_direction(direction)?;
        dp.set_revision(revision)?;
        dp.set_priority(TC_DISPATCHER_PRIORITY)?;
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

    pub(crate) async fn load(
        &mut self,
        root_db: &Db,
        programs: &mut [Program],
        old_dispatcher: Option<Dispatcher>,
        image_manager: &mut ImageManager,
        config: &RegistryConfig,
        netns: Option<PathBuf>,
    ) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let direction = self.get_direction()?;

        debug!("TcDispatcher::new() for if_index {if_index}, revision {revision}");
        let mut extensions: Vec<&mut TcProgram> = programs
            .iter_mut()
            .map(|v| match v {
                Program::Tc(p) => p,
                _ => panic!("All programs should be of type TC"),
            })
            .collect();
        let mut chain_call_actions = [0; 10];
        for v in extensions.iter() {
            chain_call_actions[v.get_current_position()?.unwrap()] = v.get_proceed_on()?.mask()
        }

        let tc_config = TcDispatcherConfig {
            num_progs_enabled: extensions.len() as u8,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        debug!("tc dispatcher config: {:?}", tc_config);
        let image = BytecodeImage::new(
            config.tc_dispatcher_image.to_string(),
            ImagePullPolicy::IfNotPresent as i32,
            None,
            None,
        );

        let (path, bpf_program_names) = image_manager
            .get_image(
                root_db,
                &image.image_url,
                image.image_pull_policy.clone(),
                image.username.clone(),
                image.password.clone(),
            )
            .await?;

        if !bpf_program_names.contains(&TC_DISPATCHER_PROGRAM_NAME.to_string()) {
            return Err(BpfmanError::ProgramNotFoundInBytecode {
                bytecode_image: image.image_url,
                expected_prog_name: TC_DISPATCHER_PROGRAM_NAME.to_string(),
                program_names: bpf_program_names,
            });
        }

        let program_bytes = image_manager.get_bytecode_from_image_store(root_db, path)?;

        let mut loader = EbpfLoader::new()
            .set_global("CONFIG", &tc_config, true)
            .load(&program_bytes)
            .map_err(|e| BpfmanError::DispatcherLoadError(format!("{e}")))?;

        if let Some(program) = loader.program_mut(TC_DISPATCHER_PROGRAM_NAME) {
            let dispatcher: &mut SchedClassifier = program.try_into()?;
            dispatcher.load()?;
        } else {
            return Err(BpfmanError::DispatcherLoadError(
                "invalid BPF function name".to_string(),
            ));
        }

        let path = tc_dispatcher_rev_path(direction, nsid(netns.clone())?, if_index, revision)?;
        fs::create_dir_all(path).unwrap();

        self.loader = Some(loader);
        self.set_num_extensions(extensions.len())?;
        self.set_program_name(TC_DISPATCHER_PROGRAM_NAME)?;

        self.attach_extensions(&mut extensions)?;

        if let Some(netns) = netns {
            let _netns_guard = enter_netns(netns)?;
            self.attach(root_db, old_dispatcher).await?;
        } else {
            self.attach(root_db, old_dispatcher).await?;
        };

        Ok(())
    }

    /// has_qdisc returns true if the qdisc_name is found on the if_index.
    async fn has_qdisc(qdisc_name: String, if_index: i32) -> Result<bool, anyhow::Error> {
        let (connection, handle, _) = rtnetlink::new_connection().unwrap();
        tokio::spawn(connection);

        let mut qdiscs = handle.qdisc().get().execute();
        while let Some(qdisc_message) = qdiscs.try_next().await? {
            if qdisc_message.header.index == if_index
                && qdisc_message
                    .attributes
                    .contains(&TcAttribute::Kind(qdisc_name.clone()))
            {
                return Ok(true);
            }
        }

        Ok(false)
    }

    async fn attach(
        &mut self,
        root_db: &Db,
        old_dispatcher: Option<Dispatcher>,
    ) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let iface = self.get_ifname()?;
        let priority = self.get_priority()?;
        let revision = self.get_revision()?;
        let direction = self.get_direction()?;
        let program_name = self.get_program_name()?;

        debug!(
            "TcDispatcher::attach() for if_index {}, revision {}",
            if_index, revision
        );

        // Aya returns an error when trying to add a qdisc that already exists, which could be ingress or clsact. We
        // need to make sure that the qdisc installed is the one that we want, i.e. clsact. If the qdisc is an ingress
        // qdisc, we return an error. If the qdisc is a clsact qdisc, we do nothing. Otherwise, we add a clsact qdisc.

        // no need to add a new clsact qdisc if one already exists.
        if TcDispatcher::has_qdisc("clsact".to_string(), if_index as i32).await? {
            debug!(
                "clsact qdisc found for if_index {}, no need to add a new clsact qdisc",
                if_index
            );

        // if ingress qdisc exists, return error.
        } else if TcDispatcher::has_qdisc("ingress".to_string(), if_index as i32).await? {
            debug!("ingress qdisc found for if_index {}", if_index);
            return Err(BpfmanError::InvalidAttach(format!(
                "Ingress qdisc found for if_index {}",
                if_index
            )));

        // otherwise, add a new clsact qdisc.
        } else {
            debug!("No qdisc found for if_index {}, adding clsact", if_index);
            let _ = tc::qdisc_add_clsact(&iface);
        }

        let new_dispatcher: &mut SchedClassifier = self
            .loader
            .as_mut()
            .ok_or(BpfmanError::NotLoaded)?
            .program_mut(program_name.as_str())
            .unwrap()
            .try_into()?;

        let attach_type = match direction {
            Direction::Ingress => TcAttachType::Ingress,
            Direction::Egress => TcAttachType::Egress,
        };

        let link_id = new_dispatcher.attach_with_options(
            &iface,
            attach_type,
            TcAttachOptions::Netlink(NlOptions {
                priority,
                ..Default::default()
            }),
        )?;

        let link = new_dispatcher.take_link(link_id)?;
        self.set_handle(link.handle()?)?;
        mem::forget(link);

        if let Some(Dispatcher::Tc(mut d)) = old_dispatcher {
            // If the old dispatcher was not attached when the new dispatcher
            // was attached above, the new dispatcher may get the same handle
            // as the old one had.  If this happens, the new dispatcher will get
            // detached if we do a full delete, so don't do it.
            if d.get_handle()? != self.get_handle()? {
                d.delete(root_db, true)?;
            } else {
                d.delete(root_db, false)?;
            }
        }

        Ok(())
    }

    fn attach_extensions(&mut self, extensions: &mut [&mut TcProgram]) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let direction = self.get_direction()?;
        let program_name = self.get_program_name()?;
        let nsid = self.get_nsid()?;

        debug!(
            "TcDispatcher::attach_extensions() for if_index {}, revision {}",
            if_index, revision
        );
        let dispatcher: &mut SchedClassifier = self
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
                let id = v.data.get_id()?;
                debug!("program {id} was already attached loading from pin");
                let mut ext = Extension::from_pin(format!("{RTDIR_FS}/prog_{id}"))?;
                let target_fn = format!("prog{i}");
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link: FdLink = ext.take_link(new_link_id)?.into();
                let path = tc_dispatcher_link_id_path(direction, nsid, if_index, revision, id)?;
                new_link.pin(path).map_err(BpfmanError::UnableToPinLink)?;
            } else {
                let name = &v.data.get_name()?;
                let global_data = &v.data.get_global_data()?;

                let mut bpf = EbpfLoader::new();

                bpf.allow_unsupported_maps().extension(name);

                for (name, value) in global_data {
                    bpf.set_global(name, value.as_slice(), true);
                }

                // If map_pin_path is set already it means we need to use a pin
                // path which should already exist on the system.
                if let Some(map_pin_path) = v.data.get_map_pin_path()? {
                    debug!("tc program {name} is using maps from {:?}", map_pin_path);
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
                v.data.set_kernel_info(&ext.info()?)?;

                let id = v.get_data().get_id()?;

                ext.pin(format!("{RTDIR_FS}/prog_{id}"))
                    .map_err(BpfmanError::UnableToPinProgram)?;
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let fd_link: FdLink = new_link.into();
                let path = tc_dispatcher_link_id_path(direction, nsid, if_index, revision, id)?;
                fd_link.pin(path).map_err(BpfmanError::UnableToPinLink)?;

                // If this program is the map(s) owner pin all maps (except for .rodata and .bss) by name.
                if v.data.get_map_pin_path()?.is_none() {
                    let map_pin_path = calc_map_pin_path(id);
                    v.data.set_map_pin_path(&map_pin_path.clone())?;
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

    pub(crate) fn delete(&mut self, root_db: &Db, full: bool) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let if_name = self.get_ifname()?;
        let revision = self.get_revision()?;
        let direction = self.get_direction()?;
        let handle = self.get_handle()?;
        let priority = self.get_priority()?;
        let nsid = self.get_nsid()?;

        debug!(
            "TcDispatcher::delete() for if_index {}, revision {}",
            if_index, revision
        );

        root_db.drop_tree(self.db_tree.name()).map_err(|e| {
            BpfmanError::DatabaseError(
                format!(
                    "unable to drop tc dispatcher tree {:?}",
                    self.db_tree.name()
                ),
                e.to_string(),
            )
        })?;

        let path = tc_dispatcher_rev_path(direction, nsid, if_index, revision)?;
        fs::remove_dir_all(path)
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;

        if full {
            // Also detach the old dispatcher.
            if let Some(old_handle) = handle {
                let attach_type = match direction {
                    Direction::Ingress => TcAttachType::Ingress,
                    Direction::Egress => TcAttachType::Egress,
                };
                if let Ok(old_link) =
                    SchedClassifierLink::attached(&if_name, attach_type, priority, old_handle)
                {
                    let detach_result = old_link.detach();
                    match detach_result {
                        Ok(_) => debug!(
                            "TC dispatcher {}, {}, {}, {} successfully detached",
                            if_name, direction, priority, old_handle
                        ),
                        Err(_) => debug!(
                            "TC dispatcher {}, {}, {}, {} not attached when detach attempted",
                            if_name, direction, priority, old_handle
                        ),
                    }
                }
            };
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

    pub(crate) fn set_priority(&mut self, priority: u16) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, PRIORITY, &priority.to_ne_bytes())
    }

    pub(crate) fn get_priority(&self) -> Result<u16, BpfmanError> {
        sled_get(&self.db_tree, PRIORITY).map(bytes_to_u16)
    }

    pub(crate) fn set_direction(&mut self, direction: Direction) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, DIRECTION, &(direction as u32).to_ne_bytes())
    }

    pub(crate) fn get_direction(&self) -> Result<Direction, BpfmanError> {
        sled_get(&self.db_tree, DIRECTION).map(|v| {
            Direction::try_from(bytes_to_u32(v)).map_err(|e| BpfmanError::Error(e.to_string()))
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

    pub(crate) fn set_handle(&mut self, handle: u32) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, HANDLE, &handle.to_ne_bytes())
    }

    pub(crate) fn get_handle(&self) -> Result<Option<u32>, BpfmanError> {
        sled_get_option(&self.db_tree, HANDLE).map(|v| v.map(bytes_to_u32))
    }

    pub(crate) fn set_nsid(&mut self, offset: u64) -> Result<(), BpfmanError> {
        sled_insert(&self.db_tree, NSID, &offset.to_ne_bytes())
    }

    pub fn get_nsid(&self) -> Result<u64, BpfmanError> {
        sled_get(&self.db_tree, NSID).map(bytes_to_u64)
    }
}
