// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{fs, mem};

use aya::{
    programs::{
        links::FdLink,
        tc::{self, SchedClassifierLink, TcOptions},
        Extension, Link, SchedClassifier, TcAttachType,
    },
    Bpf, BpfLoader,
};
use bpfman_api::{util::directories::*, ImagePullPolicy};
use futures::stream::TryStreamExt;
use log::debug;
use netlink_packet_route::tc::TcAttribute;
use tokio::sync::{mpsc::Sender, oneshot};

use crate::{
    bpf::{calc_map_pin_path, create_map_pin_path},
    command::{
        Direction,
        Direction::{Egress, Ingress},
        Program, TcProgram,
    },
    dispatcher_config::TcDispatcherConfig,
    errors::BpfmanError,
    multiprog::Dispatcher,
    oci_utils::image_manager::{BytecodeImage, Command as ImageManagerCommand},
    utils::{
        bytes_to_string, bytes_to_u16, bytes_to_u32, bytes_to_usize, should_map_be_pinned,
        sled_get, sled_get_option, sled_insert,
    },
    ROOT_DB,
};

const DEFAULT_PRIORITY: u32 = 50; // Default priority for user programs in the dispatcher
const TC_DISPATCHER_PRIORITY: u16 = 50; // Default TC priority for TC Dispatcher

/// These constants define the key of SLED DB
const TC_DISPATCHER_PREFIX: &str = "tc_dispatcher_";
const REVISION: &str = "revision";
const IF_INDEX: &str = "if_index";
const IF_NAME: &str = "if_name";
const PRIORITY: &str = "priority";
const DIRECTION: &str = "direction";
const NUM_EXTENSIONS: &str = "num_extension";
const PROGRAM_NAME: &str = "program_name";
const HANDLE: &str = "handle";

#[derive(Debug)]
pub struct TcDispatcher {
    db_tree: sled::Tree,
    loader: Option<Bpf>,
}

impl TcDispatcher {
    pub(crate) fn new(
        direction: Direction,
        if_index: u32,
        if_name: String,
        revision: u32,
    ) -> Result<Self, BpfmanError> {
        let db_tree = ROOT_DB
            .open_tree(format!(
                "{}_{}_{}_{}",
                TC_DISPATCHER_PREFIX, if_index, direction, revision
            ))
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
        programs: &mut [&mut Program],
        old_dispatcher: Option<Dispatcher>,
        image_manager: Sender<ImageManagerCommand>,
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

        let config = TcDispatcherConfig {
            num_progs_enabled: extensions.len() as u8,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        debug!("tc dispatcher config: {:?}", config);
        let image = BytecodeImage::new(
            "quay.io/bpfman/tc-dispatcher:v1".to_string(),
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
        let program_bytes = rx.await?.map_err(BpfmanError::BpfBytecodeError)?;

        let mut loader = BpfLoader::new()
            .set_global("CONFIG", &config, true)
            .load(&program_bytes)?;

        let dispatcher: &mut SchedClassifier =
            loader.program_mut(&bpf_function_name).unwrap().try_into()?;

        dispatcher.load()?;

        let base = match direction {
            Ingress => RTDIR_FS_TC_INGRESS,
            Egress => RTDIR_FS_TC_EGRESS,
        };
        let path = format!("{base}/dispatcher_{if_index}_{revision}");
        fs::create_dir_all(path).unwrap();

        self.loader = Some(loader);
        self.set_num_extensions(extensions.len())?;
        self.set_program_name(&bpf_function_name)?;

        self.attach_extensions(&mut extensions).await?;
        self.attach(old_dispatcher).await?;
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

    async fn attach(&mut self, old_dispatcher: Option<Dispatcher>) -> Result<(), BpfmanError> {
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
            TcOptions {
                priority,
                ..Default::default()
            },
        )?;

        let link = new_dispatcher.take_link(link_id)?;
        self.set_handle(link.handle())?;
        mem::forget(link);

        if let Some(Dispatcher::Tc(mut d)) = old_dispatcher {
            // If the old dispatcher was not attached when the new dispatcher
            // was attached above, the new dispatcher may get the same handle
            // as the old one had.  If this happens, the new dispatcher will get
            // detached if we do a full delete, so don't do it.
            if d.get_handle()? != self.get_handle()? {
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
    ) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let revision = self.get_revision()?;
        let direction = self.get_direction()?;
        let program_name = self.get_program_name()?;

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
                let base = match direction {
                    Direction::Ingress => RTDIR_FS_TC_INGRESS,
                    Direction::Egress => RTDIR_FS_TC_EGRESS,
                };
                let path = format!("{base}/dispatcher_{if_index}_{}/link_{id}", revision);
                new_link.pin(path).map_err(BpfmanError::UnableToPinLink)?;
            } else {
                let name = &v.data.get_name()?;
                let global_data = &v.data.get_global_data()?;

                let mut bpf = BpfLoader::new();

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
                    .load(v.data.program_bytes())
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
                let base = match direction {
                    Direction::Ingress => RTDIR_FS_TC_INGRESS,
                    Direction::Egress => RTDIR_FS_TC_EGRESS,
                };
                fd_link
                    .pin(format!(
                        "{base}/dispatcher_{if_index}_{}/link_{id}",
                        revision,
                    ))
                    .map_err(BpfmanError::UnableToPinLink)?;

                // If this program is the map(s) owner pin all maps (except for .rodata and .bss) by name.
                if v.data.get_map_pin_path()?.is_none() {
                    let map_pin_path = calc_map_pin_path(id);
                    v.data.set_map_pin_path(&map_pin_path.clone())?;
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

    pub(crate) fn delete(&mut self, full: bool) -> Result<(), BpfmanError> {
        let if_index = self.get_ifindex()?;
        let if_name = self.get_ifname()?;
        let revision = self.get_revision()?;
        let direction = self.get_direction()?;
        let handle = self.get_handle()?;
        let priority = self.get_priority()?;

        debug!(
            "TcDispatcher::delete() for if_index {}, revision {}",
            if_index, revision
        );

        ROOT_DB.drop_tree(self.db_tree.name()).map_err(|e| {
            BpfmanError::DatabaseError(
                format!(
                    "unable to drop tc dispatcher tree {:?}",
                    self.db_tree.name()
                ),
                e.to_string(),
            )
        })?;

        let base = match direction {
            Direction::Ingress => RTDIR_FS_TC_INGRESS,
            Direction::Egress => RTDIR_FS_TC_EGRESS,
        };
        let path = format!("{base}/dispatcher_{}_{}", if_index, revision);
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
}
