use std::{collections::HashMap, path::PathBuf};

use anyhow::Result;
use aya::{Ebpf, maps::Map};
use chrono::Utc;
use serde::Serialize;

use crate::{
    BpfmanError, ProgramType, calc_map_pin_path, create_map_pin_path,
    directories::*,
    models::{BpfMap, BpfProgram},
    types::Location,
    utils::should_map_be_pinned,
};

#[derive(Debug, Serialize)]
pub struct EbpfLoadResult {
    pub kind: ProgramType,
    pub program: BpfProgram,
    pub maps: Vec<BpfMap>,
}

pub struct LoadSpec<'a> {
    bytecode_source: Location,
    #[allow(dead_code)] // XXX(frobware) TODO
    function_names: &'a [String],
    global_data_json: String,
    map_owner_id: Option<u32>,
    metadata_json: String,
    program_bytes: &'a [u8],
    programs: Vec<(ProgramType, String)>,
}

impl<'a> LoadSpec<'a> {
    /// Creates a HashMap from the global data tuples.
    ///
    /// This helper method converts the internal tuple representation
    /// of global data (name, value pairs) into a HashMap for easier
    /// access and serialisation.
    ///
    /// # Arguments
    ///
    /// * `global_data` - An optional vector of name-value tuples representing global variables.
    ///
    /// # Returns
    ///
    /// A HashMap with string keys and byte vector values.
    fn create_global_data_map(
        global_data: &Option<Vec<(String, Vec<u8>)>>,
    ) -> HashMap<String, Vec<u8>> {
        let mut global_data_map: HashMap<String, Vec<u8>> = HashMap::new();
        if let Some(globals) = global_data {
            for (name, value) in globals.iter() {
                global_data_map.insert(name.clone(), value.clone());
            }
        }
        global_data_map
    }

    /// Creates a new `LoadSpec`, ensuring all required fields are
    /// valid and precomputing serialized metadata for efficient
    /// program loading.
    ///
    /// # Overview
    ///
    /// `LoadSpec` encapsulates all parameters necessary to load eBPF programs,
    /// including program bytecode, metadata, and function mappings. It validates
    /// input data to prevent invalid configurations before execution.
    ///
    /// # Parameters
    ///
    /// - `bytecode_source`: Identifies the origin of the eBPF bytecode (e.g., a file or container image).
    /// - `function_names`: A list of function names expected to be present in the bytecode.
    /// - `global_data`: Optional key-value pairs representing global variables used by the eBPF programs.
    /// - `map_owner_id`: Optional identifier for ownership tracking of pinned maps.
    /// - `metadata`: Optional key-value metadata to associate with the loaded programs.
    /// - `program_bytes`: The raw eBPF bytecode. **Must not be empty.**
    /// - `programs`: A list of program definitions mapping program types to function names. **Must not be empty.**
    ///
    /// # Returns
    ///
    /// - `Ok(LoadSpec)` if the input data is valid.
    /// - `Err(BpfmanError)` if validation fails.
    ///
    /// # Validation Rules
    ///
    /// - **`program_bytes` must not be empty.**
    /// - **`programs` must contain at least one valid entry.**
    /// - **Each program type must have a valid function name.**
    ///   - `fentry` and `fexit` programs **must** include an associated function name.
    ///   - All other programs **must** specify at least one function.
    /// - **Global data and metadata are pre-validated and serialised to JSON.**
    pub fn new(
        bytecode_source: Location,
        function_names: &'a [String],
        global_data: &'a Option<Vec<(String, Vec<u8>)>>,
        map_owner_id: Option<u32>,
        metadata: &'a Option<Vec<(String, String)>>,
        program_bytes: &'a [u8],
        programs: &'a [(String, Vec<String>)],
    ) -> Result<Self, BpfmanError> {
        if program_bytes.is_empty() {
            return Err(BpfmanError::Error(
                "`program_bytes` cannot be empty".to_string(),
            ));
        }
        if programs.is_empty() {
            return Err(BpfmanError::Error("`programs` cannot be empty".to_string()));
        }

        // Validate and convert program definitions
        let mut validated_programs = Vec::new();
        for (program_type_str, parts) in programs {
            let name = parts.first().ok_or_else(|| {
                BpfmanError::Error(format!("Missing program name for {}", program_type_str))
            })?;

            if matches!(program_type_str.as_str(), "fentry" | "fexit") && parts.len() != 2 {
                return Err(BpfmanError::Error(format!(
                    "Missing function name for {} program",
                    program_type_str
                )));
            }

            let fn_name = if matches!(program_type_str.as_str(), "fentry" | "fexit") {
                parts.get(1).map(|s| s.as_str())
            } else {
                None
            };

            let program_type = ProgramType::from_str(program_type_str, fn_name)
                .map_err(|e| BpfmanError::Error(format!("Invalid program type: {}", e)))?;

            validated_programs.push((program_type, name.clone()));
        }

        let global_data_json = serde_json::to_string(&Self::create_global_data_map(global_data))
            .map_err(|e| BpfmanError::Error(format!("Failed to serialize global data: {}", e)))?;

        let metadata_json = serde_json::to_string(metadata)
            .map_err(|e| BpfmanError::Error(format!("Failed to serialize metadata: {}", e)))?;

        Ok(LoadSpec {
            bytecode_source,
            function_names,
            global_data_json,
            map_owner_id,
            metadata_json,
            program_bytes,
            programs: validated_programs,
        })
    }
}

fn build_bpfmap_from_aya_map(
    data: &aya::maps::Map,
    map_name: &str,
) -> Result<BpfMap, aya::maps::MapError> {
    let map_info = match data {
        Map::Array(data)
        | Map::BloomFilter(data)
        | Map::CpuMap(data)
        | Map::DevMap(data)
        | Map::DevMapHash(data)
        | Map::HashMap(data)
        | Map::LpmTrie(data)
        | Map::LruHashMap(data)
        | Map::PerCpuArray(data)
        | Map::PerCpuHashMap(data)
        | Map::PerCpuLruHashMap(data)
        | Map::PerfEventArray(data)
        | Map::ProgramArray(data)
        | Map::Queue(data)
        | Map::RingBuf(data)
        | Map::SockHash(data)
        | Map::SockMap(data)
        | Map::Stack(data)
        | Map::StackTraceMap(data)
        | Map::XskMap(data) => data.info()?,

        Map::Unsupported(_) => {
            return Err(aya::maps::MapError::Unsupported {
                map_type: 0, // XXX(frobware) - is this OK?
            });
        }
    };

    Ok(BpfMap {
        id: map_info.id().into(),
        name: map_name.to_string(),
        map_type: Some(format!("{:?}", map_info.map_type()?)),
        key_size: map_info.key_size().into(),
        value_size: map_info.value_size().into(),
        max_entries: map_info.max_entries().into(),
        created_at: Utc::now().naive_utc(),
        updated_at: Utc::now().naive_utc(),
    })
}

fn build_bpfprogram_from_aya_program(
    prog_info: &aya::programs::ProgramInfo,
    program_type: &ProgramType,
    name: &str,
    spec: &LoadSpec,
    map_pin_path_str: &str,
) -> Result<BpfProgram, BpfmanError> {
    let (location_type, file_path, image_url) = match &spec.bytecode_source {
        Location::File(path) => ("file", Some(path.clone()), None),
        Location::Image(image) => ("image", None, Some(image.image_url.clone())),
    };

    if location_type == "file" && file_path.is_none() {
        return Err(BpfmanError::Error(
            "File-based program requires a file path".to_string(),
        ));
    }
    if location_type == "image" && image_url.is_none() {
        return Err(BpfmanError::Error(
            "Image-based program requires an image URL".to_string(),
        ));
    }

    let kernel_name = prog_info
        .name_as_str()
        .map(|n| n.to_string())
        .or_else(|| Some(format!("prog_{}", prog_info.id())));

    let kernel_loaded_at = prog_info
        .loaded_at()
        .map(|t| chrono::DateTime::<chrono::Utc>::from(t).to_rfc3339());

    Ok(BpfProgram {
        id: prog_info.id().into(),
        name: name.to_owned(),
        kind: program_type.to_string(),
        state: "loaded".to_string(),
        location_type: location_type.to_string(),
        file_path,
        image_url,
        image_pull_policy: None, // Not handled in LoadSpec yet
        username: None,          // Not handled in LoadSpec yet
        password: None,          // Not handled in LoadSpec yet
        map_pin_path: map_pin_path_str.to_string(),
        map_owner_id: spec.map_owner_id.map(|id| id as i32), // XXX(frobware) - generalise uint64blob
        program_bytes: spec.program_bytes.to_vec(),
        metadata: spec.metadata_json.clone(),
        global_data: spec.global_data_json.clone(),
        retprobe: program_type.is_retprobe(),
        fn_name: program_type.fn_name().map(String::from),
        kernel_name,
        kernel_program_type: prog_info.program_type().ok().map(|pt| pt as i32),
        kernel_loaded_at,
        kernel_tag: Some(format!("{:016x}", prog_info.tag())),
        kernel_gpl_compatible: prog_info.gpl_compatible(),
        kernel_btf_id: prog_info.btf_id().map(|id| id as i32),
        kernel_bytes_xlated: prog_info.size_translated().map(|s| s as i32),
        kernel_jited: Some(prog_info.size_jitted() > 0),
        kernel_bytes_jited: Some(prog_info.size_jitted() as i32),
        kernel_verified_insns: prog_info.verified_instruction_count().map(|c| c as i32),
        kernel_bytes_memlock: prog_info.memory_locked().ok().map(|m| m as i32),
        created_at: Utc::now().naive_utc(),
        updated_at: Utc::now().naive_utc(),
    })
}

/// Converts a generic `aya::programs::Program` into its specific type
/// and loads it into the kernel.
///
/// This function does not handle pinning or metadata extraction.
///
/// # Arguments
///
/// - `program_type`: The expected type of the eBPF program.
/// - `ebpf_program`: A mutable reference to the program retrieved from `program_mut()`.
///
/// # Returns
///
/// - `Ok(())` if the program is successfully converted and loaded.
/// - `Err(BpfmanError)` if conversion or loading fails.
fn load_program(
    program_type: &ProgramType,
    ebpf_program: &mut aya::programs::Program,
) -> Result<(), BpfmanError> {
    match program_type {
        ProgramType::Xdp => {
            let prog: &mut aya::programs::Xdp = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Tc | ProgramType::Tcx => {
            let prog: &mut aya::programs::SchedClassifier = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Tracepoint => {
            let prog: &mut aya::programs::TracePoint = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Kprobe | ProgramType::Kretprobe => {
            let prog: &mut aya::programs::KProbe = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Uprobe | ProgramType::Uretprobe => {
            let prog: &mut aya::programs::UProbe = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Fentry(fn_name) => {
            let btf = aya::Btf::from_sys_fs().map_err(BpfmanError::BtfError)?;
            let prog: &mut aya::programs::FEntry = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load(fn_name, &btf)
                .map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Fexit(fn_name) => {
            let btf = aya::Btf::from_sys_fs().map_err(BpfmanError::BtfError)?;
            let prog: &mut aya::programs::FExit = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load(fn_name, &btf)
                .map_err(BpfmanError::BpfProgramError)?;
        }
    };

    Ok(())
}

/// Loads an individual eBPF program into the kernel and returns
/// associated metadata.
///
/// This function retrieves the eBPF program from the loaded bytecode,
/// applies necessary configurations, and loads it into the kernel. It
/// also gathers metadata such as pinned maps and program information.
///
/// # Arguments
///
/// - `program_type`: The type of eBPF program being loaded (e.g., `Xdp`, `Kprobe`).
/// - `name`: The name of the function within the bytecode.
/// - `program_bytecode`: A mutable reference to an `Ebpf` instance containing loaded bytecode.
///
/// # Returns
///
/// - `Ok(EbpfLoadResult)` containing metadata about the loaded program.
/// - `Err(BpfmanError)` if program retrieval or kernel loading fails.
///
/// # Errors
///
/// - If the function name does not exist in the loaded bytecode.
/// - If the program fails to convert into its expected type.
/// - If the kernel rejects the program.
fn load_program_into_kernel(
    program_type: &ProgramType,
    name: &str,
    program_bytecode: &mut Ebpf,
    spec: &LoadSpec,
) -> Result<EbpfLoadResult, BpfmanError> {
    // Retrieve the raw program by name.
    let ebpf_program = program_bytecode
        .program_mut(name)
        .ok_or_else(|| BpfmanError::BpfFunctionNameNotValid(name.to_string()))?;

    load_program(program_type, ebpf_program)?;

    // Retrieve the program info after loading.
    let prog_info = ebpf_program.info().map_err(BpfmanError::BpfProgramError)?;
    let id = prog_info.id();

    // Pin the program.
    let program_pin_path = format!("{RTDIR_FS}/prog_{id}");
    ebpf_program
        .pin(&program_pin_path)
        .map_err(BpfmanError::UnableToPinProgram)?;

    // Calculate the map pin path and create the directory.
    let map_pin_path: PathBuf = calc_map_pin_path(id);
    create_map_pin_path(&map_pin_path)?;

    // Collect map metadata.
    let mut maps = Vec::new();
    for (map_name, map) in program_bytecode.maps_mut() {
        if !should_map_be_pinned(map_name) {
            continue;
        }

        // Pin the map.
        let map_fs_path = map_pin_path.join(map_name);
        map.pin(map_fs_path.clone())
            .map_err(BpfmanError::UnableToPinMap)?;

        maps.push(build_bpfmap_from_aya_map(map, map_name).map_err(BpfmanError::BpfMapInfoError)?);
    }

    let map_pin_path_str = map_pin_path.to_string_lossy().to_string();
    let bpf_prog =
        build_bpfprogram_from_aya_program(&prog_info, program_type, name, spec, &map_pin_path_str);

    Ok(EbpfLoadResult {
        kind: program_type.clone(),
        program: bpf_prog?,
        maps,
    })
}

/// Loads eBPF programs from a `LoadSpec`, parsing bytecode and registering programs.
///
/// This function takes a `LoadSpec`, which contains information about the bytecode source,
/// function mappings, metadata, and other parameters. It performs the following steps:
///
/// 1. Creates an `EbpfLoader` to parse and prepare the bytecode.
/// 2. Loads the bytecode into an `Ebpf` instance, representing the set of parsed eBPF programs.
/// 3. Iterates over the program definitions in the spec, extracting the function names and types.
/// 4. Calls `load_into_kernel` to load each program into the kernel and collect metadata.
/// 5. Returns a vector of `EbpfLoadResult`, containing details about the loaded programs.
///
/// # Arguments
///
/// * `params` - A `LoadSpec` containing eBPF program definitions, bytecode, and metadata.
///
/// # Returns
///
/// * `Ok(Vec<EbpfLoadResult>)` - A list of successfully loaded eBPF programs.
/// * `Err(BpfmanError)` - If loading the bytecode, parsing programs, or kernel loading fails.
///
/// # Errors
///
/// This function may return an error in the following cases:
/// - The bytecode fails to load (e.g., invalid format, missing sections).
/// - A program definition is invalid (e.g., missing function name).
/// - An attempt to load a program into the kernel fails.
pub(crate) fn load_from_spec(spec: &LoadSpec) -> Result<Vec<EbpfLoadResult>, BpfmanError> {
    let mut bytecode_loader = aya::EbpfLoader::new();
    bytecode_loader.allow_unsupported_maps();

    let mut program_bytecode = bytecode_loader
        .load(spec.program_bytes)
        .map_err(BpfmanError::BpfLoadError)?;

    let mut loaded_programs = Vec::new();

    for (program_type, name) in spec.programs.iter() {
        loaded_programs.push(load_program_into_kernel(
            program_type,
            name,
            &mut program_bytecode,
            spec,
        )?);
    }

    Ok(loaded_programs)
}
