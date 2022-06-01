use aya::{
    maps::MapFd,
    programs::{
        extension::ExtensionLink, xdp::XdpLink, Extension, OwnedLink, ProgramFd, Xdp, XdpFlags,
    },
    Bpf, BpfLoader,
};
use log::info;
use nix::{
    fcntl::{fcntl, FcntlArg},
    sys::socket::{
        sendmsg, socket, AddressFamily, ControlMessage, MsgFlags, SockFlag, SockType, UnixAddr,
    },
    unistd::close,
};
use std::{collections::HashMap, io::IoSlice, path::Path};
use uuid::Uuid;

use bpfd_common::*;

use crate::errors::BpfdError;

const DEFAULT_ACTIONS_MAP: u32 = 1 << 2;
const DEFAULT_PRIORITY: u32 = 50;
const DISPATCHER_PROGRAM_NAME: &str = "dispatcher";

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
}

pub(crate) struct DispatcherProgram {
    loader: Bpf,
    link: Option<OwnedLink<XdpLink>>,
}

#[derive(Debug, Clone)]
pub(crate) struct ProgramInfo {
    pub(crate) id: String,
    pub(crate) name: String,
    pub(crate) path: String,
    pub(crate) position: usize,
    pub(crate) priority: i32,
}

pub(crate) struct BpfManager {
    dispatcher_bytes: &'static [u8],
    dispatchers: HashMap<String, DispatcherProgram>,
    programs: HashMap<String, HashMap<Uuid, ExtensionProgram>>,
}

impl BpfManager {
    pub(crate) fn new(dispatcher_bytes: &'static [u8]) -> Self {
        Self {
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
    ) -> Result<(), BpfdError> {
        let id = Uuid::new_v4();
        let next_available_id = if let Some(prog) = self.programs.get(&iface) {
            prog.len()
        } else {
            self.programs.insert(iface.clone(), HashMap::new());
            0
        };

        if next_available_id > 9 {
            return Err(BpfdError::TooManyPrograms);
        }

        let mut dispatcher_loader = new_dispatcher(next_available_id as u8, self.dispatcher_bytes)?;
        let dispatcher: &mut Xdp = dispatcher_loader
            .program_mut(DISPATCHER_PROGRAM_NAME)
            .unwrap()
            .try_into()?;

        let mut old_links = vec![];
        if self.programs.contains_key(&iface) {
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
                },
            );
            let mut extensions = self
                .programs
                .get_mut(&iface)
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
                    v.link = Some(ext.forget_link(new_link_id)?);
                    v.current_position = Some(i);
                    v.metadata.attached = true;
                } else {
                    let mut ext_loader = BpfLoader::new()
                        .extension(&v.metadata.name)
                        .load_file(v.path.clone())?;

                    let ext: &mut Extension = ext_loader
                        .program_mut(&v.metadata.name)
                        .unwrap()
                        .try_into()?;

                    let target_fn = format!("prog{}", i);

                    ext.load(dispatcher.fd().unwrap(), &target_fn)?;
                    let ext_link = ext.attach()?;
                    v.link = Some(ext.forget_link(ext_link)?);
                    v.loader = Some(ext_loader);
                    v.current_position = Some(i);
                    v.metadata.attached = true;
                }
            }
        }

        if let Some(mut d) = self.dispatchers.remove(&iface) {
            let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
            let owned_link = dispatcher.forget_link(link)?;
            self.dispatchers.insert(
                iface.clone(),
                DispatcherProgram {
                    loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
            // HACK: Close old dispatcher.
            // I'm not sure why this doesn't get cleaned up on drop of `Bpf`...
            // Probably some fancy refcount thing that isn't aware of bpf_link_update.
            // We should offer program.unload() to avoid unsafe + also fix this in Aya.
            let old_dispatcher: &mut Xdp = d
                .loader
                .program_mut(DISPATCHER_PROGRAM_NAME)
                .unwrap()
                .try_into()?;
            if let Some(fd) = old_dispatcher.fd() {
                close(fd).unwrap();
            }
        } else {
            let link = dispatcher.attach(&iface, XdpFlags::default()).unwrap();
            let owned_link = dispatcher.forget_link(link)?;
            self.dispatchers.insert(
                iface.clone(),
                DispatcherProgram {
                    loader: dispatcher_loader,
                    link: Some(owned_link),
                },
            );
        }
        info!(
            "{} programs attached to {}",
            self.programs.get(&iface).unwrap().len(),
            &iface,
        );
        Ok(())
    }

    pub(crate) fn remove_program(&mut self, id: Uuid, iface: String) -> Result<(), BpfdError> {
        if let Some(programs) = self.programs.get_mut(&iface) {
            // Keep old_program until the dispatcher has been reloaded
            if let Some(mut old_program) = programs.remove(&id) {
                if programs.is_empty() {
                    if let Some(mut dispatcher) = self.dispatchers.remove(&iface) {
                        // HACK: Close old dispatcher.
                        // I'm not sure why this doesn't get cleaned up on drop of `Bpf`...
                        // Probably some fancy refcount thing that isn't aware of bpf_link_update.
                        // We should offer program.unload() to avoid unsafe + also fix this in Aya.
                        let dispatcher_prog: &mut Xdp = dispatcher
                            .loader
                            .program_mut(DISPATCHER_PROGRAM_NAME)
                            .unwrap()
                            .try_into()?;
                        if let Some(fd) = dispatcher_prog.fd() {
                            close(fd).unwrap();
                        }
                    }
                    // HACK: Close old exetnsion.
                    let old_ext: &mut Extension = old_program
                        .loader
                        .as_mut()
                        .unwrap()
                        .program_mut(old_program.metadata.name.as_str())
                        .unwrap()
                        .try_into()?;
                    if let Some(fd) = old_ext.fd() {
                        close(fd).unwrap();
                    }
                    return Ok(());
                }

                let mut dispatcher_loader =
                    new_dispatcher(programs.len() as u8, self.dispatcher_bytes)?;
                let dispatcher: &mut Xdp = dispatcher_loader
                    .program_mut(DISPATCHER_PROGRAM_NAME)
                    .unwrap()
                    .try_into()?;

                let mut old_links = vec![];
                let mut extensions = programs
                    .values_mut()
                    .collect::<Vec<&mut ExtensionProgram>>();
                extensions.sort_by(|a, b| a.metadata.cmp(&b.metadata));
                for (i, mut v) in extensions.iter_mut().enumerate() {
                    let ext: &mut Extension = (*v)
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
                    v.link = Some(ext.forget_link(new_link_id)?);
                }

                if let Some(mut d) = self.dispatchers.remove(&iface) {
                    let link = dispatcher.attach_to_link(d.link.take().unwrap()).unwrap();
                    let owned_link = dispatcher.forget_link(link)?;
                    self.dispatchers.insert(
                        iface.clone(),
                        DispatcherProgram {
                            loader: dispatcher_loader,
                            link: Some(owned_link),
                        },
                    );
                    // HACK: Close old dispatcher.
                    // I'm not sure why this doesn't get cleaned up on drop of `Bpf`...
                    // Probably some fancy refcount thing that isn't aware of bpf_link_update.
                    // We should offer program.unload() to avoid unsafe + also fix this in Aya.
                    let old_dispatcher: &mut Xdp = d
                        .loader
                        .program_mut(DISPATCHER_PROGRAM_NAME)
                        .unwrap()
                        .try_into()?;
                    if let Some(fd) = old_dispatcher.fd() {
                        close(fd).unwrap();
                    }
                }

                // HACK: Close old exetnsion.
                let old_ext: &mut Extension = old_program
                    .loader
                    .as_mut()
                    .unwrap()
                    .program_mut(old_program.metadata.name.as_str())
                    .unwrap()
                    .try_into()?;
                if let Some(fd) = old_ext.fd() {
                    close(fd).unwrap();
                }
            } else {
                return Err(BpfdError::InvalidID);
            }
        } else {
            return Err(BpfdError::NoProgramsLoaded);
        }
        Ok(())
    }

    pub(crate) fn list_programs(&mut self, iface: String) -> Result<Vec<ProgramInfo>, BpfdError> {
        if iface.is_empty() {
            return Err(BpfdError::ArgumentNotProvided("iface".to_string()));
        }
        let mut results = vec![];
        if let Some(programs) = self.programs.get(&iface) {
            let mut extensions = programs.iter().collect::<Vec<_>>();
            extensions.sort_by(|(_, a), (_, b)| a.current_position.cmp(&b.current_position));
            for (id, v) in extensions.iter() {
                results.push(ProgramInfo {
                    id: id.to_string(),
                    name: v.metadata.name.clone(),
                    path: v.path.clone(),
                    position: v.current_position.unwrap(),
                    priority: v.metadata.priority,
                })
            }
        } else {
            return Err(BpfdError::NoProgramsLoaded);
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
                    let dup = fcntl(fd, FcntlArg::F_DUPFD_CLOEXEC(fd)).unwrap();
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
}

fn new_dispatcher(num_progs_enabled: u8, bytes: &'static [u8]) -> Result<Bpf, BpfdError> {
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
