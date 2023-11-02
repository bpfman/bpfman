// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::bail;
use bpfman_api::{
    v1::{
        attach_info::Info, bytecode_location::Location, list_response::ListResult,
        KernelProgramInfo, KprobeAttachInfo, ProgramInfo, TcAttachInfo, TracepointAttachInfo,
        UprobeAttachInfo, XdpAttachInfo,
    },
    ImagePullPolicy,
    ProbeType::{Kprobe, Kretprobe, Uprobe, Uretprobe},
    ProgramType, TcProceedOn, XdpProceedOn,
};
use comfy_table::{Cell, Color, Table};
use hex::encode_upper;
pub(crate) struct ProgTable(Table);

impl ProgTable {
    pub(crate) fn new_get_bpfman(r: &Option<ProgramInfo>) -> Result<Self, anyhow::Error> {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec![Cell::new("Bpfman State")
            .add_attribute(comfy_table::Attribute::Bold)
            .add_attribute(comfy_table::Attribute::Underlined)
            .fg(Color::Green)]);

        if r.is_none() {
            table.add_row(vec!["NONE"]);
            return Ok(ProgTable(table));
        }
        let info = r.clone().unwrap();

        if info.bytecode.is_none() {
            table.add_row(vec!["NONE"]);
            return Ok(ProgTable(table));
        }

        if info.name.clone().is_empty() {
            table.add_row(vec!["Name:", "None"]);
        } else {
            table.add_row(vec!["Name:", &info.name.clone()]);
        }

        match info.bytecode.clone().unwrap().location.clone() {
            Some(l) => match l {
                Location::Image(i) => {
                    table.add_row(vec!["Image URL:", &i.url]);
                    table.add_row(vec!["Pull Policy:", &format!{ "{}", TryInto::<ImagePullPolicy>::try_into(i.image_pull_policy)?}]);
                }
                Location::File(p) => {
                    table.add_row(vec!["Path:", &p]);
                }
            },
            // not a bpfman program
            None => {
                table.add_row(vec!["NONE"]);
                return Ok(ProgTable(table));
            }
        }

        if info.global_data.is_empty() {
            table.add_row(vec!["Global:", "None"]);
        } else {
            let mut first = true;
            for (key, value) in info.global_data.clone() {
                let data = &format! {"{key}={}", encode_upper(value)};
                if first {
                    first = false;
                    table.add_row(vec!["Global:", data]);
                } else {
                    table.add_row(vec!["", data]);
                }
            }
        }

        if info.metadata.is_empty() {
            table.add_row(vec!["Metadata:", "None"]);
        } else {
            let mut first = true;
            for (key, value) in info.metadata.clone() {
                let data = &format! {"{key}={value}"};
                if first {
                    first = false;
                    table.add_row(vec!["Metadata:", data]);
                } else {
                    table.add_row(vec!["", data]);
                }
            }
        }

        if info.map_pin_path.clone().is_empty() {
            table.add_row(vec!["Map Pin Path:", "None"]);
        } else {
            table.add_row(vec!["Map Pin Path:", &info.map_pin_path.clone()]);
        }

        match info.map_owner_id {
            Some(id) => table.add_row(vec!["Map Owner ID:", &id.to_string()]),
            None => table.add_row(vec!["Map Owner ID:", "None"]),
        };

        if info.map_used_by.clone().is_empty() {
            table.add_row(vec!["Maps Used By:", "None"]);
        } else {
            let mut first = true;
            for prog_id in info.clone().map_used_by {
                if first {
                    first = false;
                    table.add_row(vec!["Maps Used By:", &prog_id]);
                } else {
                    table.add_row(vec!["", &prog_id]);
                }
            }
        };

        if info.attach.is_some() {
            match info.attach.clone().unwrap().info.unwrap() {
                Info::XdpAttachInfo(XdpAttachInfo {
                    priority,
                    iface,
                    position,
                    proceed_on,
                }) => {
                    let proc_on = match XdpProceedOn::from_int32s(proceed_on) {
                        Ok(p) => p,
                        Err(e) => bail!("error parsing proceed_on {e}"),
                    };

                    table.add_row(vec!["Priority:", &priority.to_string()]);
                    table.add_row(vec!["Iface:", &iface]);
                    table.add_row(vec!["Position:", &position.to_string()]);
                    table.add_row(vec!["Proceed On:", &format!("{proc_on}")]);
                }
                Info::TcAttachInfo(TcAttachInfo {
                    priority,
                    iface,
                    position,
                    direction,
                    proceed_on,
                }) => {
                    let proc_on = match TcProceedOn::from_int32s(proceed_on) {
                        Ok(p) => p,
                        Err(e) => bail!("error parsing proceed_on {e}"),
                    };

                    table.add_row(vec!["Priority:", &priority.to_string()]);
                    table.add_row(vec!["Iface:", &iface]);
                    table.add_row(vec!["Position:", &position.to_string()]);
                    table.add_row(vec!["Direction:", &direction]);
                    table.add_row(vec!["Proceed On:", &format!("{proc_on}")]);
                }
                Info::TracepointAttachInfo(TracepointAttachInfo { tracepoint }) => {
                    table.add_row(vec!["Tracepoint:", &tracepoint]);
                }
                Info::KprobeAttachInfo(KprobeAttachInfo {
                    fn_name,
                    offset,
                    retprobe,
                    namespace,
                }) => {
                    let probe_type = match retprobe {
                        true => Kretprobe,
                        false => Kprobe,
                    };

                    table.add_row(vec!["Probe Type:", &format!["{probe_type}"]]);
                    table.add_row(vec!["Function Name:", &fn_name]);
                    table.add_row(vec!["Offset:", &offset.to_string()]);
                    table.add_row(vec!["Namespace", &namespace.unwrap_or("".to_string())]);
                }
                Info::UprobeAttachInfo(UprobeAttachInfo {
                    fn_name,
                    offset,
                    target,
                    retprobe,
                    pid,
                    namespace,
                }) => {
                    let probe_type = match retprobe {
                        true => Uretprobe,
                        false => Uprobe,
                    };

                    table.add_row(vec!["Probe Type:", &format!["{probe_type}"]]);
                    table.add_row(vec!["Function Name:", &fn_name.unwrap_or("".to_string())]);
                    table.add_row(vec!["Offset:", &offset.to_string()]);
                    table.add_row(vec!["Target:", &target]);
                    table.add_row(vec!["PID", &pid.unwrap_or(0).to_string()]);
                    table.add_row(vec!["Namespace", &namespace.unwrap_or("".to_string())]);
                }
            }
        }

        Ok(ProgTable(table))
    }

    pub(crate) fn new_get_unsupported(
        r: &Option<KernelProgramInfo>,
    ) -> Result<Self, anyhow::Error> {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec![Cell::new("Kernel State")
            .add_attribute(comfy_table::Attribute::Bold)
            .add_attribute(comfy_table::Attribute::Underlined)
            .fg(Color::Green)]);

        if r.is_none() {
            table.add_row(vec!["NONE"]);
            return Ok(ProgTable(table));
        }
        let kernel_info = r.clone().unwrap();

        let name = if kernel_info.name.clone().is_empty() {
            "None".to_string()
        } else {
            kernel_info.name.clone()
        };

        let rows = vec![
            vec!["ID:".to_string(), kernel_info.id.to_string()],
            vec!["Name:".to_string(), name],
            vec![
                "Type:".to_string(),
                format!("{}", ProgramType::try_from(kernel_info.program_type)?),
            ],
            vec!["Loaded At:".to_string(), kernel_info.loaded_at.clone()],
            vec!["Tag:".to_string(), kernel_info.tag.clone()],
            vec![
                "GPL Compatible:".to_string(),
                kernel_info.gpl_compatible.to_string(),
            ],
            vec!["Map IDs:".to_string(), format!("{:?}", kernel_info.map_ids)],
            vec!["BTF ID:".to_string(), kernel_info.btf_id.to_string()],
            vec![
                "Size Translated (bytes):".to_string(),
                kernel_info.bytes_xlated.to_string(),
            ],
            vec!["JITted:".to_string(), kernel_info.jited.to_string()],
            vec![
                "Size JITted:".to_string(),
                kernel_info.bytes_jited.to_string(),
            ],
            vec![
                "Kernel Allocated Memory (bytes):".to_string(),
                kernel_info.bytes_memlock.to_string(),
            ],
            vec![
                "Verified Instruction Count:".to_string(),
                kernel_info.verified_insns.to_string(),
            ],
        ];
        table.add_rows(rows);

        Ok(ProgTable(table))
    }

    pub(crate) fn new_list() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec!["Program ID", "Name", "Type", "Load Time"]);
        ProgTable(table)
    }

    pub(crate) fn add_row_list(
        &mut self,
        id: String,
        name: String,
        type_: String,
        load_time: String,
    ) {
        self.0.add_row(vec![id, name, type_, load_time]);
    }

    pub(crate) fn add_response_prog(&mut self, r: ListResult) -> anyhow::Result<()> {
        if r.kernel_info.is_none() {
            self.0.add_row(vec!["NONE"]);
            return Ok(());
        }
        let kernel_info = r.kernel_info.unwrap();

        self.add_row_list(
            kernel_info.id.to_string(),
            kernel_info.name,
            (ProgramType::try_from(kernel_info.program_type)?).to_string(),
            kernel_info.loaded_at,
        );

        Ok(())
    }

    pub(crate) fn print(&self) {
        println!("{self}\n")
    }
}

impl std::fmt::Display for ProgTable {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}
