// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! # Program Loader Module
//!
//! This module is responsible for loading and unloading eBPF programs
//! into and out of the kernel. It handles the parsing of eBPF
//! bytecode, loading of individual programs (along with any
//! associated maps), and, if a load error occurs, performing a
//! best-effort rollback by unloading any programs that were already
//! loaded. This module uses the [aya] library for interfacing with
//! the kernel.
//!
//! ## Separation of Concerns
//!
//! Note: This module focuses solely on kernel-level operations
//! (loading/unloading eBPF programs). It does not deal with database
//! (DB) persistence. Database-related actions are handled by a higher
//! layer (e.g., the public API function that wraps these kernel
//! operations in a DB transaction). In the event of a DB persistence
//! failure, the higher layer will use the unload functionality
//! provided here to roll back the kernel state.
//!
//! [aya]: https://github.com/aya-rs/aya
use std::{collections::HashMap, path::PathBuf};

use aya::{Ebpf, maps::Map};
use chrono::Utc;
use derive_builder::Builder;
use serde::Serialize;
use serde_json;
use thiserror::Error;

use crate::{
    BpfmanError, calc_map_pin_path, create_map_pin_path,
    db::{BpfMap, BpfProgram, KernelU32, U64Blob},
    directories::*,
    init_image_manager,
    types::{Location, ProgramType},
    utils::should_map_be_pinned,
};

/// The `LoadSpec` struct defines the parameters required for loading eBPF
/// programs.
///
/// This struct is used to configure the details for loading eBPF programs,
/// including program bytecode, function names, global data, metadata, and
/// other related parameters.
///
/// It uses a **builder pattern** to construct instances of `LoadSpec`,
/// allowing for flexible and incremental configuration of the struct's
/// fields.
///
/// ```rust
/// use bpfman::program_loader::LoadSpecBuilder;
/// use bpfman::types::Location;
///
/// let load_spec = LoadSpecBuilder::default()
///     .bytecode_source(Location::File("path/to/program.o".to_string()))
///     .global_data(vec![("key1".to_string(), b"value1".to_vec())])
///     .build();
/// ```
///
/// # Notes
/// - The `global_data` and `metadata` fields are also optional and will
///   default to `None` if not provided. These fields are serialized to JSON
///   when the struct is built.
#[derive(Debug, Builder)]
#[builder(pattern = "mutable", build_fn(name = "build_partial"))]
pub struct LoadSpec {
    #[builder(setter(into))]
    bytecode_source: Location,

    #[builder(setter(into))]
    programs: Vec<ProgramType>,

    #[builder(setter(strip_option), default)]
    global_data: Option<Vec<(String, Vec<u8>)>>,

    #[builder(setter(strip_option), default)]
    metadata: Option<Vec<(String, String)>>,

    #[builder(default)]
    map_owner_id: Option<u32>,

    // The following fields are computed in build().
    #[builder(setter(skip))]
    global_data_json: Option<String>,

    #[builder(setter(skip))]
    metadata_json: Option<String>,
}

/// Represents errors encountered during the construction of a
/// `LoadSpec`.
///
/// These errors may occur during the builder's `build()` process,
/// either due to missing required fields or failures during
/// serialisation of data to JSON.
#[derive(Debug, Error)]
pub enum LoadSpecError {
    /// Indicates that one or more required fields were not
    /// initialised before calling `build()`. The inner string
    /// describes the missing fields, as reported by `derive_builder`.
    #[error("failed to build partial LoadSpec: {0}")]
    UninitialisedFields(String),

    /// An error occurred while serialising the global data map to a
    /// JSON string.
    ///
    /// This usually indicates invalid input in the `global_data`
    /// field of the builder. The underlying `serde_json::Error` is
    /// preserved as the source.
    #[error("error serialising global data to JSON")]
    GlobalDataSerialisation(#[from] serde_json::Error),

    /// An error occurred while serialising the metadata map to a JSON
    /// string.
    ///
    /// Unlike `GlobalDataSerialisation`, this wraps the source
    /// explicitly in a named field so that it doesn’t conflict with
    /// the `#[from]` path used earlier.
    #[error("error serialising metadata to JSON")]
    MetadataSerialisation {
        /// The original error returned by `serde_json::to_string`.
        #[source]
        source: serde_json::Error,
    },
}

impl LoadSpecBuilder {
    /// Finalises the [`LoadSpecBuilder`] into a complete [`LoadSpec`]
    /// instance.
    ///
    /// This method performs the following:
    ///
    /// 1. Calls [`Self::build_partial`] to perform structural
    ///    validation and assemble a `LoadSpec`.
    /// 2. Serialises the optional
    ///    [`global_data`](LoadSpecBuilder::global_data) and
    ///    [`metadata`](LoadSpecBuilder::metadata) fields to JSON
    ///    strings for storage.
    ///
    /// # Errors
    ///
    /// Returns a [`LoadSpecError`](crate::program_loader::LoadSpecError) if:
    ///
    /// - Required fields are missing ([`LoadSpecError::UninitialisedFields`]).
    /// - JSON serialisation of global data fails ([`LoadSpecError::GlobalDataSerialisation`]).
    /// - JSON serialisation of metadata fails ([`LoadSpecError::MetadataSerialisation`]).
    ///
    /// # Returns
    ///
    /// A fully constructed
    /// [`LoadSpec`](crate::program_loader::LoadSpec) ready for use in
    /// loading eBPF programs.
    pub fn build(&mut self) -> Result<LoadSpec, LoadSpecError> {
        let mut spec = self
            .build_partial()
            .map_err(|e| LoadSpecError::UninitialisedFields(e.to_string()))?;

        spec.global_data_json = match &spec.global_data {
            Some(data) if !data.is_empty() => {
                Some(serde_json::to_string(&Self::global_data_to_map(data))?)
            }
            _ => None,
        };

        spec.metadata_json = match &spec.metadata {
            Some(data) if !data.is_empty() => {
                Some(serde_json::to_string(&Self::metadata_to_map(data))?)
            }
            _ => None,
        };

        Ok(spec)
    }

    /// Converts a list of `(key, value)` byte arrays into a
    /// [`HashMap<String, Vec<u8>>`](std::collections::HashMap).
    ///
    /// Used to convert [`LoadSpecBuilder::global_data`] into a
    /// serialisable map format before encoding it as JSON.
    fn global_data_to_map(data: &[(String, Vec<u8>)]) -> HashMap<String, Vec<u8>> {
        data.iter().map(|(k, v)| (k.clone(), v.clone())).collect()
    }

    /// Converts a list of `(key, value)` string pairs into a
    /// [`HashMap<String, String>`](std::collections::HashMap).
    ///
    /// Used to convert [`LoadSpecBuilder::metadata`] into a
    /// serialisable map format before encoding it as JSON.
    fn metadata_to_map(data: &[(String, String)]) -> HashMap<String, String> {
        data.iter().map(|(k, v)| (k.clone(), v.clone())).collect()
    }
}

/// Represents a program that was successfully loaded into the kernel,
/// along with any associated pinned maps.
///
/// This structure records the state of an eBPF program at the time of
/// its successful kernel load. Depending on subsequent operations,
/// the program may still be live in the kernel, or it may have been
/// unloaded (rolled back) due to an error in a later phase (such as
/// database persistence).
///
/// In both cases, the information contained in this structure remains
/// accurate and useful as a historical record of the load operation.
/// However, whether the program is currently live or not must be
/// interpreted in the context of subsequent unload operations.
#[derive(Debug, Serialize)]
pub struct LoadedProgram {
    pub kind: ProgramType, // XXX(frobware): do we really need this?
    pub program: BpfProgram,
    pub maps: Vec<BpfMap>,
}

#[derive(Debug, thiserror::Error)]
pub enum UnloadError {
    #[error("Failed to unload program {program_id}: {source}")]
    Failure {
        program_id: KernelU32,
        #[source]
        source: BpfmanError,
    },
}

/// Converts an [`aya::maps::Map`] into a [`BpfMap`] by extracting
/// metadata such as ID, type, key/value sizes, and max entries.
///
/// This is used during program load to persist map metadata in the
/// database for later inspection or bookkeeping.
fn build_bpfmap_from_aya_map(data: &Map, map_name: &str) -> Result<BpfMap, aya::maps::MapError> {
    let info = match data {
        Map::Array(d)
        | Map::BloomFilter(d)
        | Map::CpuMap(d)
        | Map::DevMap(d)
        | Map::DevMapHash(d)
        | Map::HashMap(d)
        | Map::LpmTrie(d)
        | Map::LruHashMap(d)
        | Map::PerCpuArray(d)
        | Map::PerCpuHashMap(d)
        | Map::PerCpuLruHashMap(d)
        | Map::PerfEventArray(d)
        | Map::ProgramArray(d)
        | Map::Queue(d)
        | Map::RingBuf(d)
        | Map::SockHash(d)
        | Map::SockMap(d)
        | Map::Stack(d)
        | Map::StackTraceMap(d)
        | Map::XskMap(d) => d.info()?,
        Map::Unsupported(d) => d.info()?,
    };

    Ok(BpfMap {
        id: info.id().into(),
        name: map_name.to_string(),
        map_type: Some(format!("{:?}", info.map_type()?)),
        key_size: KernelU32::from(info.key_size()),
        value_size: KernelU32::from(info.value_size()),
        max_entries: KernelU32::from(info.max_entries()),
        created_at: Utc::now().naive_utc(),
        updated_at: None,
    })
}

/// Constructs a [`BpfProgram`] from a loaded
/// [`aya::programs::ProgramInfo`] and the corresponding
/// [`ProgramType`] and [`LoadSpec`].
///
/// This extracts both kernel metadata and user-supplied fields (e.g.,
/// global data, metadata, source path) to produce a fully populated
/// `BpfProgram` ready for DB persistence.
fn build_bpfprogram_from_aya_program(
    prog_info: &aya::programs::ProgramInfo,
    program_type: &ProgramType,
    program_bytes: &[u8],
    spec: &LoadSpec,
    map_pin_path_str: &str,
) -> Result<BpfProgram, BpfmanError> {
    let (location_type, file_path, image_url, image_pull_policy, username, password) =
        match &spec.bytecode_source {
            Location::File(path) => ("file", Some(path.clone()), None, None, None, None),
            Location::Image(image) => (
                "image",
                None,
                Some(image.image_url.clone()),
                Some(image.image_pull_policy.to_string()),
                image.username.clone(),
                image.password.clone(),
            ),
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
        .map(|t| chrono::DateTime::<Utc>::from(t).to_rfc3339());

    let prog_name = program_type
        .function_name()
        .ok_or_else(|| BpfmanError::BpfFunctionNameNotValid("<none>".to_string()))?;

    Ok(BpfProgram {
        id: prog_info.id().into(),
        name: prog_name.to_owned(),
        kind: program_type.type_str().to_owned(),
        state: "loaded".to_string(),
        location_type: location_type.to_owned(),
        file_path,
        image_url,
        image_pull_policy,
        username,
        password,
        map_pin_path: map_pin_path_str.to_owned(),
        map_owner_id: spec.map_owner_id.map(KernelU32::from),
        program_bytes: program_bytes.into(),
        metadata: spec.metadata_json.clone(),
        global_data: spec.global_data_json.clone(),
        retprobe: program_type.is_retprobe(),
        fn_name: program_type.function_name().map(String::from),
        kernel_name,
        kernel_program_type: prog_info
            .program_type()
            .ok()
            .map(|pt| KernelU32::from(pt as u32)),
        kernel_loaded_at,
        kernel_tag: U64Blob::from(prog_info.tag()),
        kernel_gpl_compatible: prog_info.gpl_compatible(),
        kernel_btf_id: prog_info.btf_id().map(KernelU32::from),
        kernel_bytes_xlated: prog_info.size_translated().map(KernelU32::from),
        kernel_jited: Some(prog_info.size_jitted() > 0),
        kernel_bytes_jited: Some(KernelU32::from(prog_info.size_jitted())),
        kernel_verified_insns: prog_info.verified_instruction_count().map(KernelU32::from),
        kernel_bytes_memlock: prog_info.memory_locked().ok().map(KernelU32::from),
        created_at: Utc::now().naive_utc(),
        updated_at: None,
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
        ProgramType::Xdp { function_name: _ } => {
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
        ProgramType::Tracepoint { function_name: _ } => {
            let prog: &mut aya::programs::TracePoint = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Kprobe { function_name: _ } | ProgramType::Kretprobe { function_name: _ } => {
            let prog: &mut aya::programs::KProbe = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Uprobe { function_name: _ } | ProgramType::Uretprobe { function_name: _ } => {
            let prog: &mut aya::programs::UProbe = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load().map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Fentry {
            function_name: _,
            attach_function,
        } => {
            let btf = aya::Btf::from_sys_fs().map_err(BpfmanError::BtfError)?;
            let prog: &mut aya::programs::FEntry = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load(attach_function, &btf)
                .map_err(BpfmanError::BpfProgramError)?;
        }
        ProgramType::Fexit {
            function_name: _,
            attach_function,
        } => {
            let btf = aya::Btf::from_sys_fs().map_err(BpfmanError::BtfError)?;
            let prog: &mut aya::programs::FExit = ebpf_program
                .try_into()
                .map_err(BpfmanError::BpfProgramError)?;
            prog.load(attach_function, &btf)
                .map_err(BpfmanError::BpfProgramError)?;
        }
    };

    Ok(())
}

fn attempt_unload(_lp: &LoadedProgram) -> Result<(), BpfmanError> {
    // TODO(frobware). Circle back here when we address the `unload`
    // bpfman command. We may want to go through the public API (i.e.,
    // the front door).

    Err(BpfmanError::InternalError(
        "attempt_unload is not yet implemented".into(),
    ))
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
/// - `program_type`: The type of eBPF program being loaded (e.g.,
///   `Xdp`, `Kprobe`).
/// - `name`: The name of the function within the bytecode.
/// - `program_bytecode`: A mutable reference to an `Ebpf` instance
///   containing loaded bytecode.
///
/// # Returns
///
/// - `Ok(EbpfLoadResult)` containing metadata about the loaded
///   program.
/// - `Err(BpfmanError)` if program retrieval or kernel loading fails.
///
/// # Errors
///
/// - If the function name does not exist in the loaded bytecode.
/// - If the program fails to convert into its expected type.
/// - If the kernel rejects the program.
fn load_program_into_kernel(
    program: &ProgramType,
    program_bytes: &[u8],
    bytecode: &mut Ebpf,
    spec: &LoadSpec,
) -> Result<LoadedProgram, BpfmanError> {
    let prog_name = program
        .function_name()
        .ok_or_else(|| BpfmanError::BpfFunctionNameNotValid("<none>".to_string()))?;

    let ebpf_program = bytecode
        .program_mut(prog_name)
        .ok_or_else(|| BpfmanError::BpfFunctionNameNotValid(prog_name.to_string()))?;

    load_program(program, ebpf_program)?;

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
    for (map_name, map) in bytecode.maps_mut() {
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
    let bpf_prog = build_bpfprogram_from_aya_program(
        &prog_info,
        program,
        program_bytes,
        spec,
        &map_pin_path_str,
    );

    Ok(LoadedProgram {
        kind: program.clone(),
        program: bpf_prog?,
        maps,
    })
}

/// Loads eBPF programs from a `LoadSpec`, parsing bytecode and
/// registering programs.
///
/// This function takes a `LoadSpec`, which contains information about
/// the bytecode source, metadata, and other parameters. It performs
/// the following steps:
///
/// 1. Creates an `EbpfLoader` to parse and prepare the bytecode.
/// 2. Loads the bytecode into an `Ebpf` instance, representing the
///    set of parsed eBPF programs.
/// 3. Iterates over the program definitions in the spec, extracting
///    the function names and types.
/// 4. Calls `load_into_kernel` to load each program into the kernel
///    and collect metadata.
/// 5. Returns a vector of `EbpfLoadResult`, containing details about
///    the loaded programs.
///
/// # Arguments
///
/// * `params` - A `LoadSpec` containing eBPF program definitions,
///   bytecode, and metadata.
///
/// # Returns
///
/// * `Ok(Vec<EbpfLoadResult>)` - A list of successfully loaded eBPF
///   programs.
/// * `Err(BpfmanError)` - If loading the bytecode, parsing programs,
///   or kernel loading fails.
///
/// # Errors
///
/// This function may return an error in the following cases:
/// - The bytecode fails to load (e.g., invalid format, missing
///   sections).
/// - A program definition is invalid (e.g., missing function name).
/// - An attempt to load a program into the kernel fails.
pub(crate) fn load_from_spec(spec: &LoadSpec) -> Result<Vec<LoadedProgram>, BpfmanError> {
    let mut image_manager = init_image_manager()?;

    let (program_bytes, _function_names) = spec
        .bytecode_source
        .get_program_bytes_no_sled(&mut image_manager)?;

    let mut bytecode_loader = aya::EbpfLoader::new();
    bytecode_loader.allow_unsupported_maps();

    let mut program_bytecode = bytecode_loader
        .load(&program_bytes)
        .map_err(BpfmanError::BpfLoadError)?;

    let mut loaded_programs = Vec::new();

    for program in &spec.programs {
        match load_program_into_kernel(program, &program_bytes, &mut program_bytecode, spec) {
            Ok(loaded) => loaded_programs.push(loaded),
            Err(err) => {
                let unload_failures = unload_all(&loaded_programs);

                return Err(BpfmanError::ProgramLoadError {
                    cause: Box::new(err),
                    loaded_before_failure: loaded_programs,
                    unload_failures,
                });
            }
        }
    }

    Ok(loaded_programs)
}

/// Attempts to unload every program in the provided slice.
///
/// For each `LoadedProgram` in `programs`, this function calls
/// `attempt_unload`. If an unload operation fails, it records an
/// `UnloadError` containing the program's unique identifier and a
/// description of the error. The returned vector contains all unload
/// failures; if all programs are successfully unloaded, the vector
/// will be empty.
///
/// # Arguments
///
/// - `programs`: A slice of `LoadedProgram` instances that were
///   previously loaded into the kernel.
///
/// # Returns
///
/// A vector of `UnloadError` instances detailing any failures
/// encountered during the unload process.
pub(crate) fn unload_all(programs: &[LoadedProgram]) -> Vec<UnloadError> {
    programs
        .iter()
        .filter_map(|lp| {
            attempt_unload(lp).err().map(|e| UnloadError::Failure {
                program_id: lp.program.id,
                source: e,
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    mod load_spec {
        use crate::{
            program_loader::LoadSpecBuilder,
            types::{Location, ProgramType},
        };

        fn valid_programs() -> Vec<ProgramType> {
            vec![ProgramType::Tcx]
        }

        #[test]
        fn test_build_fails_with_no_fields() {
            let result = LoadSpecBuilder::default().build();
            assert!(result.is_err());
        }

        #[test]
        fn test_build_fails_without_bytecode_source() {
            let result = LoadSpecBuilder::default()
                .programs(vec![ProgramType::Tcx])
                .build();
            assert!(result.is_err(), "missing bytecode_source should fail");
        }

        #[test]
        fn test_build_fails_without_programs() {
            let result = LoadSpecBuilder::default()
                .bytecode_source(Location::File("some.o".into()))
                .build();
            assert!(result.is_err(), "missing programs should fail");
        }

        #[test]
        fn test_build_global_data_serialises_to_json() {
            let result = LoadSpecBuilder::default()
                .bytecode_source(Location::File("path/to/bytecode".into()))
                .global_data(vec![
                    ("key1".into(), b"value1".to_vec()),
                    ("key2".into(), b"value2".to_vec()),
                ])
                .programs(valid_programs())
                .build();

            assert!(result.is_ok());
            let spec = result.unwrap();

            let json_str = spec
                .global_data_json
                .as_deref()
                .expect("global_data_json should be present");

            let json: serde_json::Value =
                serde_json::from_str(json_str).expect("global_data_json should be valid JSON");

            assert!(json.get("key1").is_some(), "expected key1 in JSON");
            assert!(json.get("key2").is_some(), "expected key2 in JSON");
        }

        #[test]
        fn test_build_metadata_serialises_to_json() {
            let result = LoadSpecBuilder::default()
                .bytecode_source(Location::File("path/to/bytecode".into()))
                .metadata(vec![
                    ("key1".into(), "value1".to_string()),
                    ("key2".into(), "value2".to_string()),
                ])
                .programs(valid_programs())
                .build();

            assert!(result.is_ok());
            let spec = result.unwrap();

            let json_str = spec
                .metadata_json
                .as_deref()
                .expect("metadata_json should be present");

            let json: serde_json::Value =
                serde_json::from_str(json_str).expect("metadata_json should be valid JSON");

            assert!(json.get("key1").is_some(), "expected key1 in JSON");
            assert!(json.get("key2").is_some(), "expected key2 in JSON");
        }
    }
}
