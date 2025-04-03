// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::{Result, bail};
use bpfman::{
    add_programs,
    errors::BpfmanError,
    load_ebpf_programs,
    models::get_program_bytes_and_validate,
    oci_utils::image_manager::ImageManager,
    program_loader::{LoadSpecBuilder, LoadedProgram},
    setup, setup_with_sqlite,
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
        let use_sqlite = std::env::var("BPFMAN_USE_SQLITE")
            .map(|val| val == "1" || val.to_lowercase() == "true")
            .unwrap_or(false);

        match self {
            LoadSubcommand::File(l) => {
                if use_sqlite {
                    sqlite_execute_load_file(l)
                } else {
                    execute_load_file(l)
                }
            }
            LoadSubcommand::Image(l) => {
                if use_sqlite {
                    sqlite_execute_load_image(l)
                } else {
                    execute_load_image(l)
                }
            }
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

enum LoadArgs<'a> {
    File(&'a LoadFileArgs),
    Image(&'a LoadImageArgs),
}

impl LoadArgs<'_> {
    fn get_programs(&self) -> &[(String, Vec<String>)] {
        match self {
            LoadArgs::File(file_args) => &file_args.programs,
            LoadArgs::Image(image_args) => &image_args.programs,
        }
    }

    fn get_global_data(&self) -> Option<Vec<(String, Vec<u8>)>> {
        match self {
            LoadArgs::File(file_args) => file_args.global.as_deref().map(Self::to_key_value_pairs),
            LoadArgs::Image(image_args) => {
                image_args.global.as_deref().map(Self::to_key_value_pairs)
            }
        }
    }

    fn get_metadata(&self) -> Option<Vec<(String, String)>> {
        match self {
            LoadArgs::File(file_args) => file_args.metadata.clone(),
            LoadArgs::Image(image_args) => image_args.metadata.clone(),
        }
    }

    fn get_map_owner_id(&self) -> Option<u32> {
        match self {
            LoadArgs::File(file_args) => file_args.map_owner_id,
            LoadArgs::Image(image_args) => image_args.map_owner_id,
        }
    }

    fn to_key_value_pairs(global: &[GlobalArg]) -> Vec<(String, Vec<u8>)> {
        global
            .iter()
            .map(|arg| (arg.name.clone(), arg.value.clone()))
            .collect()
    }
}

fn handle_load_result(res: Result<Vec<LoadedProgram>, BpfmanError>) -> Result<()> {
    match res {
        Ok(loaded) => {
            println!("Successfully loaded {} program(s):", loaded.len());
            println!(
                "{}",
                serde_json::to_string_pretty(&loaded)
                    .unwrap_or_else(|_| "Failed to serialize loaded programs".to_string())
            );
            Ok(())
        }

        Err(BpfmanError::LoadFailed {
            cause,
            loaded_before_failure,
            unload_failures,
        }) => {
            eprintln!("Kernel load failed: {cause}");
            eprintln!(
                "{} programs were loaded before the failure",
                loaded_before_failure.len()
            );
            if !unload_failures.is_empty() {
                eprintln!("Some of them also failed to unload:");
                for uf in &unload_failures {
                    eprintln!(" - program_id={}, error={}", uf.program_id, uf.error);
                }
            }

            let summary = format!(
                "LoadFailed: {cause}; {} programs loaded before error; unload_failures={:?}",
                loaded_before_failure.len(),
                unload_failures,
            );
            Err(anyhow::anyhow!(summary))
        }

        Err(BpfmanError::LoadError {
            cause,
            loaded,
            unload_failures,
        }) => {
            eprintln!("DB persistence failed: {cause}");
            eprintln!(
                "We had successfully loaded {} programs in the kernel.",
                loaded.len()
            );
            if !unload_failures.is_empty() {
                eprintln!("Unload also failed for some of them:");
                for uf in &unload_failures {
                    eprintln!(" - program_id={}, error={}", uf.program_id, uf.error);
                }
            }

            let summary = format!(
                "DbPersistFailed: {cause}; loaded {} programs; unload_failures={:?}",
                loaded.len(),
                unload_failures
            );
            Err(anyhow::anyhow!(summary))
        }

        Err(other) => {
            eprintln!("Unhandled error: {other}");
            Err(anyhow::Error::new(other))
        }
    }
}

fn sqlite_execute_load_common(source: Location, args: LoadArgs) -> anyhow::Result<()> {
    let (config, mut conn) = setup_with_sqlite()?;

    let mut image_manager = ImageManager::new(
        config.signing().verify_enabled,
        config.signing().allow_unsigned,
    )?;

    let (program_bytes, function_names) =
        get_program_bytes_and_validate(&source, &mut image_manager, args.get_programs())?;

    let load_spec = LoadSpecBuilder::default()
        .bytecode_source(source)
        .function_names(function_names)
        .global_data(args.get_global_data().unwrap_or_default())
        .map_owner_id(args.get_map_owner_id())
        .metadata(args.get_metadata().unwrap_or_default())
        .program_bytes(program_bytes)
        .programs(args.get_programs().to_vec())
        .build()
        .map_err(|e| anyhow::anyhow!("Failed to build LoadSpec: {}", e))?;

    let result = load_ebpf_programs(&mut conn, &load_spec);
    handle_load_result(result)
}

fn sqlite_execute_load_file(args: &LoadFileArgs) -> anyhow::Result<()> {
    let source = Location::File(args.path.clone());
    sqlite_execute_load_common(source, LoadArgs::File(args))
}

fn sqlite_execute_load_image(args: &LoadImageArgs) -> anyhow::Result<()> {
    let source = Location::Image((&args.pull_args).try_into()?);
    sqlite_execute_load_common(source, LoadArgs::Image(args))
}
