// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::bail;
use bpfman::{
    add_programs, setup,
    types::{
        FentryProgram, FexitProgram, KprobeProgram, Link, Location, METADATA_APPLICATION_TAG,
        Program, ProgramData, TcProgram, TcxProgram, TracepointProgram, UprobeProgram, XdpProgram,
    },
};
use log::warn;

use crate::{
    args::{GlobalArg, LoadFileArgs, LoadImageArgs, LoadSubcommand},
    table::ProgTable,
};

impl LoadSubcommand {
    pub(crate) fn execute(&self) -> anyhow::Result<()> {
        match self {
            LoadSubcommand::File(l) => execute_load_file(l),
            LoadSubcommand::Image(l) => execute_load_image(l),
        }
    }
}

pub(crate) fn execute_load_file(args: &LoadFileArgs) -> anyhow::Result<()> {
    let (config, root_db) = setup()?;
    let bytecode_source = Location::File(args.path.clone());

    let mut progs = vec![];
    let prog_list = args.programs.clone();
    for (prog_type, parts) in prog_list {
        let name = parts
            .first()
            .ok_or_else(|| anyhow::anyhow!("Missing program name"))?;
        if (prog_type == "fentry" || prog_type == "fexit") && parts.len() != 2 {
            bail!("Missing function name for fentry/fexit program");
        }
        let data = ProgramData::new(
            bytecode_source.clone(),
            name.clone(),
            parse_metadata(&args.metadata, &args.application),
            parse_global(&args.global),
            args.map_owner_id,
        )?;
        // Need to determine the program type here (XDP, TC, etc)
        // This would not be required if we had a generic "load" function
        let prog = match prog_type.as_str() {
            "xdp" => Program::Xdp(XdpProgram::new(data)?),
            "tc" => Program::Tc(TcProgram::new(data)?),
            "tcx" => Program::Tcx(TcxProgram::new(data)?),
            "tracepoint" => Program::Tracepoint(TracepointProgram::new(data)?),
            "kprobe" | "kretprobe" => Program::Kprobe(KprobeProgram::new(data)?),
            "uprobe" | "uretprobe" => Program::Uprobe(UprobeProgram::new(data)?),
            "fentry" => {
                let fn_name = parts.get(1).unwrap().clone();
                Program::Fentry(FentryProgram::new(data, fn_name)?)
            }
            "fexit" => {
                let fn_name = parts.get(1).unwrap().clone();
                Program::Fexit(FexitProgram::new(data, fn_name)?)
            }
            _ => bail!("Unknown program type: {prog_type}"),
        };
        progs.push(prog);
    }
    let programs = add_programs(&config, &root_db, progs)?;

    if programs.len() == 1 {
        let links: Vec<Link> = vec![];
        ProgTable::new_program(&programs[0], links)?.print();
        ProgTable::new_kernel_info(&programs[0])?.print();
    } else {
        let mut table = ProgTable::new_program_list();
        for program in programs {
            if let Err(e) = table.add_program_response(program) {
                bail!(e)
            }
        }
        table.print();
    }
    Ok(())
}

pub(crate) fn execute_load_image(args: &LoadImageArgs) -> anyhow::Result<()> {
    let (config, root_db) = setup()?;
    let bytecode_source = Location::Image((&args.pull_args).try_into()?);
    let mut progs = vec![];
    let prog_list = args.programs.clone();

    for (prog_type, parts) in prog_list {
        let name = parts
            .first()
            .ok_or_else(|| anyhow::anyhow!("Missing program name"))?;
        if (prog_type == "fentry" || prog_type == "fexit") && parts.len() != 2 {
            bail!("Missing function name for fentry/fexit program");
        }
        let data = ProgramData::new(
            bytecode_source.clone(),
            name.clone(),
            parse_metadata(&args.metadata, &args.application),
            parse_global(&args.global),
            args.map_owner_id,
        )?;

        // Need to determine the program type here (XDP, TC, etc)
        // This would not be required if we had a generic "load" function in

        let prog = match prog_type.as_str() {
            "xdp" => Program::Xdp(XdpProgram::new(data)?),
            "tc" => Program::Tc(TcProgram::new(data)?),
            "tcx" => Program::Tcx(TcxProgram::new(data)?),
            "tracepoint" => Program::Tracepoint(TracepointProgram::new(data)?),
            "kprobe" | "kretprobe" => Program::Kprobe(KprobeProgram::new(data)?),
            "uprobe" | "uretprobe" => Program::Uprobe(UprobeProgram::new(data)?),
            "fentry" => {
                let fn_name = parts.get(1).unwrap().clone();
                Program::Fentry(FentryProgram::new(data, fn_name)?)
            }
            "fexit" => {
                let fn_name = parts.get(1).unwrap().clone();
                Program::Fexit(FexitProgram::new(data, fn_name)?)
            }
            _ => bail!("Unknown program type: {prog_type}"),
        };
        progs.push(prog);
    }
    let programs = add_programs(&config, &root_db, progs)?;

    if programs.len() == 1 {
        let links: Vec<Link> = vec![];
        ProgTable::new_program(&programs[0], links)?.print();
        ProgTable::new_kernel_info(&programs[0])?.print();
    } else {
        let mut table = ProgTable::new_program_list();
        for program in programs {
            if let Err(e) = table.add_program_response(program) {
                bail!(e)
            }
        }
        table.print();
    }
    Ok(())
}

pub(crate) fn parse_metadata(
    metadata: &Option<Vec<(String, String)>>,
    application: &Option<String>,
) -> HashMap<String, String> {
    let mut data: HashMap<String, String> = HashMap::new();
    let mut found = false;

    if let Some(metadata) = metadata {
        for (k, v) in metadata {
            if k == METADATA_APPLICATION_TAG {
                found = true;
            }
            data.insert(k.to_string(), v.to_string());
        }
    }
    if let Some(app) = application {
        if found {
            warn!(
                "application entered but {} already in metadata, ignoring application",
                METADATA_APPLICATION_TAG
            );
        } else {
            data.insert(METADATA_APPLICATION_TAG.to_string(), app.to_string());
        }
    }

    data
}

fn parse_global(global: &Option<Vec<GlobalArg>>) -> HashMap<String, Vec<u8>> {
    let mut global_data: HashMap<String, Vec<u8>> = HashMap::new();

    if let Some(global) = global {
        for g in global.iter() {
            global_data.insert(g.name.to_string(), g.value.clone());
        }
    }
    global_data
}
