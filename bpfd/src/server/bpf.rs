// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, fs, io::IoSlice, os::unix::prelude::AsRawFd, path::Path};

use aya::{
    programs::{
        links::{FdLink, PinnedLink},
        Extension, Xdp,
    },
    Bpf, BpfLoader,
};
use bpfd_common::*;
use caps::has_cap;
use log::info;
use nix::{
    net::if_::if_nametoindex,
    sys::socket::{
        sendmsg, socket, AddressFamily, ControlMessage, MsgFlags, SockFlag, SockType, UnixAddr,
    },
};
use uuid::Uuid;

use crate::server::{
    config::{Config, XdpMode},
    errors::BpfdError,
};

// Default is Pass and DispatcherReturn
const DEFAULT_ACTIONS_MAP: u32 = 1 << 2 | 1 << 31;
const DEFAULT_PRIORITY: u32 = 50;
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";
const SUPERUSER: &str = "bpfctl";

#[derive(Debug, Eq, Ord, PartialEq, PartialOrd)]
pub(crate) struct Metadata {
    priority: i32,
    name: String,
    attached: bool,
}
pub(crate) struct ExtensionProgram {
    path: String,
    current_position: Option<usize>,
    loader: Option<Bpf>,
    metadata: Metadata,
    link: Option<PinnedLink>,
    owner: String,
    proceed_on: Vec<i32>,
}

impl ExtensionProgram {
    fn proceed_on_mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        if self.proceed_on.is_empty() {
            proceed_on_mask = DEFAULT_ACTIONS_MAP;
        } else {
            for action in self.proceed_on.clone().into_iter() {
                proceed_on_mask |= 1 << action;
            }
        }
        proceed_on_mask
    }
}

pub(crate) struct DispatcherProgram {
    mode: XdpMode,
    _loader: Bpf,
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
    revisions: HashMap<u32, usize>,
}

impl<'a> BpfManager<'a> {
    pub(crate) fn new(config: &'a Config, dispatcher_bytes: &'a [u8]) -> Self {
        Self {
            config,
            dispatcher_bytes,
            dispatchers: HashMap::new(),
            programs: HashMap::new(),
            revisions: HashMap::new(),
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
        info!(
            "has cap_bpf: {}",
            has_cap(None, caps::CapSet::Effective, caps::Capability::CAP_BPF).unwrap()
        );
        info!(
            "has cap_sys_admin: {}",
            has_cap(
                None,
                caps::CapSet::Effective,
                caps::Capability::CAP_SYS_ADMIN
            )
            .unwrap()
        );

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

        // Calculate the dispatcher revision
        let (old_revision, revision) = if let Some(old_revision) = self.revisions.remove(&if_index)
        {
            let r = old_revision.wrapping_add(1);
            self.revisions.insert(if_index, r);
            (Some(old_revision), r)
        } else {
            self.revisions.insert(if_index, 0);
            (None, 0)
        };

        self.programs.get_mut(&if_index).unwrap().insert(
            id,
            ExtensionProgram {
                path,
                loader: Some(ext_loader),
                current_position: None,
                metadata: Metadata {
                    priority,
                    name: section_name,
                    attached: false,
                },
                link: None,
                owner,
                proceed_on,
            },
        );
        self.sort_extensions(&if_index);

        let mut dispatcher_loader = self.new_dispatcher(
            &if_index,
            next_available_id as u8,
            self.dispatcher_bytes,
            revision,
        )?;

        self.attach_extensions(&if_index, &mut dispatcher_loader)?;
        self.attach_or_replace_dispatcher(iface.clone(), if_index, dispatcher_loader)?;

        if let Some(r) = old_revision {
            self.cleanup_extensions(if_index, r)?;
        }

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

            programs.remove(&id).unwrap();

            if programs.is_empty() {
                self.programs.remove(&if_index);
                self.dispatchers.remove(&if_index);
                return Ok(());
            }

            // New dispatcher required: calculate the new dispatcher revision
            let old_revision = self.revisions.remove(&if_index).unwrap();
            let revision = old_revision.wrapping_add(1);
            self.revisions.insert(if_index, revision);

            // Cache program length so programs goes out of scope and
            // sort_extensions() can generate its own list.
            let program_len = programs.len() as u8;
            self.sort_extensions(&if_index);

            let mut dispatcher_loader =
                self.new_dispatcher(&if_index, program_len, self.dispatcher_bytes, revision)?;

            self.attach_extensions(&if_index, &mut dispatcher_loader)?;

            self.attach_or_replace_dispatcher(iface, if_index, dispatcher_loader)?;

            self.cleanup_extensions(if_index, old_revision)?;
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
            results.programs.push(ProgramInfo {
                id: id.to_string(),
                name: v.metadata.name.clone(),
                path: v.path.clone(),
                position: v.current_position.unwrap(),
                priority: v.metadata.priority,
                proceed_on: v.proceed_on.clone(),
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
                    if let Err(result) =
                        sendmsg(sock, &iov, &[cmsg], MsgFlags::empty(), Some(&sock_addr))
                    {
                        info!("sendmsg error: {}", result);
                        return Err(BpfdError::SendFailure);
                    }
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
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        let revision = self.revisions.get(if_index).unwrap();
        let mut extensions = self
            .programs
            .get_mut(if_index)
            .unwrap()
            .iter_mut()
            .collect::<Vec<(&Uuid, &mut ExtensionProgram)>>();
        extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
        for (i, (k, v)) in extensions.iter_mut().enumerate() {
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
                let new_link_id = ext
                    .attach_to_program(dispatcher.fd().unwrap(), &target_fn)
                    .unwrap();
                let new_link = ext.take_link(new_link_id)?;
                let path = format!("/var/run/bpfd/fs/dispatcher-{if_index}-{revision}/prog-{k}");
                let pinned_link = Into::<FdLink>::into(new_link)
                    .pin(path)
                    .map_err(|_| BpfdError::UnableToPin)?;
                v.link = Some(pinned_link);
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
                let new_link_id = ext.attach()?;
                let new_link = ext.take_link(new_link_id)?;
                let path = format!("/var/run/bpfd/fs/dispatcher-{if_index}-{revision}/prog-{k}");
                let fd_link: FdLink = new_link.into();
                let new_link = fd_link.pin(path).map_err(|_| BpfdError::UnableToPin)?;
                v.link = Some(new_link);
                v.metadata.attached = true;
            }
        }
        Ok(())
    }

    fn attach_or_replace_dispatcher(
        &mut self,
        iface: String,
        if_index: u32,
        mut dispatcher_loader: Bpf,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        if let Some(d) = self.dispatchers.remove(&if_index) {
            let path = format!("/var/run/bpfd/fs/dispatcher-{}-link", if_index);
            let pinned_link: FdLink = PinnedLink::from_path(path).unwrap().into();
            dispatcher
                .attach_to_link(pinned_link.try_into().unwrap())
                .unwrap();
            self.dispatchers.insert(
                if_index,
                DispatcherProgram {
                    mode: d.mode,
                    _loader: dispatcher_loader,
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
            let path = format!("/var/run/bpfd/fs/dispatcher-{if_index}-link");
            let _ = TryInto::<FdLink>::try_into(owned_link)
                .unwrap() // TODO: Don't unwrap, although due to minimum kernel version this shouldn't ever panic
                .pin(path)
                .map_err(|_| BpfdError::UnableToPin)?;
            self.dispatchers.insert(
                if_index,
                DispatcherProgram {
                    mode,
                    _loader: dispatcher_loader,
                },
            );
        }
        Ok(())
    }

    fn cleanup_extensions(&self, if_index: u32, revision: usize) -> Result<(), BpfdError> {
        let path = format!("/var/run/bpfd/fs/dispatcher-{if_index}-{revision}");
        fs::remove_dir_all(path).map_err(|io_error| BpfdError::UnableToCleanup { io_error })
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
        revision: usize,
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
            chain_call_actions[v.current_position.unwrap()] = v.proceed_on_mask();
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

        let path = format!("/var/run/bpfd/fs/dispatcher-{if_index}-{revision}");
        fs::create_dir_all(path).unwrap();

        Ok(dispatcher_loader)
    }
}
