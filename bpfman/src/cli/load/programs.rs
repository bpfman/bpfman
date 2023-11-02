// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::bail;
use bpfman_api::{
    v1::{
        attach_info::Info, AttachInfo, KprobeAttachInfo, TcAttachInfo, TracepointAttachInfo,
        UprobeAttachInfo, XdpAttachInfo,
    },
    ProgramType, TcProceedOn, XdpProceedOn,
};
use clap::Subcommand;
use hex::FromHex;

#[derive(Clone, Debug)]
pub(crate) struct GlobalArg {
    name: String,
    value: Vec<u8>,
}

#[derive(Subcommand, Debug)]
pub(crate) enum LoadCommands {
    /// Install an eBPF program on the XDP hook point for a given interface.
    Xdp {
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(short, long)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Example: --proceed-on "pass" --proceed-on "drop"
        ///
        /// [possible values: aborted, drop, pass, tx, redirect, dispatcher_return]
        ///
        /// [default: pass, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,
    },
    /// Install an eBPF program on the TC hook point for a given interface.
    Tc {
        /// Required: Direction to apply program.
        ///
        /// [possible values: ingress, egress]
        #[clap(short, long, verbatim_doc_comment)]
        direction: String,

        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(short, long)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Example: --proceed-on "ok" --proceed-on "pipe"
        ///
        /// [possible values: unspec, ok, reclassify, shot, pipe, stolen, queued,
        ///                   repeat, redirect, trap, dispatcher_return]
        ///
        /// [default: ok, pipe, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,
    },
    /// Install an eBPF program on a Tracepoint.
    Tracepoint {
        /// Required: The tracepoint to attach to.
        /// Example: --tracepoint "sched/sched_switch"
        #[clap(short, long, verbatim_doc_comment)]
        tracepoint: String,
    },
    /// Install an eBPF kprobe or kretprobe
    Kprobe {
        /// Required: Function to attach the kprobe to.
        #[clap(short, long)]
        fn_name: String,

        /// Optional: Offset added to the address of the function for kprobe.
        /// Not allowed for kretprobes.
        #[clap(short, long, verbatim_doc_comment)]
        offset: Option<u64>,

        /// Optional: Whether the program is a kretprobe.
        ///
        /// [default: false]
        #[clap(short, long, verbatim_doc_comment)]
        retprobe: bool,

        /// Optional: Namespace to attach the kprobe in. (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        namespace: Option<String>,
    },
    /// Install an eBPF uprobe or uretprobe
    Uprobe {
        /// Optional: Function to attach the uprobe to.
        #[clap(short, long)]
        fn_name: Option<String>,

        /// Optional: Offset added to the address of the target function (or
        /// beginning of target if no function is identified). Offsets are
        /// supported for uretprobes, but use with caution because they can
        /// result in unintended side effects.
        #[clap(short, long, verbatim_doc_comment)]
        offset: Option<u64>,

        /// Required: Library name or the absolute path to a binary or library.
        /// Example: --target "libc".
        #[clap(short, long, verbatim_doc_comment)]
        target: String,

        /// Optional: Whether the program is a uretprobe.
        ///
        /// [default: false]
        #[clap(short, long, verbatim_doc_comment)]
        retprobe: bool,

        /// Optional: Only execute uprobe for given process identification number (PID).
        /// If PID is not provided, uprobe executes for all PIDs.
        #[clap(short, long, verbatim_doc_comment)]
        pid: Option<i32>,

        /// Optional: Namespace to attach the uprobe in. (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        namespace: Option<String>,
    },
}

impl LoadCommands {
    pub(crate) fn get_prog_type(&self) -> ProgramType {
        match self {
            LoadCommands::Xdp { .. } => ProgramType::Xdp,
            LoadCommands::Tc { .. } => ProgramType::Tc,
            LoadCommands::Tracepoint { .. } => ProgramType::Tracepoint,
            LoadCommands::Kprobe { .. } => ProgramType::Probe,
            LoadCommands::Uprobe { .. } => ProgramType::Probe,
        }
    }

    pub(crate) fn get_attach_type(&self) -> Result<Option<AttachInfo>, anyhow::Error> {
        match self {
            LoadCommands::Xdp {
                iface,
                priority,
                proceed_on,
            } => {
                let proc_on = match XdpProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(Some(AttachInfo {
                    info: Some(Info::XdpAttachInfo(XdpAttachInfo {
                        priority: *priority,
                        iface: iface.to_string(),
                        position: 0,
                        proceed_on: proc_on.as_action_vec(),
                    })),
                }))
            }
            LoadCommands::Tc {
                direction,
                iface,
                priority,
                proceed_on,
            } => {
                match direction.as_str() {
                    "ingress" | "egress" => (),
                    other => bail!("{} is not a valid direction", other),
                };
                let proc_on = match TcProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(Some(AttachInfo {
                    info: Some(Info::TcAttachInfo(TcAttachInfo {
                        priority: *priority,
                        iface: iface.to_string(),
                        position: 0,
                        direction: direction.to_string(),
                        proceed_on: proc_on.as_action_vec(),
                    })),
                }))
            }
            LoadCommands::Tracepoint { tracepoint } => Ok(Some(AttachInfo {
                info: Some(Info::TracepointAttachInfo(TracepointAttachInfo {
                    tracepoint: tracepoint.to_string(),
                })),
            })),
            LoadCommands::Kprobe {
                fn_name,
                offset,
                retprobe,
                namespace,
            } => {
                if namespace.is_some() {
                    bail!("kprobe namespace option not supported yet");
                }
                let offset = offset.unwrap_or(0);
                Ok(Some(AttachInfo {
                    info: Some(Info::KprobeAttachInfo(KprobeAttachInfo {
                        fn_name: fn_name.to_string(),
                        offset,
                        retprobe: *retprobe,
                        namespace: namespace.clone(),
                    })),
                }))
            }
            LoadCommands::Uprobe {
                fn_name,
                offset,
                target,
                retprobe,
                pid,
                namespace,
            } => {
                if namespace.is_some() {
                    bail!("uprobe namespace option not supported yet");
                }
                let offset = offset.unwrap_or(0);
                Ok(Some(AttachInfo {
                    info: Some(Info::UprobeAttachInfo(UprobeAttachInfo {
                        fn_name: fn_name.clone(),
                        offset,
                        target: target.clone(),
                        retprobe: *retprobe,
                        pid: *pid,
                        namespace: namespace.clone(),
                    })),
                }))
            }
        }
    }
}

pub(crate) fn parse_global(global: &Option<Vec<GlobalArg>>) -> HashMap<String, Vec<u8>> {
    let mut global_data: HashMap<String, Vec<u8>> = HashMap::new();

    if let Some(global) = global {
        for g in global.iter() {
            global_data.insert(g.name.to_string(), g.value.clone());
        }
    }

    global_data
}

pub(crate) fn parse_global_arg(global_arg: &str) -> Result<GlobalArg, std::io::Error> {
    let mut parts = global_arg.split('=');

    let name_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;

    let value_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;
    let value = Vec::<u8>::from_hex(value_str).map_err(|_e| std::io::ErrorKind::InvalidInput)?;
    if value.is_empty() {
        return Err(std::io::ErrorKind::InvalidInput.into());
    }

    Ok(GlobalArg {
        name: name_str.to_string(),
        value,
    })
}
