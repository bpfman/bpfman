// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    cell::RefCell,
    fs::File,
    os::fd::{FromRawFd, OwnedFd, RawFd},
};

use anyhow::{anyhow, bail};
use netlink_packet_core::{NLM_F_DUMP, NLM_F_REQUEST, NetlinkMessage, NetlinkPayload};
use netlink_packet_route::{
    RouteNetlinkMessage,
    tc::{TcAttribute, TcMessage},
};
use netlink_sys::{Socket, SocketAddr, constants::NETLINK_ROUTE};
use nix::{
    fcntl::{self, OFlag},
    sched::{CloneFlags, setns},
    sys::stat::Mode,
};

pub struct NetlinkManager {
    sock: RefCell<Socket>,
}

impl NetlinkManager {
    pub(crate) fn new() -> Self {
        NetlinkManager {
            sock: RefCell::new(init_sock()),
        }
    }

    #[allow(dead_code)]
    pub(crate) fn new_in_namespace(ns: &str) -> Result<Self, anyhow::Error> {
        Ok(NetlinkManager {
            sock: RefCell::new(init_namespace_sock(ns)?),
        })
    }

    pub(crate) fn has_qdisc(
        &self,
        qdisc_name: String,
        if_index: i32,
    ) -> Result<bool, anyhow::Error> {
        let mut req =
            NetlinkMessage::from(RouteNetlinkMessage::GetQueueDiscipline(TcMessage::default()));
        req.header.flags = NLM_F_REQUEST | NLM_F_DUMP;

        req.finalize();
        let mut buf = vec![0; req.header.length as usize];
        req.serialize(&mut buf);

        let socket = self.sock.borrow_mut();
        socket
            .send(&buf, 0)
            .expect("failed to send netlink message");

        let mut receive_buffer = vec![0; 4096];
        let mut found = false;
        loop {
            let n = socket.recv(&mut &mut receive_buffer[..], 0)?;
            let bytes = &receive_buffer[..n];
            let rx_packet: NetlinkMessage<RouteNetlinkMessage> =
                NetlinkMessage::deserialize(bytes).unwrap();
            match rx_packet.payload {
                NetlinkPayload::Done(_) => break,
                NetlinkPayload::Error(e) => bail!(e),
                NetlinkPayload::InnerMessage(RouteNetlinkMessage::GetQueueDiscipline(
                    qdisc_message,
                )) => {
                    if qdisc_message.header.index == if_index
                        && qdisc_message
                            .attributes
                            .contains(&TcAttribute::Kind(qdisc_name.clone()))
                    {
                        found = true;
                        break;
                    }
                    continue;
                }
                _ => continue,
            }
        }
        Ok(found)
    }
}

fn init_sock() -> Socket {
    let mut socket = Socket::new(NETLINK_ROUTE).unwrap();
    socket.bind_auto().unwrap();
    socket.connect(&SocketAddr::new(0, 0)).unwrap();
    socket
}

fn init_namespace_sock(namespace: &str) -> Result<Socket, anyhow::Error> {
    let current_netns = current_netns()?;
    change_netns_fd(namespace)?;
    let mut socket = Socket::new(NETLINK_ROUTE).unwrap();
    socket.bind_auto().unwrap();
    socket.connect(&SocketAddr::new(0, 0)).unwrap();
    change_netns_id(current_netns)?;
    Ok(socket)
}

pub fn current_netns() -> Result<OwnedFd, anyhow::Error> {
    // FD is opened with CLOEXEC so it will be closed once we exit
    // We need to keep this alive so we can get back home

    let fd = fcntl::open(
        "/proc/self/ns/net",
        OFlag::O_CLOEXEC | OFlag::O_RDONLY,
        Mode::empty(),
    )
    .map_err(|e| anyhow!(e))? as RawFd;
    let fd = unsafe { OwnedFd::from_raw_fd(fd) };
    Ok(fd)
}

fn change_netns_fd(path: &str) -> Result<(), anyhow::Error> {
    let f = File::open(path)?;
    setns(f, CloneFlags::CLONE_NEWNET).map_err(|e| anyhow!(e))
}

fn change_netns_id(fd: OwnedFd) -> Result<(), anyhow::Error> {
    setns(fd, CloneFlags::CLONE_NEWNET).map_err(|e| anyhow!(e))
}
