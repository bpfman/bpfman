// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{fs, io::BufReader, mem};

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
use netlink_packet_route::tc::Nla;
use serde::{Deserialize, Serialize};
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
    utils::should_map_be_pinned,
};

const DEFAULT_PRIORITY: u32 = 50; // Default priority for user programs in the dispatcher
const TC_DISPATCHER_PRIORITY: u16 = 50; // Default TC priority for TC Dispatcher

#[derive(Debug, Serialize, Deserialize)]
pub struct TcDispatcher {
    pub(crate) revision: u32,
    if_index: u32,
    if_name: String,
    direction: Direction,
    priority: u16,
    handle: Option<u32>,
    num_extensions: usize,
    #[serde(skip)]
    loader: Option<Bpf>,
    program_name: Option<String>,
}

impl TcDispatcher {
    pub(crate) async fn new(
        direction: Direction,
        if_index: &u32,
        if_name: String,
        programs: &mut [&mut Program],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
        image_manager: Sender<ImageManagerCommand>,
    ) -> Result<TcDispatcher, BpfmanError> {
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

        let mut dispatcher = TcDispatcher {
            revision,
            if_index: *if_index,
            if_name,
            direction,
            num_extensions: extensions.len(),
            priority: TC_DISPATCHER_PRIORITY,
            handle: None,
            loader: Some(loader),
            program_name: Some(bpf_function_name),
        };
        dispatcher.attach_extensions(&mut extensions).await?;
        dispatcher.attach(old_dispatcher).await?;
        dispatcher.save()?;
        Ok(dispatcher)
    }

    /// has_qdisc returns true if the qdisc_name is found on the if_index.
    async fn has_qdisc(qdisc_name: String, if_index: i32) -> Result<bool, anyhow::Error> {
        let (connection, handle, _) = rtnetlink::new_connection().unwrap();
        tokio::spawn(connection);

        let mut qdiscs = handle.qdisc().get().execute();
        while let Some(qdisc_message) = qdiscs.try_next().await? {
            if qdisc_message.header.index == if_index
                && qdisc_message.nlas.contains(&Nla::Kind(qdisc_name.clone()))
            {
                return Ok(true);
            }
        }
        Ok(false)
    }

    async fn attach(&mut self, old_dispatcher: Option<Dispatcher>) -> Result<(), BpfmanError> {
        debug!(
            "TcDispatcher::attach() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let iface = self.if_name.clone();

        // Aya returns an error when trying to add a qdisc that already exists, which could be ingress or clsact. We
        // need to make sure that the qdisc installed is the one that we want, i.e. clsact. If the qdisc is an ingress
        // qdisc, we return an error. If the qdisc is a clsact qdisc, we do nothing. Otherwise, we add a clsact qdisc.

        // no need to add a new clsact qdisc if one already exists.
        if TcDispatcher::has_qdisc("clsact".to_string(), self.if_index as i32).await? {
            debug!(
                "clsact qdisc found for if_index {}, no need to add a new clsact qdisc",
                self.if_index
            );

        // if ingress qdisc exists, return error.
        } else if TcDispatcher::has_qdisc("ingress".to_string(), self.if_index as i32).await? {
            debug!("ingress qdisc found for if_index {}", self.if_index);
            return Err(BpfmanError::InvalidAttach(format!(
                "Ingress qdisc found for if_index {}",
                self.if_index
            )));

        // otherwise, add a new clsact qdisc.
        } else {
            debug!(
                "No qdisc found for if_index {}, adding clsact",
                self.if_index
            );
            let _ = tc::qdisc_add_clsact(&iface);
        }

        let new_dispatcher: &mut SchedClassifier = self
            .loader
            .as_mut()
            .ok_or(BpfmanError::NotLoaded)?
            .program_mut(self.program_name.clone().unwrap().as_str())
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
    ) -> Result<(), BpfmanError> {
        debug!(
            "TcDispatcher::attach_extensions() for if_index {}, revision {}",
            self.if_index, self.revision
        );
        let if_index = self.if_index;
        let dispatcher: &mut SchedClassifier = self
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
                let id = v.data.get_id()?;
                debug!("program {id} was already attached loading from pin");
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
                let base = match self.direction {
                    Direction::Ingress => RTDIR_FS_TC_INGRESS,
                    Direction::Egress => RTDIR_FS_TC_EGRESS,
                };
                fd_link
                    .pin(format!(
                        "{base}/dispatcher_{if_index}_{}/link_{id}",
                        self.revision,
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

    fn save(&self) -> Result<(), BpfmanError> {
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
            .map_err(|e| BpfmanError::Error(format!("can't save state: {e}")))?;
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

    pub(crate) fn delete(&mut self, full: bool) -> Result<(), BpfmanError> {
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
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;

        let base = match self.direction {
            Direction::Ingress => RTDIR_FS_TC_INGRESS,
            Direction::Egress => RTDIR_FS_TC_EGRESS,
        };
        let path = format!("{base}/dispatcher_{}_{}", self.if_index, self.revision);
        fs::remove_dir_all(path)
            .map_err(|e| BpfmanError::Error(format!("unable to cleanup state: {e}")))?;

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

    pub(crate) fn revision(&self) -> u32 {
        self.revision
    }

    pub(crate) fn num_extensions(&self) -> usize {
        self.num_extensions
    }
}
