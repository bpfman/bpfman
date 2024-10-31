// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use bpfman::{
    errors::BpfmanError,
    types::{BytecodeImage, Location, Program},
};

use crate::v1::{
    attach_info::Info, bytecode_location::Location as V1Location, AttachInfo,
    BytecodeImage as V1BytecodeImage, BytecodeLocation, FentryAttachInfo, FexitAttachInfo,
    KernelProgramInfo as V1KernelProgramInfo, KprobeAttachInfo, ProgramInfo,
    ProgramInfo as V1ProgramInfo, TcAttachInfo, TcxAttachInfo, TracepointAttachInfo,
    UprobeAttachInfo, XdpAttachInfo,
};

#[path = "bpfman.v1.rs"]
#[rustfmt::skip]
#[allow(clippy::all)]
pub mod v1;

impl TryFrom<&Program> for ProgramInfo {
    type Error = BpfmanError;

    fn try_from(program: &Program) -> Result<Self, Self::Error> {
        let data = program.get_data();

        let bytecode = match data.get_location()? {
            Location::Image(m) => {
                Some(BytecodeLocation {
                    location: Some(V1Location::Image(V1BytecodeImage {
                        url: m.get_url().to_string(),
                        image_pull_policy: m.get_pull_policy().to_owned() as i32,
                        // Never dump Plaintext Credentials
                        username: Some(String::new()),
                        password: Some(String::new()),
                    })),
                })
            }
            Location::File(m) => Some(BytecodeLocation {
                location: Some(V1Location::File(m.to_string())),
            }),
        };

        let attach_info = AttachInfo {
            info: match program.clone() {
                Program::Xdp(p) => Some(Info::XdpAttachInfo(XdpAttachInfo {
                    priority: p.get_priority()?,
                    iface: p.get_iface()?.to_string(),
                    position: p.get_current_position()?.unwrap_or(0) as i32,
                    proceed_on: p.get_proceed_on()?.as_action_vec(),
                })),
                Program::Tc(p) => Some(Info::TcAttachInfo(TcAttachInfo {
                    priority: p.get_priority()?,
                    iface: p.get_iface()?.to_string(),
                    position: p.get_current_position()?.unwrap_or(0) as i32,
                    direction: p.get_direction()?.to_string(),
                    proceed_on: p.get_proceed_on()?.as_action_vec(),
                })),
                Program::Tcx(p) => Some(Info::TcxAttachInfo(TcxAttachInfo {
                    priority: p.get_priority()?,
                    iface: p.get_iface()?.to_string(),
                    position: p.get_current_position()?.unwrap_or(0) as i32,
                    direction: p.get_direction()?.to_string(),
                })),
                Program::Tracepoint(p) => Some(Info::TracepointAttachInfo(TracepointAttachInfo {
                    tracepoint: p.get_tracepoint()?.to_string(),
                })),
                Program::Kprobe(p) => Some(Info::KprobeAttachInfo(KprobeAttachInfo {
                    fn_name: p.get_fn_name()?.to_string(),
                    offset: p.get_offset()?,
                    retprobe: p.get_retprobe()?,
                    container_pid: p.get_container_pid()?,
                })),
                Program::Uprobe(p) => Some(Info::UprobeAttachInfo(UprobeAttachInfo {
                    fn_name: p.get_fn_name()?.map(|v| v.to_string()),
                    offset: p.get_offset()?,
                    target: p.get_target()?.to_string(),
                    retprobe: p.get_retprobe()?,
                    pid: p.get_pid()?,
                    container_pid: p.get_container_pid()?,
                })),
                Program::Fentry(p) => Some(Info::FentryAttachInfo(FentryAttachInfo {
                    fn_name: p.get_fn_name()?.to_string(),
                })),
                Program::Fexit(p) => Some(Info::FexitAttachInfo(FexitAttachInfo {
                    fn_name: p.get_fn_name()?.to_string(),
                })),
                Program::Unsupported(_) => None,
            },
        };

        // Populate the Program Info with bpfman data
        Ok(V1ProgramInfo {
            name: data.get_name()?.to_string(),
            bytecode,
            attach: Some(attach_info),
            global_data: data.get_global_data()?,
            map_owner_id: data.get_map_owner_id()?,
            map_pin_path: data
                .get_map_pin_path()?
                .map_or(String::new(), |v| v.to_str().unwrap().to_string()),
            map_used_by: data
                .get_maps_used_by()?
                .iter()
                .map(|m| m.to_string())
                .collect(),
            metadata: data.get_metadata()?,
        })
    }
}

impl TryFrom<&Program> for V1KernelProgramInfo {
    type Error = BpfmanError;

    fn try_from(program: &Program) -> Result<Self, Self::Error> {
        // Get the Kernel Info.
        let data = program.get_data();

        // Populate the Kernel Info.
        Ok(V1KernelProgramInfo {
            id: data.get_id()?,
            name: data.get_kernel_name()?.to_string(),
            program_type: program.kind() as u32,
            loaded_at: data.get_kernel_loaded_at()?.to_string(),
            tag: data.get_kernel_tag()?.to_string(),
            gpl_compatible: data.get_kernel_gpl_compatible()?,
            map_ids: data.get_kernel_map_ids()?,
            btf_id: data.get_kernel_btf_id()?,
            bytes_xlated: data.get_kernel_bytes_xlated()?,
            jited: data.get_kernel_jited()?,
            bytes_jited: data.get_kernel_bytes_jited()?,
            bytes_memlock: data.get_kernel_bytes_memlock()?,
            verified_insns: data.get_kernel_verified_insns()?,
        })
    }
}

impl From<V1BytecodeImage> for BytecodeImage {
    fn from(value: V1BytecodeImage) -> Self {
        // This function is mapping an empty string to None for
        // username and password.
        let username = if value.username.is_some() {
            match value.username.unwrap().as_ref() {
                "" => None,
                u => Some(u.to_string()),
            }
        } else {
            None
        };
        let password = if value.password.is_some() {
            match value.password.unwrap().as_ref() {
                "" => None,
                u => Some(u.to_string()),
            }
        } else {
            None
        };
        BytecodeImage::new(value.url, value.image_pull_policy, username, password)
    }
}
