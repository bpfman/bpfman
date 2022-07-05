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
    fcntl::{fcntl, FcntlArg},
    sys::socket::{
        sendmsg, socket, AddressFamily, ControlMessage, MsgFlags, SockFlag, SockType, UnixAddr,
    },
};
use uuid::Uuid;

use crate::server::{
    config::{Config, XdpMode},
    errors::BpfdError,
};

const DEFAULT_ACTIONS_MAP: u32 = 1 << 2;
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
}

pub(crate) struct BpfManager<'a> {
    config: &'a Config,
    dispatcher_bytes: &'a [u8],
    dispatchers: HashMap<String, DispatcherProgram>,
    programs: HashMap<String, HashMap<Uuid, ExtensionProgram>>,
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
        owner: String,
    ) -> Result<Uuid, BpfdError> {
        let id = Uuid::new_v4();
        let next_available_id = if let Some(prog) = self.programs.get(&iface) {
            prog.len()
        } else {
            self.programs.insert(iface.clone(), HashMap::new());
            1
        };

        if next_available_id > 10 {
            return Err(BpfdError::TooManyPrograms);
        }

        let mut dispatcher_loader = new_dispatcher(next_available_id as u8, self.dispatcher_bytes)?;
        self.programs.get_mut(&iface).unwrap().insert(
            id,
            ExtensionProgram {
                path,
                loader: None,
                current_position: None,
                metadata: Metadata {
                    priority,
                    name: section_name,
                    attached: false,
                },
                link: None,
                owner,
            },
        );

        // Keep old_links in scope until after this function exits to avoid dropping
        // them before the new dispatcher is attached
        let _old_links = self.attach_extensions(&iface, &mut dispatcher_loader)?;
        self.update_or_replace_dispatcher(iface.clone(), dispatcher_loader)?;
        info!(
            "{} programs attached to {}",
            self.programs.get(&iface).unwrap().len(),
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
        if let Some(programs) = self.programs.get_mut(&iface) {
            if let Some(prog) = programs.get(&id) {
                if !(prog.owner == owner || owner == SUPERUSER) {
                    return Err(BpfdError::NotAuthorized);
                }
            } else {
                return Err(BpfdError::InvalidID);
            }
            // Keep old_program until the dispatcher has been reloaded
            let _old_program = programs.remove(&id).unwrap();
            if programs.is_empty() {
                return Ok(());
            }

            let mut dispatcher_loader =
                new_dispatcher(programs.len() as u8, self.dispatcher_bytes)?;

            // Keep old_links in scope until after this function exits to avoid dropping
            // them before the new dispatcher is attached
            let _old_links = self.attach_extensions(&iface, &mut dispatcher_loader)?;

            self.update_or_replace_dispatcher(iface, dispatcher_loader)?;
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self, iface: String) -> Result<InterfaceInfo, BpfdError> {
        if !self.dispatchers.contains_key(&iface) {
            return Err(BpfdError::NoProgramsLoaded);
        };
        let xdp_mode = self.dispatchers.get(&iface).unwrap().mode.to_string();
        let mut results = InterfaceInfo {
            xdp_mode,
            programs: vec![],
        };
        let mut extensions = self
            .programs
            .get(&iface)
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
        if let Some(programs) = self.programs.get_mut(&iface) {
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
                    let dup =
                        fcntl(fd.as_raw_fd(), FcntlArg::F_DUPFD_CLOEXEC(fd.as_raw_fd())).unwrap();
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
                    let fds = [dup];
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
        iface: &str,
        dispatcher_loader: &mut Bpf,
    ) -> Result<Vec<OwnedLink<ExtensionLink>>, BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        let mut old_links = vec![];
        let mut extensions = self
            .programs
            .get_mut(iface)
            .unwrap()
            .values_mut()
            .collect::<Vec<&mut ExtensionProgram>>();
        extensions.sort_by(|a, b| a.metadata.cmp(&b.metadata));
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
                v.current_position = Some(i);
                v.metadata.attached = true;
            } else {
                let mut ext_loader = BpfLoader::new()
                    .extension(&v.metadata.name)
                    .load_file(v.path.clone())?;
                let ext: &mut Extension = ext_loader
                    .program_mut(&v.metadata.name)
                    .ok_or_else(|| BpfdError::SectionNameNotValid(v.metadata.name.clone()))?
                    .try_into()?;

                let target_fn = format!("prog{}", i);

                ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                let ext_link = ext.attach()?;
                v.link = Some(ext.take_link(ext_link)?);
                v.loader = Some(ext_loader);
                v.current_position = Some(i);
                v.metadata.attached = true;
            }
        }
        Ok(old_links)
    }

    fn update_or_replace_dispatcher(
        &mut self,
        iface: String,
        mut dispatcher_loader: Bpf,
    ) -> Result<(), BpfdError> {
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;
        if let Some(mut d) = self.dispatchers.remove(&iface) {
            let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
            let owned_link = dispatcher.take_link(link)?;
            self.dispatchers.insert(
                iface.clone(),
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
                iface.clone(),
                DispatcherProgram {
                    mode,
                    _loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        }
        Ok(())
    }
}

fn new_dispatcher(num_progs_enabled: u8, bytes: &[u8]) -> Result<Bpf, BpfdError> {
    let config = XdpDispatcherConfig {
        num_progs_enabled,
        chain_call_actions: [DEFAULT_ACTIONS_MAP; 10],
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
