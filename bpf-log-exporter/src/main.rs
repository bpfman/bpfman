// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{cell::RefCell, num::NonZeroI32, str::from_utf8};

use anyhow::{anyhow, Context};
use clap::Parser;
use log::{debug, info};
use netlink_packet_audit::AuditMessage;
use netlink_packet_core::{NetlinkMessage, NetlinkPayload};
use netlink_sys::{protocols::NETLINK_AUDIT, Socket, SocketAddr};
use regex::Regex;

shadow_rs::shadow!(build);
use crate::build::CLAP_LONG_VERSION;

#[derive(Parser)]
#[clap(author, version=CLAP_LONG_VERSION, about, long_about = None)]
struct Cli {}

fn main() -> anyhow::Result<()> {
    env_logger::init();

    let _cli = Cli::parse();

    let audit = NetlinkAudit::new()?;
    audit
        .receive_loop()
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

// From include/uapi/linux/audit.h
const AUDIT_SYSCALL: u16 = 1300;
const AUDIT_EOE: u16 = 1320;
const AUDIT_PROCTITLE: u16 = 1327;
const AUDIT_BPF: u16 = 1334;

impl NetlinkAudit {
    fn new() -> anyhow::Result<Self> {
        let mut sock = match Socket::new(NETLINK_AUDIT) {
            Ok(s) => s,
            Err(_) => {
                return Err(anyhow!(
                    "unable to create socket, make sure audit is enabled"
                ))
            }
        };
        if sock.bind_auto().is_err() {
            return Err(anyhow!(
                "unable to bind to socket, make sure audit is enabled"
            ));
        }
        if sock.connect(&SocketAddr::new(0, 0)).is_err() {
            return Err(anyhow!("unable to connect, make sure audit is enabled"));
        }
        if sock.add_membership(NETLINK_GROUP_READ_LOG).is_err() {
            return Err(anyhow!("failed to add membership"));
        }

        Ok(NetlinkAudit {
            sock: RefCell::new(sock),
        })
    }

    fn receive_loop(&self) -> anyhow::Result<()> {
        let mut receive_buffer = vec![0; 4096];
        let socket = self.sock.borrow_mut();
        let is_hex = Regex::new(r"^[0-9A-F]+$").map_err(|e| anyhow!("regex new() failed: {e}"))?;
        let mut log_paylod = LogMessage::new();
        loop {
            let n = socket.recv(&mut &mut receive_buffer[..], 0)?;
            let bytes = &receive_buffer[..n];
            let rx_packet = match <NetlinkMessage<AuditMessage>>::deserialize(bytes) {
                Ok(packet) => packet,
                Err(e) => {
                    info!("Unable to deserialize packet: {e}");
                    continue;
                }
            };
            match rx_packet.payload {
                NetlinkPayload::InnerMessage(AuditMessage::Event((event_id, message))) => {
                    match event_id {
                        AUDIT_SYSCALL => {
                            // syscall event - 321 is sys_bpf
                            if message.contains("syscall=321") {
                                let parts: Vec<&str> = message.split_whitespace().collect();
                                let a0 =
                                    parse_field(parts.clone(), "a0=", '=', true, "AUDIT_SYSCALL");
                                let uid =
                                    parse_field(parts.clone(), "uid=", '=', true, "AUDIT_SYSCALL");
                                let pid =
                                    parse_field(parts.clone(), "pid=", '=', true, "AUDIT_SYSCALL");
                                let gid =
                                    parse_field(parts.clone(), "gid=", '=', true, "AUDIT_SYSCALL");
                                let comm =
                                    parse_field(parts.clone(), "comm=", '=', true, "AUDIT_SYSCALL");

                                log_paylod.syscall_op = a0.parse().unwrap_or_default();
                                log_paylod.uid = uid.parse().unwrap_or_default();
                                log_paylod.pid = pid.parse().unwrap_or_default();
                                log_paylod.gid = gid.parse().unwrap_or_default();
                                log_paylod.comm = comm.replace('"', "").to_string();
                                info!("AUDIT_SYSCALL (sys_bpf): {:?}", log_paylod);
                            }
                        }
                        AUDIT_BPF => {
                            // ebpf event
                            let parts: Vec<&str> = message.split_whitespace().collect();
                            let timestamp =
                                parse_field(parts.clone(), "audit(", ':', false, "AUDIT_BPF");
                            let prog_id =
                                parse_field(parts.clone(), "prog-id=", '=', true, "AUDIT_BPF");
                            let op = parse_field(parts.clone(), "op=", '=', true, "AUDIT_BPF");

                            log_paylod.timestamp = timestamp.replace("audit(", "").to_string();
                            log_paylod.prog_id = prog_id.parse().unwrap_or_default();
                            log_paylod.op = op.to_string();
                            info!("AUDIT_BPF: {:?}", log_paylod);
                        }
                        AUDIT_PROCTITLE => {
                            // proctitle emit event
                            let parts = message.split_whitespace().collect::<Vec<&str>>();
                            let timestamp =
                                parse_field(parts.clone(), "audit(", ':', false, "AUDIT_PROCTITLE");
                            let proctile = parse_field(
                                parts.clone(),
                                "proctitle",
                                '=',
                                true,
                                "AUDIT_PROCTITLE",
                            );

                            log_paylod.timestamp = timestamp.replace("audit(", "").to_string();
                            if is_hex.is_match(proctile) {
                                let decoded_str = hex::decode(proctile);
                                if let Ok(decoded_str) = decoded_str {
                                    let cmdline = from_utf8(&decoded_str);
                                    if let Ok(cmdline) = cmdline {
                                        log_paylod.cmdline = cmdline.replace('\0', " ").to_string();
                                    }
                                }
                            } else {
                                log_paylod.cmdline = proctile.replace('"', "").to_string();
                            }
                            info!("AUDIT_PROCTITLE: {:?}", log_paylod);
                        }
                        AUDIT_EOE => {
                            info!("AUDIT_EOE: {:?}", log_paylod);
                            log_paylod = LogMessage::new();
                        }
                        _ => {
                            debug!("Event {:?} Received but not processed", event_id);
                        }
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

fn parse_field<'a>(
    parts: Vec<&'a str>,
    substring: &str,
    delimiter: char,
    last: bool,
    msg_type: &str,
) -> &'a str {
    let tmp = parts.iter().find(|s| s.starts_with(substring));

    if let Some(tmp) = tmp {
        let value = if last {
            tmp.split(delimiter).next_back()
        } else {
            tmp.split(delimiter).next()
        };

        if let Some(value) = value {
            return value;
        }
    }

    debug!("{msg_type}: Unable to parse {substring}");
    ""
}
