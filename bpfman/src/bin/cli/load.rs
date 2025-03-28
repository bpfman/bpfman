// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::{Result, bail};
use bpfman::{
    add_programs,
    errors::BpfmanError,
    load_ebpf_programs,
    models::{BpfMap, BpfProgram, get_program_bytes_and_validate},
    oci_utils::image_manager::ImageManager,
    program_loader::{EbpfLoadResult, LoadSpec},
    setup, setup_with_sqlite,
    types::{
        FentryProgram, FexitProgram, KprobeProgram, Location, Program, ProgramData, TcProgram,
        TcxProgram, TracepointProgram, UprobeProgram, XdpProgram,
    },
};
use diesel::SqliteConnection;

use crate::{
    args::{GlobalArg, LoadFileArgs, LoadImageArgs, LoadSubcommand},
    table::ProgTable,
};

impl LoadSubcommand {
    pub(crate) fn execute(&self) -> anyhow::Result<()> {
        let use_sqlite = std::env::var("USE_SQLITE")
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
            args.metadata
                .clone()
                .unwrap_or_default()
                .iter()
                .map(|(k, v)| (k.to_owned(), v.to_owned()))
                .collect(),
            parse_global(&args.global),
            args.map_owner_id,
        )?;
        // Need to determin the program type here (XDP, TC, etc)
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
    for program in programs {
        ProgTable::new_program(&program)?.print();
        ProgTable::new_kernel_info(&program)?.print();
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
            args.metadata
                .clone()
                .unwrap_or_default()
                .iter()
                .map(|(k, v)| (k.to_owned(), v.to_owned()))
                .collect(),
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
    for program in programs {
        ProgTable::new_program(&program)?.print();
        ProgTable::new_kernel_info(&program)?.print();
    }

    Ok(())
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

/// Converts GlobalArg to the tuple format (String, Vec<u8>) needed by
/// LoadSpec.
fn convert_globals_to_tuples(globals: &Option<Vec<GlobalArg>>) -> Option<Vec<(String, Vec<u8>)>> {
    globals.as_ref().map(|args| {
        args.iter()
            .map(|arg| (arg.name.clone(), arg.value.clone()))
            .collect::<Vec<_>>()
    })
}

fn sqlite_execute_load_file(args: &LoadFileArgs) -> anyhow::Result<()> {
    let (config, mut conn) = setup_with_sqlite()?;
    let bytecode_source = Location::File(args.path.clone());

    let mut image_manager = ImageManager::new(
        config.signing().verify_enabled,
        config.signing().allow_unsigned,
    )?;

    let (program_bytes, function_names) =
        get_program_bytes_and_validate(&bytecode_source, &mut image_manager, &args.programs)?;

    let global_data_tuples = convert_globals_to_tuples(&args.global);

    let load_spec = LoadSpec::new(
        bytecode_source,
        &function_names,
        &global_data_tuples,
        args.map_owner_id,
        &args.metadata,
        &program_bytes,
        &args.programs,
    )?;

    save_and_report_programs(&mut conn, &load_ebpf_programs(&load_spec)?)
}

fn sqlite_execute_load_image(args: &LoadImageArgs) -> anyhow::Result<()> {
    let (config, mut conn) = setup_with_sqlite()?;
    let bytecode_source = Location::Image((&args.pull_args).try_into()?);

    let mut image_manager = ImageManager::new(
        config.signing().verify_enabled,
        config.signing().allow_unsigned,
    )?;

    let (program_bytes, function_names) =
        get_program_bytes_and_validate(&bytecode_source, &mut image_manager, &args.programs)?;

    let global_data_tuples = convert_globals_to_tuples(&args.global);

    let load_spec = LoadSpec::new(
        bytecode_source,
        &function_names,
        &global_data_tuples,
        args.map_owner_id,
        &args.metadata,
        &program_bytes,
        &args.programs,
    )?;

    save_and_report_programs(&mut conn, &load_ebpf_programs(&load_spec)?)
}

pub fn save_ebpf_load_result(
    conn: &mut SqliteConnection,
    loaded_programs: &[EbpfLoadResult],
) -> Result<(), BpfmanError> {
    conn.immediate_transaction::<_, diesel::result::Error, _>(|conn| {
        for loaded in loaded_programs {
            BpfProgram::insert_record(conn, &loaded.program)?;
            for map in &loaded.maps {
                BpfMap::insert_record_on_conflict_do_nothing(conn, map)?;
            }
        }
        Ok(())
    })
    .map_err(|e| BpfmanError::DatabaseError("Transaction failed".into(), e.to_string()))
}

/// Saves eBPF load results to the database and handles error
/// reporting.
///
/// This function attempts to save the loaded programs to the
/// database. On success, it prints information about the loaded
/// programs. On failure, it logs which programs need to be unloaded
/// and returns an appropriate error.
///
/// # Arguments
///
/// * `conn` - A mutable reference to the SQLite connection
/// * `loaded_programs` - A slice of EbpfLoadResult to save
///
/// # Returns
///
/// * `Ok(())` on success
/// * `Err(anyhow::Error)` with context on failure
fn save_and_report_programs(
    conn: &mut SqliteConnection,
    loaded_programs: &[EbpfLoadResult],
) -> anyhow::Result<()> {
    if let Err(err) = save_ebpf_load_result(conn, loaded_programs) {
        eprintln!("Database transaction failed. Programs that need to be unloaded:");
        for program in loaded_programs {
            eprintln!(
                "- ID: {}, Name: {}, Type: {}, Kernel Tag: {}",
                program.program.id,
                program.program.name,
                program.kind,
                program.program.kernel_tag.as_deref().unwrap_or("unknown")
            );
        }

        let error_msg = format!(
            "Database transaction failed while inserting eBPF programs: {}. \
             This likely means one of the {} program(s) couldn't be properly inserted. \
             Check database constraints and ensure all required fields are populated.",
            err,
            loaded_programs.len()
        );

        return Err(anyhow::anyhow!(error_msg));
    }

    println!("Successfully loaded {} program(s):", loaded_programs.len());
    println!(
        "{}",
        serde_json::to_string_pretty(loaded_programs)
            .unwrap_or_else(|_| "Failed to serialize loaded programs".to_string())
    );
    Ok(())
}
