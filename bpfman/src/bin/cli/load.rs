// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::collections::HashMap;

use anyhow::{anyhow, bail, Context};
use bpfman::{
    add_programs,
    config::SigningConfig,
    load_ebpf_program,
    models::{get_program_bytes_and_validate, BpfProgram},
    oci_utils::image_manager::ImageManager,
    setup, setup_with_sqlite,
    types::{
        FentryProgram, FexitProgram, KprobeProgram, Location, Program, ProgramData, TcProgram,
        TcxProgram, TracepointProgram, UprobeProgram, XdpProgram,
    },
    LoadedProgram, ProgType,
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

pub struct LoadParams<'a> {
    pub bytecode_source: Location,
    pub program_bytes: &'a [u8],
    pub programs: &'a [(String, Vec<String>)],
    pub global_data: &'a Option<Vec<GlobalArg>>,
    pub metadata: &'a Option<Vec<(String, String)>>,
    pub map_owner_id: Option<u32>,
    pub function_names: &'a [String],
}

impl LoadParams<'_> {
    /// Returns the `global_data` as a `HashMap<String, Vec<u8>>`.
    pub fn global_data_map(&self) -> HashMap<String, Vec<u8>> {
        parse_global(self.global_data)
    }
}

pub fn execute_load_programs(
    mut conn: SqliteConnection,
    load_params: &LoadParams,
) -> anyhow::Result<()> {
    // Compute JSON early to fail fast if serialisation fails.
    let global_data_json = serde_json::to_string(&load_params.global_data_map())
        .context("Failed to serialise global data")?;

    let metadata_json =
        serde_json::to_string(load_params.metadata).context("Failed to serialise metadata")?;

    let mut parsed_programs = Vec::new();

    for (prog_type_str, parts) in load_params.programs {
        let name = parts
            .first()
            .ok_or_else(|| anyhow!("Missing program name for type '{}'", prog_type_str))?;

        if !load_params.function_names.is_empty() && !load_params.function_names.contains(name) {
            bail!("Function '{}' not found in eBPF Image", name);
        }

        let fn_name = if prog_type_str == "fentry" || prog_type_str == "fexit" {
            parts.get(1).map(|s| s.as_str())
        } else {
            None
        };

        let prog_type = ProgType::from_str(prog_type_str, fn_name)?;
        parsed_programs.push((prog_type, name.to_owned()));
    }

    // First phase: Load programs into the kernel via Aya.
    let loaded_programs = load_programs_into_kernel(load_params)?;

    // If transaction fails, ensure we unload loaded programs.
    match conn.immediate_transaction(|conn| {
        persist_programs(
            conn,
            &loaded_programs,
            load_params,
            &global_data_json,
            &metadata_json,
        )
    }) {
        Ok(inserted_programs) => {
            println!(
                "Successfully loaded {} program(s):",
                inserted_programs.len()
            );
            println!(
                "{}",
                serde_json::to_string_pretty(&inserted_programs)
                    .unwrap_or_else(|_| "Failed to serialise inserted programs".to_string())
            );
            Ok(())
        }
        Err(err) => {
            // Transaction failed, unload the loaded programs
            // unload_programs(&loaded_programs);
            eprintln!("Database transaction failed. XXX(frobware) Programs to be unloaded:");
            for program in &loaded_programs {
                eprintln!(
                    "- ID: {}, Name: {}, Type: {}",
                    program.program_info.id(),
                    program.name,
                    program.prog_type
                );
            }
            Err(err.context("Database transaction failed while inserting eBPF programs"))
        }
    }
}

fn sqlite_execute_load_file(args: &LoadFileArgs) -> anyhow::Result<()> {
    let (config, conn) = setup_with_sqlite()?;

    let bytecode_source = Location::File(args.path.clone());

    let mut image_manager = ImageManager::new(
        config.signing().verify_enabled,
        config.signing().allow_unsigned,
    )?;

    let (program_bytes, function_names) =
        get_program_bytes_and_validate(&bytecode_source, &mut image_manager, &args.programs)?;

    execute_load_programs(
        conn,
        &LoadParams {
            bytecode_source,
            program_bytes: &program_bytes,
            programs: &args.programs,
            global_data: &args.global,
            metadata: &args.metadata,
            map_owner_id: args.map_owner_id,
            function_names: &function_names,
        },
    )
}

fn sqlite_execute_load_image(args: &LoadImageArgs) -> anyhow::Result<()> {
    let (_config, conn) = setup_with_sqlite()?;
    let bytecode_source = Location::Image((&args.pull_args).try_into()?);

    let mut image_manager = ImageManager::new(
        SigningConfig::default().verify_enabled,
        SigningConfig::default().allow_unsigned,
    )?;

    let (program_bytes, function_names) =
        get_program_bytes_and_validate(&bytecode_source, &mut image_manager, &args.programs)?;

    execute_load_programs(
        conn,
        &LoadParams {
            bytecode_source,
            program_bytes: &program_bytes,
            programs: &args.programs,
            global_data: &args.global,
            metadata: &args.metadata,
            map_owner_id: args.map_owner_id,
            function_names: &function_names,
        },
    )
}

fn load_programs_into_kernel(params: &LoadParams) -> anyhow::Result<Vec<LoadedProgram>> {
    let mut parsed_programs = Vec::new();

    for (prog_type_str, parts) in params.programs {
        let name = parts
            .first()
            .ok_or_else(|| anyhow!("Missing program name for type '{}'", prog_type_str))?;
        let fn_name = if prog_type_str == "fentry" || prog_type_str == "fexit" {
            parts.get(1).map(|s| s.as_str())
        } else {
            None
        };

        let prog_type = ProgType::from_str(prog_type_str, fn_name)?;
        parsed_programs.push((prog_type, name.to_owned()));
    }

    let mut loader = aya::EbpfLoader::new();
    loader.allow_unsupported_maps();

    let global_data_map = params.global_data_map();
    for (key, value) in global_data_map.iter() {
        loader.set_global(key, value.as_slice(), true);
    }

    let mut ebpf_loader = loader.load(params.program_bytes)?;
    let mut loaded_programs = Vec::new();

    for (prog_type, name) in parsed_programs {
        loaded_programs.push(load_ebpf_program(prog_type, &name, &mut ebpf_loader)?);
    }

    Ok(loaded_programs)
}

fn persist_programs(
    conn: &mut SqliteConnection,
    loaded_programs: &[LoadedProgram],
    params: &LoadParams,
    global_data_json: &str,
    metadata_json: &str,
) -> anyhow::Result<Vec<BpfProgram>> {
    let (location_type, file_path, image_url) = match &params.bytecode_source {
        Location::File(path) => ("file", Some(path.clone()), None),
        Location::Image(image) => ("image", None, Some(image.image_url.clone())),
    };

    let mut inserted_programs = Vec::new();

    for program in loaded_programs {
        let program_info = &program.program_info;

        let bpf_prog = BpfProgram {
            id: program_info.id() as i64,
            name: program.name.clone(),
            kind: program.prog_type.to_string(),
            state: "loaded".to_string(),
            location_type: location_type.to_string(),
            file_path: file_path.clone(),
            image_url: image_url.clone(),
            map_pin_path: program.map_pin_path.clone(),
            map_owner_id: params.map_owner_id.map(|id| id as i32), // XXX(frobware) - generalise uint64blob
            program_bytes: params.program_bytes.to_vec(),
            metadata: metadata_json.to_string(),
            global_data: global_data_json.to_string(),
            retprobe: program.retprobe,
            fn_name: program.fn_name.clone(),

            kernel_name: match program_info.name_as_str() {
                Some(name) => Some(name.to_string()),
                None => Some(format!("program_{}", program_info.id())),
            },

            kernel_program_type: program_info.program_type().ok().map(|pt| pt as i32),

            kernel_loaded_at: program_info
                .loaded_at()
                .map(|time| chrono::DateTime::<chrono::Utc>::from(time).to_rfc3339()),

            kernel_tag: Some(format!("{:016x}", program_info.tag())),
            kernel_gpl_compatible: program_info.gpl_compatible(),
            kernel_btf_id: program_info.btf_id().map(|id| id as i32),
            kernel_bytes_xlated: program_info.size_translated().map(|size| size as i32),
            kernel_jited: Some(program_info.size_jitted() > 0),
            kernel_bytes_jited: Some(program_info.size_jitted() as i32),
            kernel_verified_insns: program_info
                .verified_instruction_count()
                .map(|count| count as i32),
            kernel_map_ids: program_info
                .map_ids()
                .map(|opt| opt.unwrap_or_default()) // Handle `None` case
                .map(|ids| serde_json::to_string(&ids).unwrap_or_else(|_| "[]".to_string()))
                .unwrap_or_else(|e| {
                    log::warn!("Failed to retrieve map IDs: {}", e);
                    "[]".to_string()
                }),
            kernel_bytes_memlock: program_info.memory_locked().ok().map(|size| size as i32),
            ..Default::default()
        };

        inserted_programs.push(BpfProgram::insert_record(conn, bpf_prog)?);
    }

    Ok(inserted_programs)
}
