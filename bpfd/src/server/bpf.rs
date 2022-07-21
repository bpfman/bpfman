// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, io::IoSlice, os::unix::prelude::AsRawFd, path::Path};

use aya::{
    programs::{extension::ExtensionLink, xdp::XdpLink, Extension, OwnedLink, Xdp},
    Bpf, BpfLoader,
};
use bpfd_common::*;
use log::info;
use nix::{
    net::if_::if_nametoindex,
    sys::socket::{
        sendmsg, socket, AddressFamily, ControlMessage, MsgFlags, SockFlag, SockType, UnixAddr,
    },
};
use uuid::Uuid;

use crate::{
    proto::bpfd_api::ProceedOn,
    server::{
        config::{Config, XdpMode},
        errors::BpfdError,
    },
};

const DEFAULT_ACTIONS_MAP: u32 =
    1 << ProceedOn::Pass as u32 | 1 << ProceedOn::DispatcherReturn as u32;
const DEFAULT_PRIORITY: u32 = 50;
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";
const SUPERUSER: &str = "bpfctl";

#[derive(Debug, Eq, Ord, PartialEq, PartialOrd)]
pub(crate) struct Metadata {
    priority: i32,
    proceed_on_mask: u32,
    name: String,
    attached: bool,
}

pub(crate) struct ExtensionProgram {
    path: String,
    current_position: Option<usize>,
    loader: Option<Bpf>,
    metadata: Metadata,
    link: Option<OwnedLink<ExtensionLink>>,
    owner: String,
}

pub(crate) struct DispatcherProgram {
    mode: XdpMode,
    _loader: Bpf,
    link: Option<OwnedLink<XdpLink>>,
}

#[derive(Debug, Clone)]
pub(crate) struct InterfaceInfo {
    pub(crate) xdp_mode: String,
    pub(crate) programs: Vec<ProgramInfo>,
}

#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: String,
    pub(crate) name: String,
    pub(crate) path: String,
    pub(crate) position: usize,
    pub(crate) priority: i32,
    pub(crate) proceed_on: Vec<i32>,
}

pub(crate) struct BpfManager<'a> {
    config: &'a Config,
    dispatcher_bytes: &'a [u8],
    dispatchers: HashMap<u32, DispatcherProgram>,
    programs: HashMap<u32, HashMap<Uuid, ExtensionProgram>>,
}

impl<'a> BpfManager<'a> {
    pub(crate) fn new(config: &'a Config, dispatcher_bytes: &'a [u8]) -> Self {
        Self {
            config,
            dispatcher_bytes,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
        }
    }

    pub(crate) fn add_program(
        &mut self,
        iface: String,
        path: String,
        priority: i32,
        section_name: String,
        proceed_on: Vec<i32>,
        owner: String,
    ) -> Result<Uuid, BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        let mut ext_loader = BpfLoader::new()
            .extension(&section_name)
            .load_file(path.clone())?;
        ext_loader
            .program_mut(&section_name)
            .ok_or_else(|| BpfdError::SectionNameNotValid(section_name.clone()))?;
        let id = Uuid::new_v4();

        // Calculate the next_available_id
        let next_available_id = if let Some(prog) = self.programs.get(&if_index) {
            prog.len() + 1
        } else {
            self.programs.insert(if_index, HashMap::new());
            1
        };
        if next_available_id > 10 {
            return Err(BpfdError::TooManyPrograms);
        }

        // Process the input proceed_on
        let mut proceed_on_mask: u32 = 0;
        if proceed_on.is_empty() {
            proceed_on_mask = DEFAULT_ACTIONS_MAP;
        } else {
            for action in proceed_on.into_iter() {
                proceed_on_mask |= 1 << action;
            }
        }

        self.programs.get_mut(&if_index).unwrap().insert(
            id,
            ExtensionProgram {
                path,
                loader: Some(ext_loader),
                current_position: None,
                metadata: Metadata {
                    priority,
                    proceed_on_mask,
                    name: section_name,
                    attached: false,
                },
                link: None,
                owner,
            },
        );
        self.sort_extensions(&if_index);

        let mut dispatcher_loader =
            self.new_dispatcher(&if_index, next_available_id as u8, self.dispatcher_bytes)?;

        // Keep old_links in scope until after this function exits to avoid dropping
        // them before the new dispatcher is attached
        let _old_links = self.attach_extensions(&if_index, &mut dispatcher_loader)?;
        self.update_or_replace_dispatcher(iface.clone(), if_index, dispatcher_loader)?;
        info!(
            "Program added: {} programs attached to {}",
            self.programs.get(&if_index).unwrap().len(),
            &iface,
        );
        Ok(id)
    }

    pub(crate) fn remove_program(
        &mut self,
        id: Uuid,
        iface: String,
        owner: String,
    ) -> Result<(), BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if let Some(programs) = self.programs.get_mut(&if_index) {
            if let Some(prog) = programs.get(&id) {
                if !(prog.owner == owner || owner == SUPERUSER) {
                    return Err(BpfdError::NotAuthorized);
                }
            } else {
                return Err(BpfdError::InvalidID);
            }
            // Keep old_program until the dispatcher has been reloaded
            let _old_program = programs.remove(&id).unwrap();
            info!(
                "Program removed: {} programs attached to {}",
                programs.len(),
                &iface,
            );
            if programs.is_empty() {
                self.programs.remove(&if_index);
                self.dispatchers.remove(&if_index);
                return Ok(());
            }

            // Cache program length so programs goes out of scope and
            // sort_extensions() can generate its own list.
            let program_len = programs.len() as u8;
            self.sort_extensions(&if_index);

            let mut dispatcher_loader =
                self.new_dispatcher(&if_index, program_len, self.dispatcher_bytes)?;

            // Keep old_links in scope until after this function exits to avoid dropping
            // them before the new dispatcher is attached
            let _old_links = self.attach_extensions(&if_index, &mut dispatcher_loader)?;

            self.update_or_replace_dispatcher(iface, if_index, dispatcher_loader)?;
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self, iface: String) -> Result<InterfaceInfo, BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if !self.dispatchers.contains_key(&if_index) {
            return Err(BpfdError::NoProgramsLoaded);
        };
        let xdp_mode = self.dispatchers.get(&if_index).unwrap().mode.to_string();
        let mut results = InterfaceInfo {
            xdp_mode,
            programs: vec![],
        };
        let mut extensions = self
            .programs
            .get(&if_index)
            .unwrap()
            .iter()
            .collect::<Vec<_>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (id, v) in extensions.iter() {
            let mut proceed_on = Vec::new();
            if v.metadata.proceed_on_mask & (1 << ProceedOn::Aborted as u32) != 0 {
                proceed_on.push(ProceedOn::Aborted as i32)
            }
            if v.metadata.proceed_on_mask & (1 << ProceedOn::Drop as u32) != 0 {
                proceed_on.push(ProceedOn::Drop as i32)
            }
            if v.metadata.proceed_on_mask & (1 << ProceedOn::Pass as u32) != 0 {
                proceed_on.push(ProceedOn::Pass as i32)
            }
            if v.metadata.proceed_on_mask & (1 << ProceedOn::Tx as u32) != 0 {
                proceed_on.push(ProceedOn::Tx as i32)
            }
            if v.metadata.proceed_on_mask & (1 << ProceedOn::Redirect as u32) != 0 {
                proceed_on.push(ProceedOn::Redirect as i32)
            }
            if v.metadata.proceed_on_mask & (1 << ProceedOn::DispatcherReturn as u32) != 0 {
                proceed_on.push(ProceedOn::DispatcherReturn as i32)
            }
            results.programs.push(ProgramInfo {
                id: id.to_string(),
                name: v.metadata.name.clone(),
                path: v.path.clone(),
                position: v.current_position.unwrap(),
                priority: v.metadata.priority,
                proceed_on,
            })
        }
        Ok(results)
    }

    pub(crate) fn get_map(
        &mut self,
        iface: String,
        id: String,
        map_name: String,
        socket_path: String,
    ) -> Result<(), BpfdError> {
        let if_index = self.get_ifindex(&iface)?;
        if let Some(programs) = self.programs.get_mut(&if_index) {
            let uuid = id.parse::<Uuid>().map_err(|_| BpfdError::InvalidID)?;
            if let Some(target_prog) = programs.get_mut(&uuid) {
                let map = target_prog
                    .loader
                    .as_mut()
                    .unwrap()
                    .map_mut(&map_name)
                    .map_err(|_| BpfdError::MapNotFound)?;
                if let Some(fd) = map.fd() {
                    // FIXME: Error handling here is terrible!
                    // Don't unwrap everything and return a BpfdError::SocketError instead.
                    let path = Path::new(&socket_path);
                    let sock_addr = UnixAddr::new(path).unwrap();
                    let sock = socket(
                        AddressFamily::Unix,
                        SockType::Datagram,
                        SockFlag::empty(),
                        None,
                    )
                    .unwrap();
                    let iov = [IoSlice::new(b"a")];
                    let fds = [fd.as_raw_fd()];
                    let cmsg = ControlMessage::ScmRights(&fds);
                    sendmsg(sock, &iov, &[cmsg], MsgFlags::empty(), Some(&sock_addr)).unwrap();
                } else {
                    return Err(BpfdError::MapNotLoaded);
                }
            }
        } else {
            return Err(BpfdError::NoProgramsLoaded);
        }
        Ok(())
    }

    fn attach_extensions(
        &mut self,
        if_index: &u32,
        dispatcher_loader: &mut Bpf,
    ) -> Result<Vec<OwnedLink<ExtensionLink>>, BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        let mut old_links = vec![];
        let mut extensions = self
            .programs
            .get_mut(if_index)
            .unwrap()
            .values_mut()
            .collect::<Vec<&mut ExtensionProgram>>();
        extensions.sort_by(|a, b| a.current_position.cmp(&b.current_position));
        for (i, v) in extensions.iter_mut().enumerate() {
            if v.metadata.attached {
                let ext: &mut Extension = v
                    .loader
                    .as_mut()
                    .unwrap()
                    .programs_mut()
                    .next()
                    .unwrap()
                    .1
                    .try_into()?;
                let target_fn = format!("prog{}", i);
                let old_link = v.link.take().unwrap();
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                old_links.push(old_link);
                v.link = Some(ext.take_link(new_link_id)?);
                v.metadata.attached = true;
            } else {
                let ext: &mut Extension = v
                    .loader
                    .as_mut()
                    .unwrap()
                    .program_mut(&v.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{}", i);

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                let ext_link = ext.attach()?;
                v.link = Some(ext.take_link(ext_link)?);
                v.metadata.attached = true;
            }
        }
        Ok(old_links)
    }

    fn update_or_replace_dispatcher(
        &mut self,
        iface: String,
        if_index: u32,
        mut dispatcher_loader: Bpf,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        if let Some(mut d) = self.dispatchers.remove(&if_index) {
            let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
            let owned_link = dispatcher.take_link(link)?;
            self.dispatchers.insert(
                if_index,
                DispatcherProgram {
                    mode: d.mode,
                    _loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        } else {
            let mode = if let Some(i) = &self.config.interfaces {
                i.get(&iface).map_or(XdpMode::Skb, |i| i.xdp_mode)
            } else {
                XdpMode::Skb
            };
            let flags = mode.as_flags();
            let link = dispatcher.attach(&iface, flags).unwrap();
            let owned_link = dispatcher.take_link(link)?;
            self.dispatchers.insert(
                if_index,
                DispatcherProgram {
                    mode,
                    _loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        }
        Ok(())
    }

    fn get_ifindex(&mut self, iface: &str) -> Result<u32, BpfdError> {
        match if_nametoindex(iface) {
            Ok(index) => {
                info!("Map {} to {}", iface, index);
                Ok(index)
            }
            Err(_) => {
                info!("Unable to validate interface {}", iface);
                Err(BpfdError::InvalidInterface)
            }
        }
    }

    fn sort_extensions(&mut self, if_index: &u32) {
        let mut extensions = self
            .programs
            .get_mut(if_index)
            .unwrap()
            .values_mut()
            .collect::<Vec<&mut ExtensionProgram>>();
        extensions.sort_by(|a, b| a.metadata.cmp(&b.metadata));
        for (i, v) in extensions.iter_mut().enumerate() {
            v.current_position = Some(i);
        }
    }

    fn new_dispatcher(
        &mut self,
        if_index: &u32,
        num_progs_enabled: u8,
        bytes: &[u8],
    ) -> Result<Bpf, BpfdError> {
        let mut chain_call_actions = [DEFAULT_ACTIONS_MAP; 10];

        let mut extensions = self
            .programs
            .get(if_index)
            .unwrap()
            .iter()
            .collect::<Vec<_>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (_, v) in extensions.iter() {
            chain_call_actions[v.current_position.unwrap()] = v.metadata.proceed_on_mask;
        }

        let config = XdpDispatcherConfig {
            num_progs_enabled,
            chain_call_actions,
            run_prios: [DEFAULT_PRIORITY; 10],
        };

        let mut dispatcher_loader = BpfLoader::new().set_global("CONFIG", &config).load(bytes)?;

        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        dispatcher.load()?;

        Ok(dispatcher_loader)
    }
}
