// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{collections::HashMap, path::PathBuf};

use bpfman::{
    BpfMap, BpfProgram,
    errors::BpfmanError,
    types::{ImagePullPolicy, Link, Location, Program, ProgramData},
};
use comfy_table::{Cell, Color, Table};
use hex::encode_upper;
use log::warn;

pub(crate) struct ProgTable(Table);

const NUM_LIST_LINKS: usize = 3;

impl ProgTable {
    fn create_bpfman_state_table() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec![
            Cell::new("Bpfman State")
                .add_attribute(comfy_table::Attribute::Bold)
                .add_attribute(comfy_table::Attribute::Underlined)
                .fg(Color::Green),
        ]);

        ProgTable(table)
    }

    fn add_program_detail_fields(&mut self, program: &Program) {
        let data = program.get_data();

        Self::add_string(self, "BPF Function:".to_string(), data.get_name());

        self.0
            .add_row(vec!["Program Type:", &Self::get_type_str(program)]);

        match data.get_location() {
            Ok(location) => match location {
                Location::Image(i) => {
                    let pull_policy =
                        match TryInto::<ImagePullPolicy>::try_into(i.image_pull_policy) {
                            Ok(pp) => pp.to_string(),
                            Err(e) => {
                                warn!("error processing Image Pull Policy: {}", e);
                                "None".to_string()
                            }
                        };

                    self.0.add_row(vec!["Image URL:", &i.image_url]);
                    self.0.add_row(vec!["Pull Policy:", &pull_policy]);
                }
                Location::File(p) => {
                    self.0.add_row(vec!["Path:", &p]);
                }
            },
            Err(e) => {
                warn!("error retrieving Path: {}", e);
                self.0.add_row(vec!["Path:", "None"]);
            }
        };

        match data.get_global_data() {
            Ok(global_data) => {
                if global_data.is_empty() {
                    self.0.add_row(vec!["Global:", "None"]);
                } else {
                    let mut first = true;
                    for (key, value) in global_data {
                        let data = &format! {"{key}={}", encode_upper(value)};
                        if first {
                            first = false;
                            self.0.add_row(vec!["Global:", data]);
                        } else {
                            self.0.add_row(vec!["", data]);
                        }
                    }
                }
            }
            Err(e) => {
                warn!("error retrieving Global Data: {}", e);
                self.0.add_row(vec!["Global:", "None"]);
            }
        };

        Self::add_metadata(self, data.get_metadata());

        Self::add_option_pathbuf(self, "Map Pin Path:".to_string(), data.get_map_pin_path());

        match data.get_map_owner_id() {
            Ok(map_id) => match map_id {
                Some(id) => {
                    self.0.add_row(vec!["Map Owner ID:", &id.to_string()]);
                }
                None => {
                    self.0.add_row(vec!["Map Owner ID:", "None"]);
                }
            },
            Err(e) => {
                warn!("error retrieving Map Owner ID: {}", e);
                self.0.add_row(vec!["Map Owner ID:", "None"]);
            }
        };

        match data.get_maps_used_by() {
            Ok(map_used_by) => {
                if map_used_by.is_empty() {
                    self.0.add_row(vec!["Maps Used By:", "None"]);
                } else {
                    let mut first = true;
                    for prog_id in map_used_by {
                        if first {
                            first = false;
                            self.0.add_row(vec!["Maps Used By:", &prog_id.to_string()]);
                        } else {
                            self.0.add_row(vec!["", &prog_id.to_string()]);
                        }
                    }
                };
            }
            Err(e) => {
                warn!("error retrieving Maps Used By: {}", e);
                self.0.add_row(vec!["Maps Used By:", "None"]);
            }
        };
    }

    fn add_program_multiple_links(&mut self, program: &Program, links: Vec<Link>) {
        if links.is_empty() {
            self.0.add_row(vec!["Links:", "None"]);
        } else {
            let mut first = true;
            for link in links {
                let attach_str: String = format! {"{} ({})",
                    link.get_id().unwrap_or(0),
                    Self::get_attach_str(program, &link),
                };

                if first {
                    first = false;
                    self.0.add_row(vec!["Links:", &attach_str]);
                } else {
                    self.0.add_row(vec!["", &attach_str]);
                }
            }
        }
    }

    fn add_program_single_link(&mut self, program: &Program, link: &Link) {
        let data = program.get_data();

        Self::add_string(self, "BPF Function:".to_string(), data.get_name());

        self.0
            .add_row(vec!["Program Type:", &Self::get_type_str(program)]);

        Self::add_u32(self, "Program ID:".to_string(), link.get_program_id());
        Self::add_u32(self, "Link ID:".to_string(), link.get_id());

        match link {
            Link::Fentry(fentry_link) => {
                match program {
                    Program::Fentry(fentry_program) => {
                        Self::add_string(
                            self,
                            "Attach Function:".to_string(),
                            fentry_program.get_fn_name(),
                        );
                    }
                    _ => {
                        warn!("fentry program type and link type mismatch");
                        self.0.add_row(vec!["Attach Function:", "None"]);
                    }
                };

                Self::add_metadata(self, fentry_link.get_metadata());
            }
            Link::Fexit(fexit_link) => {
                match program {
                    Program::Fexit(fexit_program) => {
                        Self::add_string(
                            self,
                            "Attach Function:".to_string(),
                            fexit_program.get_fn_name(),
                        );
                    }
                    _ => {
                        warn!("fexit program type and link type mismatch");
                        self.0.add_row(vec!["Attach Function:", "None"]);
                    }
                };

                Self::add_metadata(self, fexit_link.get_metadata());
            }
            Link::Kprobe(kprobe_link) => {
                Self::add_string(
                    self,
                    "Attach Function:".to_string(),
                    kprobe_link.get_fn_name(),
                );

                Self::add_u64(self, "Offset:".to_string(), kprobe_link.get_offset());

                Self::add_container_pid(self, kprobe_link.get_container_pid());

                Self::add_metadata(self, kprobe_link.get_metadata());
            }
            Link::Tc(tc_link) => {
                Self::add_string(self, "Interface:".to_string(), tc_link.get_iface());

                match tc_link.get_direction() {
                    Ok(d) => {
                        self.0.add_row(vec!["Direction:", &d.to_string()]);
                    }
                    Err(e) => {
                        warn!("error retrieving Direction: {}", e);
                        self.0.add_row(vec!["Direction:", "None"]);
                    }
                };

                Self::add_i32(self, "Priority:".to_string(), tc_link.get_priority());

                Self::add_option_usize(
                    self,
                    "Position:".to_string(),
                    tc_link.get_current_position(),
                );

                match tc_link.get_proceed_on() {
                    Ok(proceed_on) => {
                        self.0.add_row(vec!["Proceed On:", &proceed_on.to_string()]);
                    }
                    Err(e) => {
                        warn!("error retrieving Proceed On: {}", e);
                        self.0.add_row(vec!["Proceed On:", "None"]);
                    }
                };

                Self::add_option_pathbuf(
                    self,
                    "Network Namespace:".to_string(),
                    tc_link.get_netns(),
                );

                Self::add_metadata(self, tc_link.get_metadata());
            }
            Link::Tcx(tcx_link) => {
                Self::add_string(self, "Interface:".to_string(), tcx_link.get_iface());

                match tcx_link.get_direction() {
                    Ok(d) => {
                        self.0.add_row(vec!["Direction:", &d.to_string()]);
                    }
                    Err(e) => {
                        warn!("error retrieving Direction: {}", e);
                        self.0.add_row(vec!["Direction:", "None"]);
                    }
                };

                Self::add_i32(self, "Priority:".to_string(), tcx_link.get_priority());

                Self::add_option_usize(
                    self,
                    "Position:".to_string(),
                    tcx_link.get_current_position(),
                );

                Self::add_option_pathbuf(
                    self,
                    "Network Namespace:".to_string(),
                    tcx_link.get_netns(),
                );

                Self::add_metadata(self, tcx_link.get_metadata());
            }
            Link::Tracepoint(tracepoint_link) => {
                Self::add_string(
                    self,
                    "Tracepoint:".to_string(),
                    tracepoint_link.get_tracepoint(),
                );

                Self::add_metadata(self, tracepoint_link.get_metadata());
            }
            Link::Uprobe(uprobe_link) => {
                Self::add_string(self, "Target:".to_string(), uprobe_link.get_target());

                Self::add_option_string(
                    self,
                    "Attach Function:".to_string(),
                    uprobe_link.get_fn_name(),
                );

                Self::add_u64(self, "Offset:".to_string(), uprobe_link.get_offset());

                match uprobe_link.get_pid() {
                    Ok(pid) => match pid {
                        Some(p) => {
                            self.0.add_row(vec!["PID:", &p.to_string()]);
                        }
                        None => {
                            self.0.add_row(vec!["PID:", "None"]);
                        }
                    },
                    Err(e) => {
                        warn!("error retrieving PID: {}", e);
                        self.0.add_row(vec!["PID:", "None"]);
                    }
                };

                Self::add_container_pid(self, uprobe_link.get_container_pid());

                Self::add_metadata(self, uprobe_link.get_metadata());
            }
            Link::Xdp(xdp_link) => {
                Self::add_string(self, "Interface:".to_string(), xdp_link.get_iface());

                Self::add_i32(self, "Priority:".to_string(), xdp_link.get_priority());

                Self::add_option_usize(
                    self,
                    "Position:".to_string(),
                    xdp_link.get_current_position(),
                );

                match xdp_link.get_proceed_on() {
                    Ok(proceed_on) => {
                        self.0.add_row(vec!["Proceed On:", &proceed_on.to_string()]);
                    }
                    Err(e) => {
                        warn!("error retrieving Proceed On: {}", e);
                        self.0.add_row(vec!["Proceed On:", "None"]);
                    }
                };

                Self::add_option_pathbuf(
                    self,
                    "Network Namespace:".to_string(),
                    xdp_link.get_netns(),
                );

                Self::add_metadata(self, xdp_link.get_metadata());
            }
        }
    }

    pub(crate) fn new_program(program: &Program, links: Vec<Link>) -> Result<Self, anyhow::Error> {
        let mut table = Self::create_bpfman_state_table();

        table.add_program_detail_fields(program);
        table.add_program_multiple_links(program, links);

        Ok(table)
    }

    pub(crate) fn new_link(program: &Program, link: &Link) -> Result<Self, anyhow::Error> {
        let mut table = Self::create_bpfman_state_table();

        table.add_program_single_link(program, link);

        Ok(table)
    }

    fn create_kernel_info_table() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec![
            Cell::new("Kernel State")
                .add_attribute(comfy_table::Attribute::Bold)
                .add_attribute(comfy_table::Attribute::Underlined)
                .fg(Color::Green),
        ]);

        ProgTable(table)
    }

    fn add_kernel_info(&mut self, program: &Program) {
        let p = program.get_data();

        Self::add_u32(self, "Program ID:".to_string(), p.get_id());
        Self::add_string(self, "BPF Function:".to_string(), p.get_kernel_name());
        self.0.add_row(vec![
            "Kernel Type:".to_string(),
            format!("{}", program.kind()),
        ]);
        Self::add_string(self, "Loaded At:".to_string(), p.get_kernel_loaded_at());
        Self::add_string(self, "Tag:".to_string(), p.get_kernel_tag());
        Self::add_bool(
            self,
            "GPL Compatible:".to_string(),
            p.get_kernel_gpl_compatible(),
        );

        match p.get_kernel_map_ids() {
            Ok(map_ids) => {
                self.0
                    .add_row(vec!["Map IDs:".to_string(), format!("{:?}", map_ids)]);
            }
            Err(e) => {
                warn!("error retrieving Map IDs: {}", e);
                self.0.add_row(vec!["Map IDs:", "None"]);
            }
        };

        Self::add_u32(self, "BTF ID:".to_string(), p.get_kernel_btf_id());
        Self::add_u32(
            self,
            "Size Translated (bytes):".to_string(),
            p.get_kernel_bytes_xlated(),
        );
        Self::add_bool(self, "JITted:".to_string(), p.get_kernel_jited());
        Self::add_u32(self, "Size JITted:".to_string(), p.get_kernel_bytes_jited());
        Self::add_u32(
            self,
            "Kernel Allocated Memory (bytes):".to_string(),
            p.get_kernel_bytes_memlock(),
        );
        Self::add_u32(
            self,
            "Verified Instruction Count:".to_string(),
            p.get_kernel_verified_insns(),
        );
    }

    pub(crate) fn new_kernel_info(program: &Program) -> Result<Self, anyhow::Error> {
        let mut table = Self::create_kernel_info_table();

        table.add_kernel_info(program);

        Ok(table)
    }

    pub(crate) fn new_program_list() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec![
            "Program ID",
            "Application",
            "Type",
            "Function Name",
            "Links",
        ]);
        ProgTable(table)
    }

    pub(crate) fn add_program_row_list(
        &mut self,
        prog_id: String,
        application: String,
        type_: String,
        fn_name: String,
        links: String,
    ) {
        self.0
            .add_row(vec![prog_id, application, type_, fn_name, links]);
    }

    pub(crate) fn add_program_response(&mut self, r: Program) -> anyhow::Result<()> {
        let data = r.get_data();

        // Build up the list of links string with a count and limit the number links in the list
        let mut link_ids = data.get_link_ids().unwrap_or_default();
        let count = link_ids.len();
        link_ids.truncate(NUM_LIST_LINKS);
        let link_list = link_ids
            .into_iter()
            .map(|m| m.to_string())
            .collect::<Vec<String>>()
            .join(", ");
        let mut truncate_str = String::new();
        if count > NUM_LIST_LINKS {
            truncate_str = ", ...".to_string();
        }
        let links = if count == 0 {
            "".to_string()
        } else {
            format! {"({count}) {link_list}{truncate_str}"}
        };

        let prog_id = match data.get_id() {
            Ok(id) => id.to_string(),
            Err(_) => "None".to_string(),
        };
        let fn_name = match data.get_kernel_name() {
            Ok(name) => name,
            Err(_) => "None".to_string(),
        };

        self.add_program_row_list(
            prog_id,
            Self::get_program_application(data),
            Self::get_type_str(&r),
            fn_name,
            links,
        );

        Ok(())
    }

    pub(crate) fn new_link_list() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec![
            "Program ID",
            "Link ID",
            "Application",
            "Type",
            "Function Name",
            "Attachment",
        ]);
        ProgTable(table)
    }

    pub(crate) fn add_link_row_list(
        &mut self,
        prog_id: String,
        link_id: String,
        application: String,
        type_: String,
        fn_name: String,
        attachments: String,
    ) {
        self.0.add_row(vec![
            prog_id,
            link_id,
            application,
            type_,
            fn_name,
            attachments,
        ]);
    }

    pub(crate) fn add_link_response(
        &mut self,
        program: &Program,
        link: &Link,
    ) -> anyhow::Result<()> {
        let data = program.get_data();

        let prog_id = match data.get_id() {
            Ok(id) => id.to_string(),
            Err(_) => "None".to_string(),
        };
        let link_id = match link.get_id() {
            Ok(id) => id.to_string(),
            Err(_) => "None".to_string(),
        };
        let fn_name = match data.get_kernel_name() {
            Ok(name) => name,
            Err(_) => "None".to_string(),
        };

        self.add_link_row_list(
            prog_id,
            link_id,
            Self::get_link_application(link),
            Self::get_type_str(program),
            fn_name,
            Self::get_attach_str(program, link),
        );

        Ok(())
    }

    pub(crate) fn print(&self) {
        println!("{self}\n")
    }

    fn get_type_str(program: &Program) -> String {
        match program {
            Program::Fentry(_program) => "fentry".to_string(),
            Program::Fexit(_program) => "fexit".to_string(),
            Program::Kprobe(_program) => "kprobe".to_string(),
            Program::Tc(_program) => "tc".to_string(),
            Program::Tcx(_program) => "tcx".to_string(),
            Program::Tracepoint(_program) => "tracepoint".to_string(),
            Program::Uprobe(_program) => "uprobe".to_string(),
            Program::Xdp(_program) => "xdp".to_string(),
            _ => program.kind().to_string(),
        }
    }

    fn get_attach_str(program: &Program, link: &Link) -> String {
        match link {
            Link::Fentry(_fentry_link) => match program {
                Program::Fentry(fentry_program) => match fentry_program.get_fn_name() {
                    Ok(fn_name) => fn_name,
                    Err(_) => "unknown".to_string(),
                },
                _ => "unknown".to_string(),
            },
            Link::Fexit(_fexit_link) => match program {
                Program::Fexit(fexit_program) => match fexit_program.get_fn_name() {
                    Ok(fn_name) => fn_name,
                    Err(_) => "unknown".to_string(),
                },
                _ => "unknown".to_string(),
            },
            Link::Kprobe(kprobe_link) => match kprobe_link.get_fn_name() {
                Ok(fn_name) => fn_name,
                Err(_) => "unknown".to_string(),
            },
            Link::Tc(tc_link) => {
                let iface = match tc_link.get_iface() {
                    Ok(iface) => iface,
                    Err(_) => "unknown".to_string(),
                };
                let dir = match tc_link.get_direction() {
                    Ok(d) => d.to_string(),
                    Err(_) => "unknown".to_string(),
                };
                let position = match tc_link.get_current_position() {
                    Ok(pos) => match pos {
                        Some(p) => p.to_string(),
                        None => "unknown".to_string(),
                    },
                    Err(_) => "unknown".to_string(),
                };
                format! {"{} {} pos-{}", iface, dir, position}
            }
            Link::Tcx(tcx_link) => {
                let iface = match tcx_link.get_iface() {
                    Ok(iface) => iface,
                    Err(_) => "unknown".to_string(),
                };
                let dir = match tcx_link.get_direction() {
                    Ok(d) => d.to_string(),
                    Err(_) => "unknown".to_string(),
                };
                let position = match tcx_link.get_current_position() {
                    Ok(pos) => match pos {
                        Some(p) => p.to_string(),
                        None => "unknown".to_string(),
                    },
                    Err(_) => "unknown".to_string(),
                };
                format! {"{} {} pos-{}", iface, dir, position}
            }
            Link::Tracepoint(tracepoint_link) => match tracepoint_link.get_tracepoint() {
                Ok(tracepoint) => tracepoint,
                Err(_) => "unknown".to_string(),
            },
            Link::Uprobe(uprobe_link) => {
                let target = match uprobe_link.get_target() {
                    Ok(target) => target,
                    Err(_) => "unknown".to_string(),
                };
                match uprobe_link.get_fn_name() {
                    Ok(fn_name) => match fn_name {
                        Some(name) => format! {"{} {}", target, name},
                        None => target,
                    },
                    Err(_) => target,
                }
            }
            Link::Xdp(xdp_link) => {
                let iface = match xdp_link.get_iface() {
                    Ok(iface) => iface,
                    Err(_) => "unknown".to_string(),
                };
                let position = match xdp_link.get_current_position() {
                    Ok(pos) => match pos {
                        Some(p) => p.to_string(),
                        None => "unknown".to_string(),
                    },
                    Err(_) => "unknown".to_string(),
                };
                format! {"{} pos-{}", iface, position}
            }
        }
    }

    fn get_program_application(data: &ProgramData) -> String {
        match data.get_application_from_metadata() {
            Some(application) => application,
            None => "".to_string(),
        }
    }

    fn get_link_application(link: &Link) -> String {
        match link.get_application_from_metadata() {
            Some(application) => application,
            None => "".to_string(),
        }
    }

    fn add_bool(&mut self, tag: String, value: Result<bool, BpfmanError>) {
        match value {
            Ok(v) => {
                self.0.add_row(vec![tag, v.to_string()]);
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_i32(&mut self, tag: String, value: Result<i32, BpfmanError>) {
        match value {
            Ok(v) => {
                self.0.add_row(vec![tag, v.to_string()]);
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_u32(&mut self, tag: String, value: Result<u32, BpfmanError>) {
        match value {
            Ok(v) => {
                self.0.add_row(vec![tag, v.to_string()]);
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_option_usize(&mut self, tag: String, value: Result<Option<usize>, BpfmanError>) {
        match value {
            Ok(val) => {
                match val {
                    Some(v) => {
                        self.0.add_row(vec![tag, v.to_string()]);
                    }
                    None => {
                        self.0.add_row(vec![tag, "None".to_string()]);
                    }
                };
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_u64(&mut self, tag: String, value: Result<u64, BpfmanError>) {
        match value {
            Ok(v) => {
                self.0.add_row(vec![tag, v.to_string()]);
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_string(&mut self, tag: String, value: Result<String, BpfmanError>) {
        match value {
            Ok(v) => {
                if v.is_empty() {
                    self.0.add_row(vec![tag, "None".to_string()]);
                } else {
                    self.0.add_row(vec![tag, v]);
                }
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_option_string(&mut self, tag: String, value: Result<Option<String>, BpfmanError>) {
        match value {
            Ok(option_str) => {
                match option_str {
                    Some(v) => {
                        self.0.add_row(vec![tag, v]);
                    }
                    None => {
                        self.0.add_row(vec![tag, "None".to_string()]);
                    }
                };
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_option_pathbuf(&mut self, tag: String, value: Result<Option<PathBuf>, BpfmanError>) {
        match value {
            Ok(val) => {
                match val {
                    Some(v) => {
                        self.0.add_row(vec![tag, format!("{}", v.display())]);
                    }
                    None => {
                        self.0.add_row(vec![tag, "None".to_string()]);
                    }
                };
            }
            Err(e) => {
                warn!("error retrieving {} {}", tag, e);
                self.0.add_row(vec![tag, "None".to_string()]);
            }
        };
    }

    fn add_container_pid(&mut self, container_pid: Result<Option<i32>, BpfmanError>) {
        match container_pid {
            Ok(pid) => {
                match pid {
                    Some(p) => {
                        self.0.add_row(vec!["Container PID:", &p.to_string()]);
                    }
                    None => {
                        self.0.add_row(vec!["Container PID:", "None"]);
                    }
                };
            }
            Err(e) => {
                warn!("error retrieving Container PID: {}", e);
                self.0.add_row(vec!["Container PID:", "None"]);
            }
        };
    }

    fn add_metadata(&mut self, metadata: Result<HashMap<String, String>, BpfmanError>) {
        match metadata {
            Ok(md) => {
                if md.is_empty() {
                    self.0.add_row(vec!["Metadata:", "None"]);
                } else {
                    let mut first = true;
                    for (key, value) in md.clone() {
                        let data = &format! {"{key}={value}"};
                        if first {
                            first = false;
                            self.0.add_row(vec!["Metadata:", data]);
                        } else {
                            self.0.add_row(vec!["", data]);
                        }
                    }
                }
            }
            Err(e) => {
                warn!("error retrieving Metadata: {}", e);
                self.0.add_row(vec!["Metadata:", "None"]);
            }
        };
    }
}

impl std::fmt::Display for ProgTable {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

pub fn sqlite_print_program_list(programs: &[BpfProgram]) -> anyhow::Result<()> {
    let mut table = ProgTable::new_program_list();

    for p in programs {
        let prog_id = p.id.to_string();
        let application = extract_application(p.metadata.as_deref());
        let kind = p.kind.clone();
        let fn_name = p.kernel_name.clone().unwrap_or_else(|| "None".into());

        // No links yet, so this remains blank
        let links = "".into();

        table.add_program_row_list(prog_id, application, kind, fn_name, links);
    }

    table.print();
    Ok(())
}

pub fn sqlite_print_program_detail(p: &BpfProgram, maps: &[BpfMap]) -> anyhow::Result<()> {
    let mut table = ProgTable::create_bpfman_state_table();

    table.add_string("BPF Function:".into(), Ok(p.name.clone()));
    table.0.add_row(vec!["Program Type:", &p.kind]);

    match p.location_type.as_str() {
        "file" => {
            table.add_string(
                "Path:".into(),
                Ok(p.file_path.clone().unwrap_or_else(|| "None".to_string())),
            );
        }
        "image" => {
            table.add_string(
                "Path:".into(),
                Ok(p.file_path.clone().unwrap_or_else(|| "None".to_string())),
            );
            table.add_string(
                "Pull Policy:".into(),
                Ok(p.image_pull_policy.clone().unwrap_or_else(|| "None".into())),
            );
        }
        _ => {
            table.0.add_row(vec!["Location:", "Unknown"]);
        }
    }

    match &p.global_data {
        Some(s) => table.add_string("Global:".into(), Ok(s.clone())),
        None => table.add_string("Global:".into(), Ok("None".into())),
    }

    match &p.metadata {
        Some(s) => table.add_string("Metadata:".into(), Ok(s.clone())),
        None => table.add_string("Metadata:".into(), Ok("None".into())),
    }

    table.add_string("Map Pin Path:".into(), Ok(p.map_pin_path.clone()));

    match p.map_owner_id {
        Some(id) => table.add_string("Map Owner ID:".into(), Ok(id.to_string())),
        None => table.add_string("Map Owner ID:".into(), Ok("None".into())),
    }

    let used_maps: Vec<&BpfMap> = maps.iter().filter(|m| m.name == p.name).collect();
    if used_maps.is_empty() {
        table.0.add_row(vec!["Maps Used:", "None"]);
    } else {
        let mut first = true;
        for map in used_maps {
            let entry = format!(
                "{} (key={} val={} entries={})",
                map.name, map.key_size, map.value_size, map.max_entries
            );
            if first {
                table.0.add_row(vec!["Maps Used:", &entry]);
                first = false;
            } else {
                table.0.add_row(vec!["", &entry]);
            }
        }
    }

    let mut ktable = ProgTable::create_kernel_info_table();
    ktable.add_string("Program ID:".into(), Ok(p.id.to_string()));
    ktable.add_string(
        "BPF Function:".into(),
        Ok(p.kernel_name.clone().unwrap_or_default()),
    );
    ktable.0.add_row(vec!["Kernel Type:", &p.kind]);
    ktable.add_string(
        "Loaded At:".into(),
        Ok(p.kernel_loaded_at.clone().unwrap_or_default()),
    );
    ktable.add_string("Tag:".into(), Ok(format!("{:#x}", p.kernel_tag.get())));
    ktable.add_bool(
        "GPL Compatible:".into(),
        Ok(p.kernel_gpl_compatible.unwrap_or(false)),
    );
    ktable.add_string(
        "BTF ID:".into(),
        Ok(p.kernel_btf_id.map(|v| v.to_string()).unwrap_or_default()),
    );
    ktable.add_string(
        "Size Translated (bytes):".into(),
        Ok(p.kernel_bytes_xlated
            .map(|v| v.to_string())
            .unwrap_or_default()),
    );
    ktable.add_bool("JITted:".into(), Ok(p.kernel_jited.unwrap_or(false)));
    ktable.add_string(
        "Size JITted:".into(),
        Ok(p.kernel_bytes_jited
            .map(|v| v.to_string())
            .unwrap_or_default()),
    );
    ktable.add_string(
        "Kernel Allocated Memory (bytes):".into(),
        Ok(p.kernel_bytes_memlock
            .map(|v| v.to_string())
            .unwrap_or_default()),
    );
    ktable.add_string(
        "Verified Instruction Count:".into(),
        Ok(p.kernel_verified_insns
            .map(|v| v.to_string())
            .unwrap_or_default()),
    );

    table.print();
    ktable.print();
    Ok(())
}

fn extract_application(metadata: Option<&str>) -> String {
    match metadata.and_then(|s| serde_json::from_str::<serde_json::Value>(s).ok()) {
        Some(serde_json::Value::Object(map)) => map
            .get("application")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        _ => "".into(),
    }
}
