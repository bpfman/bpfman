// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub mod config;
pub mod util;
#[path = "bpfman.v1.rs"]
#[rustfmt::skip]
#[allow(clippy::all)]
pub mod v1;
use std::iter::FromIterator;

use clap::ValueEnum;
use serde::{Deserialize, Serialize};
use thiserror::Error;
use url::ParseError as urlParseError;
use v1::bytecode_location::Location;

#[derive(Error, Debug)]
pub enum ParseError {
    #[error("{program} is not a valid program type")]
    InvalidProgramType { program: String },
    #[error("{proceedon} is not a valid proceed-on value")]
    InvalidProceedOn { proceedon: String },
    #[error("not a valid direction: {direction}")]
    InvalidDirection { direction: String },
    #[error("Failed to Parse bytecode location: {0}")]
    BytecodeLocationParseFailure(#[source] urlParseError),
    #[error("Invalid bytecode location: {location}")]
    InvalidBytecodeLocation { location: String },
    #[error("Invalid bytecode image pull policy: {pull_policy}")]
    InvalidBytecodeImagePullPolicy { pull_policy: String },
    #[error("{probe} is not a valid probe type")]
    InvalidProbeType { probe: String },
}

#[derive(ValueEnum, Copy, Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
pub enum ProgramType {
    Unspec,
    SocketFilter,
    Probe, // kprobe, kretprobe, uprobe, uretprobe
    Tc,
    SchedAct,
    Tracepoint,
    Xdp,
    PerfEvent,
    CgroupSkb,
    CgroupSock,
    LwtIn,
    LwtOut,
    LwtXmit,
    SockOps,
    SkSkb,
    CgroupDevice,
    SkMsg,
    RawTracepoint,
    CgroupSockAddr,
    LwtSeg6Local,
    LircMode2,
    SkReuseport,
    FlowDissector,
    CgroupSysctl,
    RawTracepointWritable,
    CgroupSockopt,
    Tracing,
    StructOps,
    Ext,
    Lsm,
    SkLookup,
    Syscall,
}

impl TryFrom<String> for ProgramType {
    type Error = ParseError;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "unspec" => ProgramType::Unspec,
            "socket_filter" => ProgramType::SocketFilter,
            "probe" => ProgramType::Probe,
            "tc" => ProgramType::Tc,
            "sched_act" => ProgramType::SchedAct,
            "tracepoint" => ProgramType::Tracepoint,
            "xdp" => ProgramType::Xdp,
            "perf_event" => ProgramType::PerfEvent,
            "cgroup_skb" => ProgramType::CgroupSkb,
            "cgroup_sock" => ProgramType::CgroupSock,
            "lwt_in" => ProgramType::LwtIn,
            "lwt_out" => ProgramType::LwtOut,
            "lwt_xmit" => ProgramType::LwtXmit,
            "sock_ops" => ProgramType::SockOps,
            "sk_skb" => ProgramType::SkSkb,
            "cgroup_device" => ProgramType::CgroupDevice,
            "sk_msg" => ProgramType::SkMsg,
            "raw_tracepoint" => ProgramType::RawTracepoint,
            "cgroup_sock_addr" => ProgramType::CgroupSockAddr,
            "lwt_seg6local" => ProgramType::LwtSeg6Local,
            "lirc_mode2" => ProgramType::LircMode2,
            "sk_reuseport" => ProgramType::SkReuseport,
            "flow_dissector" => ProgramType::FlowDissector,
            "cgroup_sysctl" => ProgramType::CgroupSysctl,
            "raw_tracepoint_writable" => ProgramType::RawTracepointWritable,
            "cgroup_sockopt" => ProgramType::CgroupSockopt,
            "tracing" => ProgramType::Tracing,
            "struct_ops" => ProgramType::StructOps,
            "ext" => ProgramType::Ext,
            "lsm" => ProgramType::Lsm,
            "sk_lookup" => ProgramType::SkLookup,
            "syscall" => ProgramType::Syscall,
            other => {
                return Err(ParseError::InvalidProgramType {
                    program: other.to_string(),
                })
            }
        })
    }
}

impl TryFrom<u32> for ProgramType {
    type Error = ParseError;

    fn try_from(value: u32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ProgramType::Unspec,
            1 => ProgramType::SocketFilter,
            2 => ProgramType::Probe,
            3 => ProgramType::Tc,
            4 => ProgramType::SchedAct,
            5 => ProgramType::Tracepoint,
            6 => ProgramType::Xdp,
            7 => ProgramType::PerfEvent,
            8 => ProgramType::CgroupSkb,
            9 => ProgramType::CgroupSock,
            10 => ProgramType::LwtIn,
            11 => ProgramType::LwtOut,
            12 => ProgramType::LwtXmit,
            13 => ProgramType::SockOps,
            14 => ProgramType::SkSkb,
            15 => ProgramType::CgroupDevice,
            16 => ProgramType::SkMsg,
            17 => ProgramType::RawTracepoint,
            18 => ProgramType::CgroupSockAddr,
            19 => ProgramType::LwtSeg6Local,
            20 => ProgramType::LircMode2,
            21 => ProgramType::SkReuseport,
            22 => ProgramType::FlowDissector,
            23 => ProgramType::CgroupSysctl,
            24 => ProgramType::RawTracepointWritable,
            25 => ProgramType::CgroupSockopt,
            26 => ProgramType::Tracing,
            27 => ProgramType::StructOps,
            28 => ProgramType::Ext,
            29 => ProgramType::Lsm,
            30 => ProgramType::SkLookup,
            31 => ProgramType::Syscall,
            other => {
                return Err(ParseError::InvalidProgramType {
                    program: other.to_string(),
                })
            }
        })
    }
}

impl From<ProgramType> for u32 {
    fn from(val: ProgramType) -> Self {
        match val {
            ProgramType::Unspec => 0,
            ProgramType::SocketFilter => 1,
            ProgramType::Probe => 2,
            ProgramType::Tc => 3,
            ProgramType::SchedAct => 4,
            ProgramType::Tracepoint => 5,
            ProgramType::Xdp => 6,
            ProgramType::PerfEvent => 7,
            ProgramType::CgroupSkb => 8,
            ProgramType::CgroupSock => 9,
            ProgramType::LwtIn => 10,
            ProgramType::LwtOut => 11,
            ProgramType::LwtXmit => 12,
            ProgramType::SockOps => 13,
            ProgramType::SkSkb => 14,
            ProgramType::CgroupDevice => 15,
            ProgramType::SkMsg => 16,
            ProgramType::RawTracepoint => 17,
            ProgramType::CgroupSockAddr => 18,
            ProgramType::LwtSeg6Local => 19,
            ProgramType::LircMode2 => 20,
            ProgramType::SkReuseport => 21,
            ProgramType::FlowDissector => 22,
            ProgramType::CgroupSysctl => 23,
            ProgramType::RawTracepointWritable => 24,
            ProgramType::CgroupSockopt => 25,
            ProgramType::Tracing => 26,
            ProgramType::StructOps => 27,
            ProgramType::Ext => 28,
            ProgramType::Lsm => 29,
            ProgramType::SkLookup => 30,
            ProgramType::Syscall => 31,
        }
    }
}

impl std::fmt::Display for ProgramType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            ProgramType::Unspec => "unspec",
            ProgramType::SocketFilter => "socket_filter",
            ProgramType::Probe => "probe",
            ProgramType::Tc => "tc",
            ProgramType::SchedAct => "sched_act",
            ProgramType::Tracepoint => "tracepoint",
            ProgramType::Xdp => "xdp",
            ProgramType::PerfEvent => "perf_event",
            ProgramType::CgroupSkb => "cgroup_skb",
            ProgramType::CgroupSock => "cgroup_sock",
            ProgramType::LwtIn => "lwt_in",
            ProgramType::LwtOut => "lwt_out",
            ProgramType::LwtXmit => "lwt_xmit",
            ProgramType::SockOps => "sock_ops",
            ProgramType::SkSkb => "sk_skb",
            ProgramType::CgroupDevice => "cgroup_device",
            ProgramType::SkMsg => "sk_msg",
            ProgramType::RawTracepoint => "raw_tracepoint",
            ProgramType::CgroupSockAddr => "cgroup_sock_addr",
            ProgramType::LwtSeg6Local => "lwt_seg6local",
            ProgramType::LircMode2 => "lirc_mode2",
            ProgramType::SkReuseport => "sk_reuseport",
            ProgramType::FlowDissector => "flow_dissector",
            ProgramType::CgroupSysctl => "cgroup_sysctl",
            ProgramType::RawTracepointWritable => "raw_tracepoint_writable",
            ProgramType::CgroupSockopt => "cgroup_sockopt",
            ProgramType::Tracing => "tracing",
            ProgramType::StructOps => "struct_ops",
            ProgramType::Ext => "ext",
            ProgramType::Lsm => "lsm",
            ProgramType::SkLookup => "sk_lookup",
            ProgramType::Syscall => "syscall",
        };
        write!(f, "{v}")
    }
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
pub enum ProbeType {
    Kprobe,
    Kretprobe,
    Uprobe,
    Uretprobe,
}

impl TryFrom<i32> for ProbeType {
    type Error = ParseError;

    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ProbeType::Kprobe,
            1 => ProbeType::Kretprobe,
            2 => ProbeType::Uprobe,
            3 => ProbeType::Uretprobe,
            other => {
                return Err(ParseError::InvalidProbeType {
                    probe: other.to_string(),
                })
            }
        })
    }
}

impl From<aya::programs::ProbeKind> for ProbeType {
    fn from(value: aya::programs::ProbeKind) -> Self {
        match value {
            aya::programs::ProbeKind::KProbe => ProbeType::Kprobe,
            aya::programs::ProbeKind::KRetProbe => ProbeType::Kretprobe,
            aya::programs::ProbeKind::UProbe => ProbeType::Uprobe,
            aya::programs::ProbeKind::URetProbe => ProbeType::Uretprobe,
        }
    }
}

impl std::fmt::Display for ProbeType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            ProbeType::Kprobe => "kprobe",
            ProbeType::Kretprobe => "kretprobe",
            ProbeType::Uprobe => "uprobe",
            ProbeType::Uretprobe => "uretprobe",
        };
        write!(f, "{v}")
    }
}

#[derive(Serialize, Deserialize, Copy, Clone, Debug)]
pub enum XdpProceedOnEntry {
    Aborted,
    Drop,
    Pass,
    Tx,
    Redirect,
    DispatcherReturn = 31,
}

impl FromIterator<XdpProceedOnEntry> for XdpProceedOn {
    fn from_iter<I: IntoIterator<Item = XdpProceedOnEntry>>(iter: I) -> Self {
        let mut c = Vec::new();

        for i in iter {
            c.push(i);
        }

        XdpProceedOn(c)
    }
}

impl TryFrom<String> for XdpProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "aborted" => XdpProceedOnEntry::Aborted,
            "drop" => XdpProceedOnEntry::Drop,
            "pass" => XdpProceedOnEntry::Pass,
            "tx" => XdpProceedOnEntry::Tx,
            "redirect" => XdpProceedOnEntry::Redirect,
            "dispatcher_return" => XdpProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl TryFrom<i32> for XdpProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => XdpProceedOnEntry::Aborted,
            1 => XdpProceedOnEntry::Drop,
            2 => XdpProceedOnEntry::Pass,
            3 => XdpProceedOnEntry::Tx,
            4 => XdpProceedOnEntry::Redirect,
            31 => XdpProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl std::fmt::Display for XdpProceedOnEntry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            XdpProceedOnEntry::Aborted => "aborted",
            XdpProceedOnEntry::Drop => "drop",
            XdpProceedOnEntry::Pass => "pass",
            XdpProceedOnEntry::Tx => "tx",
            XdpProceedOnEntry::Redirect => "redirect",
            XdpProceedOnEntry::DispatcherReturn => "dispatcher_return",
        };
        write!(f, "{v}")
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct XdpProceedOn(Vec<XdpProceedOnEntry>);
impl Default for XdpProceedOn {
    fn default() -> Self {
        XdpProceedOn(vec![
            XdpProceedOnEntry::Pass,
            XdpProceedOnEntry::DispatcherReturn,
        ])
    }
}

impl XdpProceedOn {
    pub fn from_strings<T: AsRef<[String]>>(values: T) -> Result<XdpProceedOn, ParseError> {
        let entries = values.as_ref();
        let mut res = vec![];
        for e in entries {
            res.push(e.to_owned().try_into()?)
        }
        Ok(XdpProceedOn(res))
    }

    pub fn from_int32s<T: AsRef<[i32]>>(values: T) -> Result<XdpProceedOn, ParseError> {
        let entries = values.as_ref();
        if entries.is_empty() {
            return Ok(XdpProceedOn::default());
        }
        let mut res = vec![];
        for e in entries {
            res.push((*e).try_into()?)
        }
        Ok(XdpProceedOn(res))
    }

    pub fn mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        for action in self.0.clone().into_iter() {
            proceed_on_mask |= 1 << action as u32;
        }
        proceed_on_mask
    }

    pub fn as_action_vec(&self) -> Vec<i32> {
        let mut res = vec![];
        for entry in &self.0 {
            res.push((*entry) as i32)
        }
        res
    }
}

impl std::fmt::Display for XdpProceedOn {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let res: Vec<String> = self.0.iter().map(|x| x.to_string()).collect();
        write!(f, "{}", res.join(", "))
    }
}

#[derive(Serialize, Deserialize, Copy, Clone, Debug)]
pub enum TcProceedOnEntry {
    Unspec = -1,
    Ok = 0,
    Reclassify,
    Shot,
    Pipe,
    Stolen,
    Queued,
    Repeat,
    Redirect,
    Trap,
    DispatcherReturn = 30,
}

impl TryFrom<String> for TcProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "unspec" => TcProceedOnEntry::Unspec,
            "ok" => TcProceedOnEntry::Ok,
            "reclassify" => TcProceedOnEntry::Reclassify,
            "shot" => TcProceedOnEntry::Shot,
            "pipe" => TcProceedOnEntry::Pipe,
            "stolen" => TcProceedOnEntry::Stolen,
            "queued" => TcProceedOnEntry::Queued,
            "repeat" => TcProceedOnEntry::Repeat,
            "redirect" => TcProceedOnEntry::Redirect,
            "trap" => TcProceedOnEntry::Trap,
            "dispatcher_return" => TcProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl TryFrom<i32> for TcProceedOnEntry {
    type Error = ParseError;
    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            -1 => TcProceedOnEntry::Unspec,
            0 => TcProceedOnEntry::Ok,
            1 => TcProceedOnEntry::Reclassify,
            2 => TcProceedOnEntry::Shot,
            3 => TcProceedOnEntry::Pipe,
            4 => TcProceedOnEntry::Stolen,
            5 => TcProceedOnEntry::Queued,
            6 => TcProceedOnEntry::Repeat,
            7 => TcProceedOnEntry::Redirect,
            8 => TcProceedOnEntry::Trap,
            30 => TcProceedOnEntry::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl std::fmt::Display for TcProceedOnEntry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            TcProceedOnEntry::Unspec => "unspec",
            TcProceedOnEntry::Ok => "ok",
            TcProceedOnEntry::Reclassify => "reclassify",
            TcProceedOnEntry::Shot => "shot",
            TcProceedOnEntry::Pipe => "pipe",
            TcProceedOnEntry::Stolen => "stolen",
            TcProceedOnEntry::Queued => "queued",
            TcProceedOnEntry::Repeat => "repeat",
            TcProceedOnEntry::Redirect => "redirect",
            TcProceedOnEntry::Trap => "trap",
            TcProceedOnEntry::DispatcherReturn => "dispatcher_return",
        };
        write!(f, "{v}")
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct TcProceedOn(pub(crate) Vec<TcProceedOnEntry>);
impl Default for TcProceedOn {
    fn default() -> Self {
        TcProceedOn(vec![
            TcProceedOnEntry::Pipe,
            TcProceedOnEntry::DispatcherReturn,
        ])
    }
}

impl FromIterator<TcProceedOnEntry> for TcProceedOn {
    fn from_iter<I: IntoIterator<Item = TcProceedOnEntry>>(iter: I) -> Self {
        let mut c = Vec::new();

        for i in iter {
            c.push(i);
        }

        TcProceedOn(c)
    }
}

impl TcProceedOn {
    pub fn from_strings<T: AsRef<[String]>>(values: T) -> Result<TcProceedOn, ParseError> {
        let entries = values.as_ref();
        let mut res = vec![];
        for e in entries {
            res.push(e.to_owned().try_into()?)
        }
        Ok(TcProceedOn(res))
    }

    pub fn from_int32s<T: AsRef<[i32]>>(values: T) -> Result<TcProceedOn, ParseError> {
        let entries = values.as_ref();
        if entries.is_empty() {
            return Ok(TcProceedOn::default());
        }
        let mut res = vec![];
        for e in entries {
            res.push((*e).try_into()?)
        }
        Ok(TcProceedOn(res))
    }

    // Valid TC return values range from -1 to 8.  Since -1 is not a valid shift value,
    // 1 is added to the value to determine the bit to set in the bitmask and,
    // correspondingly, The TC dispatcher adds 1 to the return value from the BPF program
    // before it compares it to the configured bit mask.
    pub fn mask(&self) -> u32 {
        let mut proceed_on_mask: u32 = 0;
        for action in self.0.clone().into_iter() {
            proceed_on_mask |= 1 << ((action as i32) + 1);
        }
        proceed_on_mask
    }

    pub fn as_action_vec(&self) -> Vec<i32> {
        let mut res = vec![];
        for entry in &self.0 {
            res.push((*entry) as i32)
        }
        res
    }
}

impl std::fmt::Display for TcProceedOn {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let res: Vec<String> = self.0.iter().map(|x| x.to_string()).collect();
        write!(f, "{}", res.join(", "))
    }
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub enum ImagePullPolicy {
    Always,
    IfNotPresent,
    Never,
}

impl std::fmt::Display for ImagePullPolicy {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let v = match self {
            ImagePullPolicy::Always => "Always",
            ImagePullPolicy::IfNotPresent => "IfNotPresent",
            ImagePullPolicy::Never => "Never",
        };
        write!(f, "{v}")
    }
}

impl TryFrom<i32> for ImagePullPolicy {
    type Error = ParseError;
    fn try_from(value: i32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ImagePullPolicy::Always,
            1 => ImagePullPolicy::IfNotPresent,
            2 => ImagePullPolicy::Never,
            policy => {
                return Err(ParseError::InvalidBytecodeImagePullPolicy {
                    pull_policy: policy.to_string(),
                })
            }
        })
    }
}

impl TryFrom<&str> for ImagePullPolicy {
    type Error = ParseError;
    fn try_from(value: &str) -> Result<Self, Self::Error> {
        Ok(match value {
            "Always" => ImagePullPolicy::Always,
            "IfNotPresent" => ImagePullPolicy::IfNotPresent,
            "Never" => ImagePullPolicy::Never,
            policy => {
                return Err(ParseError::InvalidBytecodeImagePullPolicy {
                    pull_policy: policy.to_string(),
                })
            }
        })
    }
}

impl From<ImagePullPolicy> for i32 {
    fn from(value: ImagePullPolicy) -> Self {
        match value {
            ImagePullPolicy::Always => 0,
            ImagePullPolicy::IfNotPresent => 1,
            ImagePullPolicy::Never => 2,
        }
    }
}

impl ToString for Location {
    fn to_string(&self) -> String {
        match &self {
            // Cast imagePullPolicy into it's concrete type so we can easily print.
            Location::Image(i) => format!(
                "image: {{ url: {}, pullpolicy: {} }}",
                i.url,
                TryInto::<ImagePullPolicy>::try_into(i.image_pull_policy).unwrap()
            ),
            Location::File(p) => format!("file: {{ path: {p} }}"),
        }
    }
}
