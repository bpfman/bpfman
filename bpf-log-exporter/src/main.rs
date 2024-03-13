// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{cell::RefCell, num::NonZeroI32, str::from_utf8};

use anyhow::{anyhow, Context};
use log::info;
use netlink_packet_audit::AuditMessage;
use netlink_packet_core::{NetlinkMessage, NetlinkPayload};
use netlink_sys::{protocols::NETLINK_AUDIT, Socket, SocketAddr};
use regex::Regex;

fn main() -> anyhow::Result<()> {
    env_logger::init();
    let audit = NetlinkAudit::new();
    audit
        .recieve_loop()
        .context("failed to receive audit events")?;
    Ok(())
}

struct NetlinkAudit {
    sock: RefCell<Socket>,
}

#[derive(Debug)]
struct LogMessage {
    timestamp: String,
    prog_id: u32,
    op: String,
    syscall_op: u32,
    pid: u32,
    uid: u32,
    gid: u32,
    comm: String,
    cmdline: String,
}

impl LogMessage {
    fn new() -> Self {
        LogMessage {
            timestamp: String::new(),
            prog_id: 0,
            op: String::new(),
            syscall_op: 0,
            pid: 0,
            uid: 0,
            gid: 0,
            comm: String::new(),
            cmdline: String::new(),
        }
    }
}

const NETLINK_GROUP_READ_LOG: u32 = 0x1;

impl NetlinkAudit {
    fn new() -> Self {
        let mut sock = Socket::new(NETLINK_AUDIT).unwrap();
        sock.bind_auto().unwrap();
        sock.connect(&SocketAddr::new(0, 0)).unwrap();
        sock.add_membership(NETLINK_GROUP_READ_LOG)
            .expect("failed to add membership");
        NetlinkAudit {
            sock: RefCell::new(sock),
        }
    }

    fn recieve_loop(&self) -> anyhow::Result<()> {
        let mut receive_buffer = vec![0; 4096];
        let socket = self.sock.borrow_mut();
        let is_hex = Regex::new(r"^[0-9A-F]+$").unwrap();
        let mut log_paylod = LogMessage::new();
        loop {
            let n = socket.recv(&mut &mut receive_buffer[..], 0)?;
            let bytes = &receive_buffer[..n];
            let rx_packet = <NetlinkMessage<AuditMessage>>::deserialize(bytes).unwrap();
            match rx_packet.payload {
                NetlinkPayload::InnerMessage(AuditMessage::Event((event_id, message))) => {
                    match event_id {
                        1300 => {
                            // syscall event - 321 is sys_bpf
                            if message.contains("syscall=321") {
                                let parts: Vec<&str> = message.split_whitespace().collect();
                                let a0 = parts
                                    .iter()
                                    .find(|s| s.starts_with("a0="))
                                    .unwrap()
                                    .split('=')
                                    .last()
                                    .unwrap();
                                let uid = parts
                                    .iter()
                                    .find(|s| s.starts_with("uid="))
                                    .expect("uid not found")
                                    .split('=')
                                    .last()
                                    .unwrap();
                                let pid = parts
                                    .iter()
                                    .find(|s| s.starts_with("pid="))
                                    .expect("pid not found")
                                    .split('=')
                                    .last()
                                    .unwrap();
                                let gid = parts
                                    .iter()
                                    .find(|s| s.starts_with("gid="))
                                    .expect("gid not found")
                                    .split('=')
                                    .last()
                                    .unwrap();
                                let comm = parts
                                    .iter()
                                    .find(|s| s.starts_with("comm="))
                                    .unwrap()
                                    .split('=')
                                    .last()
                                    .unwrap();

                                log_paylod.syscall_op = a0.parse().unwrap();
                                log_paylod.uid = uid.parse().unwrap();
                                log_paylod.pid = pid.parse().unwrap();
                                log_paylod.gid = gid.parse().unwrap();
                                log_paylod.comm = comm.replace('"', "").to_string();
                            }
                        }
                        1334 => {
                            // ebpf event
                            let parts: Vec<&str> = message.split_whitespace().collect();
                            let prog_id = parts
                                .iter()
                                .find(|s| s.starts_with("prog-id="))
                                .unwrap()
                                .split('=')
                                .last()
                                .unwrap();
                            let op = parts
                                .iter()
                                .find(|s| s.starts_with("op="))
                                .unwrap()
                                .split('=')
                                .last()
                                .unwrap();
                            log_paylod.prog_id = prog_id.parse().unwrap();
                            log_paylod.op = op.to_string();
                        }
                        1327 => {
                            // proctitle emit event
                            let parts = message.split_whitespace().collect::<Vec<&str>>();
                            let timestamp = parts
                                .iter()
                                .find(|s| s.starts_with("audit("))
                                .unwrap()
                                .split(':')
                                .next()
                                .unwrap();
                            log_paylod.timestamp = timestamp.replace("audit(", "").to_string();

                            let proctile = parts
                                .iter()
                                .find(|s| s.starts_with("proctitle="))
                                .unwrap()
                                .split('=')
                                .last()
                                .unwrap();
                            if is_hex.is_match(proctile) {
                                log_paylod.cmdline = from_utf8(&hex::decode(proctile).unwrap())
                                    .unwrap()
                                    .replace('\0', " ")
                                    .to_string();
                            } else {
                                log_paylod.cmdline = proctile.replace('"', "").to_string();
                            }
                        }
                        1320 => {
                            info!("{:?}", log_paylod);
                            log_paylod = LogMessage::new();
                        }
                        _ => {}
                    }
                }
                NetlinkPayload::InnerMessage(AuditMessage::Other(_)) => {
                    // discard messages that are not events
                }
                NetlinkPayload::Error(e) => {
                    if e.code == NonZeroI32::new(-17) {
                        break;
                    } else {
                        return Err(anyhow!(e));
                    }
                }
                m => return Err(anyhow!("unexpected netlink message {:?}", m)),
            }
        }
        Ok(())
    }
}
