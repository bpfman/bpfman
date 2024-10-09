// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    collections::HashMap,
    fs::{create_dir_all, remove_dir_all},
    path::{Path, PathBuf},
};

use aya::{
    programs::{
        fentry::FEntryLink, fexit::FExitLink, kprobe::KProbeLink, links::FdLink, loaded_programs,
        trace_point::TracePointLink, uprobe::UProbeLink, FEntry, FExit, KProbe, TracePoint, UProbe,
    },
    Btf, EbpfLoader,
};
use log::{debug, info, warn};
use sled::{Config as SledConfig, Db};
use tokio::time::{sleep, Duration};
use utils::initialize_bpfman;

use crate::{
    config::Config,
    directories::*,
    errors::BpfmanError,
    multiprog::{
        Dispatcher, DispatcherId, DispatcherInfo, TC_DISPATCHER_PREFIX, XDP_DISPATCHER_PREFIX,
    },
    oci_utils::image_manager::ImageManager,
    types::{
        BytecodeImage, Direction, ListFilter,
        ProbeType::{self, *},
        Program, ProgramData, ProgramType, PROGRAM_PREFIX,
    },
    utils::{
        bytes_to_string, bytes_to_u32, get_error_msg_from_stderr, get_ifindex, open_config_file,
        set_dir_permissions, should_map_be_pinned, sled_insert,
    },
};

mod config;
mod dispatcher_config;
pub mod errors;
mod multiprog;
mod oci_utils;
mod static_program;
pub mod types;
pub mod utils;

const MAPS_MODE: u32 = 0o0660;
const MAP_PREFIX: &str = "map_";
const MAPS_USED_BY_PREFIX: &str = "map_used_by_";

pub(crate) mod directories {
    // When release is made, the dispatcher images should be changed to the
    // release tag, for example "v0.5.2". After release is made, the tag should
    // be changed back to the "latest" release tag.
    pub(crate) const XDP_DISPATCHER_IMAGE: &str = "quay.io/bpfman/xdp-dispatcher:v0.5.2";
    pub(crate) const TC_DISPATCHER_IMAGE: &str = "quay.io/bpfman/tc-dispatcher:v0.5.2";

    // The following directories are used by bpfman. They should be created by bpfman service
    // via the bpfman.service settings. They will be manually created in the case where bpfman
    // is not being run as a service.
    //
    // ConfigurationDirectory: /etc/bpfman/
    pub(crate) const CFGDIR_MODE: u32 = 0o6750;
    pub(crate) const CFGDIR: &str = "/etc/bpfman";
    pub(crate) const CFGDIR_STATIC_PROGRAMS: &str = "/etc/bpfman/programs.d";
    pub(crate) const CFGPATH_BPFMAN_CONFIG: &str = "/etc/bpfman/bpfman.toml";

    // RuntimeDirectory: /run/bpfman/
    pub(crate) const RTDIR_MODE: u32 = 0o6770;
    pub(crate) const RTDIR: &str = "/run/bpfman";
    pub(crate) const RTDIR_XDP_DISPATCHER: &str = "/run/bpfman/dispatchers/xdp";
    pub(crate) const RTDIR_TC_INGRESS_DISPATCHER: &str = "/run/bpfman/dispatchers/tc-ingress";
    pub(crate) const RTDIR_TC_EGRESS_DISPATCHER: &str = "/run/bpfman/dispatchers/tc-egress";
    pub(crate) const RTDIR_FS: &str = "/run/bpfman/fs";
    pub(crate) const RTDIR_FS_TC_INGRESS: &str = "/run/bpfman/fs/tc-ingress";
    pub(crate) const RTDIR_FS_TC_EGRESS: &str = "/run/bpfman/fs/tc-egress";
    pub(crate) const RTDIR_FS_XDP: &str = "/run/bpfman/fs/xdp";
    pub(crate) const RTDIR_FS_MAPS: &str = "/run/bpfman/fs/maps";
    pub(crate) const RTDIR_PROGRAMS: &str = "/run/bpfman/programs";
    // The TUF repository is used to store Rekor and Fulcio public keys.
    pub(crate) const RTDIR_TUF: &str = "/run/bpfman/tuf";
    // StateDirectory: /var/lib/bpfman/
    pub(crate) const STDIR_MODE: u32 = 0o6770;
    pub(crate) const STDIR: &str = "/var/lib/bpfman";
    #[cfg(not(test))]
    pub(crate) const STDIR_DB: &str = "/var/lib/bpfman/db";
}

#[cfg(not(test))]
pub(crate) fn get_db_config() -> SledConfig {
    SledConfig::default().path(STDIR_DB)
}

#[cfg(test)]
pub(crate) fn get_db_config() -> SledConfig {
    SledConfig::default().temporary(true)
}

/// Adds an eBPF program to the system.
///
/// This function takes a `Program` and performs the necessary loading
/// and attaching operations to add it to the system. It supports
/// various types of eBPF programs such as XDP, TC, TCX, Tracepoint,
/// Kprobe, Uprobe, Fentry, and Fexit. The program can be added from a
/// locally built bytecode file or a remote bytecode image. If the
/// program is successfully added, it returns the updated [`Program`];
/// otherwise, it returns a `BpfmanError`.
///
/// # Arguments
///
/// * `program` - The eBPF program to be added.
///
/// # Returns
///
/// * `Ok(Program)` - If the program is successfully added.
/// * `Err(BpfmanError)` - If there is an error during the process.
///
/// # Example
///
/// ```rust,no_run
/// use bpfman::add_program;
/// use bpfman::errors::BpfmanError;
/// use bpfman::types::{KprobeProgram, Location, Program, ProgramData};
/// use std::collections::HashMap;
///
/// #[tokio::main]
/// async fn main() -> Result<(), BpfmanError> {
///     // Define the location of the eBPF object file.
///     let location = Location::File(String::from("kprobe.o"));
///
///     // Metadata is used by the userspace program to attach additional
///     // information (similar to Kubernetes labels) to an eBPF program
///     // when it is loaded. This metadata consists of key-value pairs
///     // (e.g., `owner=acme`) and can be used for filtering or selecting
///     // programs later, for instance, using commands like `bpfman list
///     // --metadata-selector owner=acme`.
///     let mut metadata = HashMap::new();
///     metadata.insert("owner".to_string(), "acme".to_string());
///
///     // Optionally, initialise global data for the program. Global data
///     // allows you to set configuration values that are applied at runtime
///     // during the loading of the eBPF program. The keys in global_data must
///     // already exist in the eBPF program, otherwise, an error
///     // will be encountered. For example:
///     let mut global_data = HashMap::new();
///     global_data.insert("global_counter".to_string(), vec![0; 8]);
///
///     // Optionally specify the owner of eBPF maps as a UID (e.g.,
///     // Some(1001)).
///     let map_owner_id = None;
///
///     // Create the program data with the specified location, name,
///     // metadata, global data, and optional map owner ID. ProgramData
///     // holds all necessary information for an eBPF program.
///     let program_data = ProgramData::new(location, String::from("kprobe_do_sys_open"), metadata, global_data, map_owner_id)?;
///
///     // Create a kprobe program with the specified function name,
///     // offset, and options. This sets up a probe at a specific point
///     // in the kernel function named "do_sys_open".
///     let probe_offset: u64 = 0;
///     let is_retprobe: bool = false;
///     let container_pid: Option<i32> = None;
///     let kprobe_program = KprobeProgram::new(program_data, String::from("do_sys_open"), probe_offset, is_retprobe, container_pid)?;
///
///     // Add the kprobe program using the bpfman manager.
///     let added_program = add_program(Program::Kprobe(kprobe_program)).await?;
///
///     // Print a success message with the name of the added program.
///     println!("Program '{}' added successfully.", added_program.get_data().get_name()?);
///     Ok(())
/// }
/// ```
///
/// # Errors
///
/// This function will return an error if:
/// * The setup or initialization steps fail.
/// * The program data fails to load.
/// * The map owner ID is invalid or setting the map pin path fails.
/// * The program bytes fail to set.
/// * Adding the program (multi-attach or single-attach) fails.
/// * The program is unsupported.
///
/// In case of failure, any created directories or loaded programs
/// will be cleaned up to maintain system integrity.
///
/// # Asynchronous Operation
///
/// This function is asynchronous and should be awaited in an
/// asynchronous context.
pub async fn add_program(mut program: Program) -> Result<Program, BpfmanError> {
    let (config, root_db) = &setup().await?;
    let mut image_manager = init_image_manager().await;
    // This is only required in the add_program api
    program.get_data_mut().load(root_db)?;

    let map_owner_id = program.get_data().get_map_owner_id()?;
    // Set map_pin_path if we're using another program's maps
    if let Some(map_owner_id) = map_owner_id {
        let map_pin_path = is_map_owner_id_valid(root_db, map_owner_id)?;
        program.get_data_mut().set_map_pin_path(&map_pin_path)?;
    }

    program
        .get_data_mut()
        .set_program_bytes(root_db, &mut image_manager)
        .await?;

    let result = match program {
        Program::Xdp(_) | Program::Tc(_) => {
            program.set_if_index(get_ifindex(&program.if_name().unwrap())?)?;

            add_multi_attach_program(root_db, &mut program, &mut image_manager, config).await
        }
        Program::Tracepoint(_)
        | Program::Kprobe(_)
        | Program::Uprobe(_)
        | Program::Fentry(_)
        | Program::Fexit(_) => add_single_attach_program(root_db, &mut program),
        Program::Unsupported(_) => panic!("Cannot add unsupported program"),
    };

    match result {
        Ok(id) => {
            info!(
                "Added {} program with name: {} and id: {id}",
                program.kind(),
                program.get_data().get_name()?
            );

            // Now that program is successfully loaded, update the id, maps hash table,
            // and allow access to all maps by bpfman group members.
            save_map(root_db, &mut program, id, map_owner_id)?;

            // Swap the db tree to be persisted with the unique program ID generated
            // by the kernel.
            program.get_data_mut().swap_tree(root_db, id)?;

            Ok(program)
        }
        Err(e) => {
            // Cleanup any directories associated with the map_pin_path.
            // map_pin_path may or may not exist depending on where the original
            // error occured, so don't error if not there and preserve original error.
            if let Ok(Some(pin_path)) = program.get_data().get_map_pin_path() {
                let _ = cleanup_map_pin_path(&pin_path, map_owner_id);
            }

            // Cleanup any program that failed to create. Ignore any delete errors.
            let _ = program.delete(root_db);

            Err(e)
        }
    }
}

/// Removes an eBPF program specified by its ID.
///
/// This function attempts to remove an eBPF program that has been
/// previously loaded by the `bpfman` tool. It performs the necessary
/// cleanup and removal steps based on the type of program (e.g., XDP,
/// Tc, Tracepoint, Kprobe, etc.).
///
/// # Arguments
///
/// * `id` - A `u32` kernel allocated value that uniquely identifies the eBPF program to be removed.
///
/// # Returns
///
/// * `Result<(), BpfmanError>` - Returns `Ok(())` if the program is successfully
///   removed, or a `BpfmanError` if an error occurs during the removal process.
///
/// # Errors
///
/// This function will return a `BpfmanError` in the following cases:
///
/// * The program with the given ID does not exist or was not created
///   by `bpfman`.
/// * The program with the given ID is currently in use and cannot be
///   deleted.
/// * The program with the given ID has dependent resources that must
///   be deleted first.
/// * The user does not have sufficient permissions to delete the program.
/// * The program type specified is invalid or unsupported for deletion.
/// * The deletion operation encountered a database error.
/// * The deletion operation was aborted due to a system timeout or
///   interruption.
///
/// # Example
///
/// ```rust,no_run
/// use bpfman::remove_program;
///
/// #[tokio::main]
/// async fn main() {
///     match remove_program(42).await {
///         Ok(()) => println!("Program successfully removed."),
///         Err(e) => eprintln!("Failed to remove program: {:?}", e),
///     }
/// }
/// ```
///
/// # Asynchronous Operation
///
/// This function is asynchronous and should be awaited in an
/// asynchronous context.
pub async fn remove_program(id: u32) -> Result<(), BpfmanError> {
    let (config, root_db) = &setup().await?;

    info!("Removing program with id: {id}");
    let prog = match get(root_db, &id) {
        Some(p) => p,
        None => {
            return Err(BpfmanError::Error(format!(
                "Program {0} does not exist or was not created by bpfman",
                id,
            )));
        }
    };

    let map_owner_id = prog.get_data().get_map_owner_id()?;

    match prog {
        Program::Xdp(_) | Program::Tc(_) => {
            let did = prog
                .dispatcher_id()?
                .ok_or(BpfmanError::DispatcherNotRequired)?;
            let program_type = prog.kind();
            let if_index = prog.if_index()?;
            let if_name = prog.if_name().unwrap();
            let direction = prog.direction()?;

            prog.delete(root_db)
                .map_err(BpfmanError::BpfmanProgramDeleteError)?;

            remove_multi_attach_program(
                root_db,
                config,
                did,
                program_type,
                if_index,
                if_name,
                direction,
            )
            .await?
        }
        Program::Tracepoint(_)
        | Program::Kprobe(_)
        | Program::Uprobe(_)
        | Program::Fentry(_)
        | Program::Fexit(_)
        | Program::Unsupported(_) => {
            prog.delete(root_db)
                .map_err(BpfmanError::BpfmanProgramDeleteError)?;
        }
    }

    delete_map(root_db, id, map_owner_id)?;

    Ok(())
}

/// Lists the currently loaded eBPF programs.
///
/// This function fetches the list of all eBPF programs loaded in the
/// system, combining those managed by `bpfman` with any other loaded
/// eBPF programs obtained from Aya. It returns a vector of `Program`
/// objects that match the provided filter.
///
/// # Arguments
///
/// * `filter` - A `ListFilter` used to filter the programs based on the caller's criteria.
///
/// # Returns
///
/// Returns a `Result` which is:
/// * `Ok(Vec<Program>)` containing a vector of `Program` objects matching the filter.
/// * `Err(BpfmanError)` if there is an error in setting up the database or retrieving programs.
///
/// # Errors
///
/// This function can return the following errors:
/// * `BpfmanError::DatabaseError` - If there is an error setting up
///   the root database.
/// * `BpfmanError::ProgramRetrievalError` - If there is an error
///   while retrieving programs from the database.
/// * `sled::Error` - If there is an error opening the program
///   database tree for a specific program ID.
/// * Other errors might be encountered while setting kernel
///   information for programs, these errors will be logged with a
///   warning but will not cause the function to return an error.
///
/// # Example
///
/// ```rust,no_run
/// use bpfman::{errors::BpfmanError, list_programs, types::ListFilter};
/// use std::collections::HashMap;
///
/// #[tokio::main]
/// async fn main() -> Result<(), BpfmanError> {
///     let program_type = None;
///     let metadata_selector = HashMap::new();
///     let bpfman_programs_only = true;
///
///     // This filter is created with None for program_type, an empty
///     // HashMap for metadata_selector, and true for
///     // bpfman_programs_only, which means it will only match bpfman
///     // programs. Setting program_type to None means it will match all
///     // program types.
///     let filter = ListFilter::new(program_type, metadata_selector, bpfman_programs_only);
///
///     match list_programs(filter).await {
///         Ok(programs) => {
///             for program in programs {
///                 match program.get_data().get_id() {
///                     Ok(id) => match program.get_data().get_name() {
///                         Ok(name) => println!("Program ID: {}, Name: {}", id, name),
///                         Err(e) => eprintln!("Error retrieving program name for ID {}: {:?}", id, e),
///                     },
///                     Err(e) => eprintln!("Error retrieving program ID: {:?}", e),
///                 }
///             }
///         }
///         Err(e) => eprintln!("Error listing programs: {:?}", e),
///     }
///     Ok(())
/// }
/// ```
///
/// # Asynchronous Operation
///
/// This function is asynchronous and should be awaited in an
/// asynchronous context.
pub async fn list_programs(filter: ListFilter) -> Result<Vec<Program>, BpfmanError> {
    let (_, root_db) = &setup().await?;

    debug!("BpfManager::list_programs()");

    // Get an iterator for the bpfman load programs, a hash map indexed by program id.
    let mut bpfman_progs: HashMap<u32, Program> = get_programs_iter(root_db).collect();

    // Call Aya to get ALL the loaded eBPF programs, and loop through each one.
    Ok(loaded_programs()
        .filter_map(|p| p.ok())
        .map(|prog| {
            let prog_id = prog.id();

            // If the program was loaded by bpfman (check the hash map), then use it.
            // Otherwise, convert the data returned from Aya into an Unsupported Program Object.
            match bpfman_progs.remove(&prog_id) {
                Some(p) => p.to_owned(),
                None => {
                    let db_tree = root_db
                        .open_tree(prog_id.to_string())
                        .expect("Unable to open program database tree for listing programs");

                    let mut data = ProgramData::new_empty(db_tree);
                    if let Err(e) = data.set_kernel_info(&prog) {
                        warn!("Unable to set kernal info for prog {prog_id}, error: {e}");
                    };

                    Program::Unsupported(data)
                }
            }
        })
        .filter(|p| filter.matches(p))
        .collect())
}

/// Retrieves information about a currently loaded eBPF program.
///
/// Attempts to retrieve detailed information about an eBPF program
/// identified by the given kernel `id`. If the program was loaded by
/// `bpfman`, it uses that information; otherwise, it queries all
/// loaded eBPF programs through the Aya library. If a match is found,
/// the program is converted into an unsupported program object.
///
/// The `Location` of the program indicates whether the program's
/// bytecode was provided via a fully qualified local path or through
/// an OCI-compliant container image tag.
///
/// # Arguments
///
/// * `id` - A `u32` representing the unique identifier of the eBPF program.
///
/// # Returns
///
/// * `Ok(Program)` - On successful retrieval of the program
///   information, returns a `Program` object encapsulating the
///   program details.
///
/// * `Err(BpfmanError)` - Returns an error if the setup fails, the
///   program is not found, or there is an issue opening the database
///   tree or setting kernel information.
///
/// # Errors
///
/// This function can return several types of errors:
/// * `BpfmanError::SetupError` - If the setup function fails.
/// * `BpfmanError::DatabaseError` - If there is an issue opening the
///   database tree for the program.
/// * `BpfmanError::KernelInfoError` - If there is an issue setting
///   the kernel information for the program data.
/// * `BpfmanError::Error` - If the program with the specified `id`
///   does not exist.
///
/// # Examples
///
/// ```rust,no_run
/// use bpfman::get_program;
///
/// #[tokio::main]
/// async fn main() {
///     match get_program(42).await {
///         Ok(program) => println!("Program info: {:?}", program),
///         Err(e) => eprintln!("Error fetching program: {:?}", e),
///     }
/// }
/// ```
///
/// # Asynchronous Operation
///
/// This function is asynchronous and should be awaited in an
/// asynchronous context.
pub async fn get_program(id: u32) -> Result<Program, BpfmanError> {
    let (_, root_db) = &setup().await?;

    debug!("Getting program with id: {id}");
    // If the program was loaded by bpfman, then use it.
    // Otherwise, call Aya to get ALL the loaded eBPF programs, and convert the data
    // returned from Aya into an Unsupported Program Object.
    match get(root_db, &id) {
        Some(p) => Ok(p.to_owned()),
        None => loaded_programs()
            .find_map(|p| {
                let prog = p.ok()?;
                if prog.id() == id {
                    let db_tree = root_db
                        .open_tree(prog.id().to_string())
                        .expect("Unable to open program database tree for listing programs");

                    let mut data = ProgramData::new_empty(db_tree);
                    data.set_kernel_info(&prog)
                        .expect("unable to set kernel info");

                    Some(Program::Unsupported(data))
                } else {
                    None
                }
            })
            .ok_or(BpfmanError::Error(format!(
                "Program {0} does not exist",
                id
            ))),
    }
}

/// Pulls an OCI-compliant image containing eBPF bytecode from a
/// remote container registry.
///
/// # Arguments
///
/// * `image` - A `BytecodeImage` struct that contains information
///             about the bytecode image to be pulled, including its
///             URL, pull policy, username, and password.
///
/// # Returns
///
/// This function returns an `anyhow::Result<()>` which, on success,
/// contains an empty tuple `()`. On failure, it returns an
/// `anyhow::Error` encapsulating the cause of the failure.
///
/// # Errors
///
/// This function can return the following errors:
///
/// * `SetupError` - If the `setup()` function fails to initialise
///                  correctly.
/// * `ImageManagerInitializationError` - If there is an error
///                                        initialising the image
///                                        manager with `init_image_manager()`.
/// * `RegistryAuthenticationError` - If there is an authentication
///                                    failure while accessing the
///                                    container registry.
/// * `NetworkError` - If there are network issues while pulling the
///                    image from the container registry.
/// * `ImagePullError` - If there is a problem pulling the image due
///                      to invalid image URL, unsupported image
///                      format, or other image-specific issues.
///
/// # Examples
///
/// ```rust,no_run
/// use bpfman::pull_bytecode;
/// use bpfman::types::{BytecodeImage, ImagePullPolicy};
///
/// #[tokio::main]
/// async fn main() {
///     let image = BytecodeImage {
///         image_url: "example.com/myrepository/myimage:latest".to_string(),
///         image_pull_policy: ImagePullPolicy::IfNotPresent,
///
///         // Optional username/password for authentication.
///         username: Some("username".to_string()),
///         password: Some("password".to_string()),
///     };
///
///     match pull_bytecode(image).await {
///         Ok(_) => println!("Image pulled successfully."),
///         Err(e) => eprintln!("Failed to pull image: {}", e),
///     }
/// }
/// ```
///
/// # Asynchronous Operation
///
/// This function is asynchronous and should be awaited in an
/// asynchronous context.
pub async fn pull_bytecode(image: BytecodeImage) -> anyhow::Result<()> {
    let (_, root_db) = &setup().await?;
    let image_manager = &mut init_image_manager().await;

    image_manager
        .get_image(
            root_db,
            &image.image_url,
            image.image_pull_policy.clone(),
            image.username.clone(),
            image.password.clone(),
        )
        .await?;
    Ok(())
}

pub(crate) async fn init_database(sled_config: SledConfig) -> Result<Db, BpfmanError> {
    let database_config = open_config_file().database().to_owned().unwrap_or_default();
    for _ in 0..=database_config.max_retries {
        if let Ok(db) = sled_config.open() {
            debug!("Successfully opened database");
            return Ok(db);
        } else {
            info!(
                "Database lock is already held, retrying after {} milliseconds",
                database_config.millisec_delay
            );
            sleep(Duration::from_millis(database_config.millisec_delay)).await;
        }
    }
    Err(BpfmanError::DatabaseLockError)
}

// Make sure to call init_image_manger if the command requires interaction with
// an OCI based container registry. It should ONLY be used where needed, to
// explicitly control when bpfman blocks for network calls to both sigstore's
// cosign tuf registries and container registries.
pub(crate) async fn init_image_manager() -> ImageManager {
    let signing_config = open_config_file().signing().to_owned().unwrap_or_default();
    ImageManager::new(signing_config.verify_enabled, signing_config.allow_unsigned)
        .await
        .expect("failed to initialize image manager")
}

fn get_dispatcher(id: &DispatcherId, root_db: &Db) -> Option<Dispatcher> {
    let tree_name_prefix = match id {
        DispatcherId::Xdp(DispatcherInfo(if_index, _)) => {
            format!("{}_{}", XDP_DISPATCHER_PREFIX, if_index)
        }
        DispatcherId::Tc(DispatcherInfo(if_index, Some(direction))) => {
            format!("{}_{}_{}", TC_DISPATCHER_PREFIX, if_index, direction)
        }
        _ => {
            return None;
        }
    };

    root_db
        .tree_names()
        .into_iter()
        .find(|p| bytes_to_string(p).contains(&tree_name_prefix))
        .map(|p| {
            let tree = root_db.open_tree(p).expect("unable to open database tree");
            Dispatcher::new_from_db(tree)
        })
}

/// Returns the number of extension programs currently attached to the dispatcher that
/// would be used to attach the provided [`Program`].
fn num_attached_programs(did: &DispatcherId, root_db: &Db) -> usize {
    if let Some(d) = get_dispatcher(did, root_db) {
        d.num_extensions()
    } else {
        0
    }
}

fn get(root_db: &Db, id: &u32) -> Option<Program> {
    let prog_tree: sled::IVec = (PROGRAM_PREFIX.to_string() + &id.to_string())
        .as_bytes()
        .into();
    if root_db.tree_names().contains(&prog_tree) {
        let tree = root_db
            .open_tree(prog_tree)
            .expect("unable to open database tree");
        Some(Program::new_from_db(*id, tree).expect("Failed to build program from database"))
    } else {
        None
    }
}

fn filter(
    root_db: &'_ Db,
    program_type: ProgramType,
    if_index: Option<u32>,
    direction: Option<Direction>,
) -> impl Iterator<Item = Program> + '_ {
    root_db
        .tree_names()
        .into_iter()
        .filter(|p| bytes_to_string(p).contains(PROGRAM_PREFIX))
        .map(|p| {
            let id = bytes_to_string(&p)
                .split('_')
                .last()
                .unwrap()
                .parse::<u32>()
                .unwrap();
            let tree = root_db.open_tree(p).expect("unable to open database tree");
            Program::new_from_db(id, tree).expect("Failed to build program from database")
        })
        .filter(move |p| {
            p.kind() == program_type
                && p.if_index().unwrap() == if_index
                && p.direction().unwrap() == direction
        })
}

// Adds a new program and sets the positions of programs that are to be attached via a dispatcher.
// Positions are set based on order of priority. Ties are broken based on:
// - Already attached programs are preferred
// - Program name. Lowest lexical order wins.
fn add_and_set_program_positions(root_db: &Db, program: Program) {
    let program_type = program.kind();
    let if_index = program.if_index().unwrap();
    let direction = program.direction().unwrap();

    let mut extensions =
        filter(root_db, program_type, if_index, direction).collect::<Vec<Program>>();

    extensions.sort_by_key(|b| {
        (
            b.priority().unwrap(),
            b.attached(),
            b.get_data().get_name().unwrap().to_owned(),
        )
    });
    for (i, v) in extensions.iter_mut().enumerate() {
        v.set_position(i).expect("unable to set program position");
    }
}

// Sets the positions of programs that are to be attached via a dispatcher.
// Positions are set based on order of priority. Ties are broken based on:
// - Already attached programs are preferred
// - Program name. Lowest lexical order wins.
fn set_program_positions(
    root_db: &Db,
    program_type: ProgramType,
    if_index: u32,
    direction: Option<Direction>,
) {
    let mut extensions =
        filter(root_db, program_type, Some(if_index), direction).collect::<Vec<Program>>();

    extensions.sort_by_key(|b| {
        (
            b.priority().unwrap(),
            b.attached(),
            b.get_data().get_name().unwrap().to_owned(),
        )
    });
    for (i, v) in extensions.iter_mut().enumerate() {
        v.set_position(i).expect("unable to set program position");
    }
}

fn get_programs_iter(root_db: &Db) -> impl Iterator<Item = (u32, Program)> + '_ {
    root_db
        .tree_names()
        .into_iter()
        .filter(|p| bytes_to_string(p).contains(PROGRAM_PREFIX))
        .map(|p| {
            let id = bytes_to_string(&p)
                .split('_')
                .last()
                .unwrap()
                .parse::<u32>()
                .unwrap();
            let tree = root_db.open_tree(p).expect("unable to open database tree");
            (
                id,
                Program::new_from_db(id, tree).expect("Failed to build program from database"),
            )
        })
}

async fn setup() -> Result<(Config, Db), BpfmanError> {
    initialize_bpfman()?;

    Ok((open_config_file(), init_database(get_db_config()).await?))
}

async fn add_multi_attach_program(
    root_db: &Db,
    program: &mut Program,
    image_manager: &mut ImageManager,
    config: &Config,
) -> Result<u32, BpfmanError> {
    debug!("BpfManager::add_multi_attach_program()");
    let name = &program.get_data().get_name()?;

    // This load is just to verify the BPF Function Name is valid.
    // The actual load is performed in the XDP or TC logic.
    // don't pin maps here.
    let mut ext_loader = EbpfLoader::new()
        .allow_unsupported_maps()
        .extension(name)
        .load(&program.get_data().get_program_bytes()?)?;

    match ext_loader.program_mut(name) {
        Some(_) => Ok(()),
        None => Err(BpfmanError::BpfFunctionNameNotValid(name.to_owned())),
    }?;

    let did = program
        .dispatcher_id()?
        .ok_or(BpfmanError::DispatcherNotRequired)?;

    let next_available_id = num_attached_programs(&did, root_db);
    if next_available_id >= 10 {
        return Err(BpfmanError::TooManyPrograms);
    }

    debug!("next_available_id={next_available_id}");

    let program_type = program.kind();
    let if_index = program.if_index()?;
    let if_name = program.if_name().unwrap().to_string();
    let direction = program.direction()?;

    add_and_set_program_positions(root_db, program.clone());

    let mut programs: Vec<Program> =
        filter(root_db, program_type, if_index, direction).collect::<Vec<Program>>();

    let old_dispatcher = get_dispatcher(&did, root_db);

    let if_config = if let Some(ref i) = config.interfaces() {
        i.get(&if_name)
    } else {
        None
    };
    let next_revision = if let Some(ref old) = old_dispatcher {
        old.next_revision()
    } else {
        1
    };

    Dispatcher::new(
        root_db,
        if_config,
        &mut programs,
        next_revision,
        old_dispatcher,
        image_manager,
    )
    .await
    .or_else(|e| {
        // If kernel ID was never set there's no pins to cleanup here so just continue
        if program.get_data().get_id().is_ok() {
            program
                .delete(root_db)
                .map_err(BpfmanError::BpfmanProgramDeleteError)?;
        }
        Err(e)
    })?;

    let id = program.get_data().get_id()?;
    program.set_attached();

    Ok(id)
}

pub(crate) fn add_single_attach_program(root_db: &Db, p: &mut Program) -> Result<u32, BpfmanError> {
    debug!("BpfManager::add_single_attach_program()");
    let name = &p.get_data().get_name()?;
    let mut bpf = EbpfLoader::new();

    let data = &p.get_data().get_global_data()?;
    for (key, value) in data {
        bpf.set_global(key, value.as_slice(), true);
    }

    // If map_pin_path is set already it means we need to use a pin
    // path which should already exist on the system.
    if let Some(map_pin_path) = p.get_data().get_map_pin_path()? {
        debug!(
            "single-attach program {name} is using maps from {:?}",
            map_pin_path
        );
        bpf.map_pin_path(map_pin_path);
    }

    let mut loader = bpf
        .allow_unsupported_maps()
        .load(&p.get_data().get_program_bytes()?)?;

    let raw_program = loader
        .program_mut(name)
        .ok_or(BpfmanError::BpfFunctionNameNotValid(name.to_owned()))?;

    let res = match p {
        Program::Tracepoint(ref mut program) => {
            let tracepoint = program.get_tracepoint()?;
            let parts: Vec<&str> = tracepoint.split('/').collect();
            if parts.len() != 2 {
                return Err(BpfmanError::InvalidAttach(
                    program.get_tracepoint()?.to_string(),
                ));
            }
            let category = parts[0].to_owned();
            let name = parts[1].to_owned();

            let tracepoint: &mut TracePoint = raw_program.try_into()?;

            tracepoint.load()?;
            program
                .get_data_mut()
                .set_kernel_info(&tracepoint.info()?)?;

            let id = program.data.get_id()?;

            let link_id = tracepoint.attach(&category, &name)?;

            let owned_link: TracePointLink = tracepoint.take_link(link_id)?;
            let fd_link: FdLink = owned_link
                .try_into()
                .expect("unable to get owned tracepoint attach link");

            fd_link
                .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                .map_err(BpfmanError::UnableToPinLink)?;

            tracepoint
                .pin(format!("{RTDIR_FS}/prog_{}", id))
                .map_err(BpfmanError::UnableToPinProgram)?;

            Ok(id)
        }
        Program::Kprobe(ref mut program) => {
            let requested_probe_type = match program.get_retprobe()? {
                true => Kretprobe,
                false => Kprobe,
            };

            if requested_probe_type == Kretprobe && program.get_offset()? != 0 {
                return Err(BpfmanError::Error(format!(
                    "offset not allowed for {Kretprobe}"
                )));
            }

            let kprobe: &mut KProbe = raw_program.try_into()?;
            kprobe.load()?;

            // verify that the program loaded was the same type as the
            // user requested
            let loaded_probe_type = ProbeType::from(kprobe.kind());
            if requested_probe_type != loaded_probe_type {
                return Err(BpfmanError::Error(format!(
                    "expected {requested_probe_type}, loaded program is {loaded_probe_type}"
                )));
            }

            program.get_data_mut().set_kernel_info(&kprobe.info()?)?;

            let id = program.data.get_id()?;

            let link_id = kprobe.attach(program.get_fn_name()?, program.get_offset()?)?;

            let owned_link: KProbeLink = kprobe.take_link(link_id)?;
            let fd_link: FdLink = owned_link
                .try_into()
                .expect("unable to get owned kprobe attach link");

            fd_link
                .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                .map_err(BpfmanError::UnableToPinLink)?;

            kprobe
                .pin(format!("{RTDIR_FS}/prog_{}", id))
                .map_err(BpfmanError::UnableToPinProgram)?;

            Ok(id)
        }
        Program::Uprobe(ref mut program) => {
            let requested_probe_type = match program.get_retprobe()? {
                true => Uretprobe,
                false => Uprobe,
            };

            let uprobe: &mut UProbe = raw_program.try_into()?;
            uprobe.load()?;

            // verify that the program loaded was the same type as the
            // user requested
            let loaded_probe_type = ProbeType::from(uprobe.kind());
            if requested_probe_type != loaded_probe_type {
                return Err(BpfmanError::Error(format!(
                    "expected {requested_probe_type}, loaded program is {loaded_probe_type}"
                )));
            }

            program.get_data_mut().set_kernel_info(&uprobe.info()?)?;

            let id = program.data.get_id()?;

            let program_pin_path = format!("{RTDIR_FS}/prog_{}", id);
            let fn_name = program.get_fn_name()?;

            uprobe
                .pin(program_pin_path.clone())
                .map_err(BpfmanError::UnableToPinProgram)?;

            match program.get_container_pid()? {
                None => {
                    // Attach uprobe in same container as the bpfman process
                    let link_id = uprobe.attach(
                        fn_name.as_deref(),
                        program.get_offset()?,
                        program.get_target()?,
                        None,
                    )?;

                    let owned_link: UProbeLink = uprobe.take_link(link_id)?;
                    let fd_link: FdLink = owned_link
                        .try_into()
                        .expect("unable to get owned uprobe attach link");

                    fd_link
                        .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                        .map_err(BpfmanError::UnableToPinLink)?;
                }
                Some(p) => {
                    // Attach uprobe in different container from the bpfman process
                    let offset = program.get_offset()?.to_string();
                    let container_pid = p.to_string();
                    let mut prog_args = vec![
                        "uprobe".to_string(),
                        "--program-pin-path".to_string(),
                        program_pin_path,
                        "--offset".to_string(),
                        offset,
                        "--target".to_string(),
                        program.get_target()?.to_string(),
                        "--container-pid".to_string(),
                        container_pid,
                    ];

                    if let Some(fn_name) = &program.get_fn_name()? {
                        prog_args.extend(["--fn-name".to_string(), fn_name.to_string()])
                    }

                    if program.get_retprobe()? {
                        prog_args.push("--retprobe".to_string());
                    }

                    if let Some(pid) = program.get_pid()? {
                        prog_args.extend(["--pid".to_string(), pid.to_string()])
                    }

                    debug!("calling bpfman-ns to attach uprobe in pid: {:?}", p);

                    // Figure out where the bpfman-ns binary is located
                    let bpfman_ns_path = if Path::new("./target/debug/bpfman-ns").exists() {
                        // If we're running natively from the bpfman
                        // directory, use the binary in the target/debug
                        // directory
                        "./target/debug/bpfman-ns"
                    } else if Path::new("./bpfman-ns").exists() {
                        // If we're running on kubernetes, the bpfman-ns
                        // binary will be in the current directory
                        "./bpfman-ns"
                    } else {
                        // look for bpfman-ns in the PATH
                        "bpfman-ns"
                    };

                    let output = std::process::Command::new(bpfman_ns_path)
                        .args(prog_args)
                        .output();

                    match output {
                        Ok(o) => {
                            if !o.status.success() {
                                info!(
                                    "Error from bpfman-ns: {:?}",
                                    get_error_msg_from_stderr(&o.stderr)
                                );
                                return Err(BpfmanError::ContainerAttachError {
                                    program_type: "uprobe".to_string(),
                                    container_pid: program.get_container_pid()?.unwrap(),
                                });
                            };
                        }
                        Err(e) => {
                            info!("bpfman-ns returned error: {:?}", e);
                            return Err(BpfmanError::ContainerAttachError {
                                program_type: "uprobe".to_string(),
                                container_pid: program.get_container_pid()?.unwrap(),
                            });
                        }
                    };
                }
            };

            Ok(id)
        }
        Program::Fentry(ref mut program) => {
            let fn_name = program.get_fn_name()?;
            let btf = Btf::from_sys_fs()?;
            let fentry: &mut FEntry = raw_program.try_into()?;
            fentry
                .load(&fn_name, &btf)
                .map_err(BpfmanError::BpfProgramError)?;
            program.get_data_mut().set_kernel_info(&fentry.info()?)?;

            let id = program.data.get_id()?;
            let link_id = fentry.attach()?;
            let owned_link: FEntryLink = fentry.take_link(link_id)?;
            let fd_link: FdLink = owned_link.into();
            fd_link
                .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                .map_err(BpfmanError::UnableToPinLink)?;

            fentry
                .pin(format!("{RTDIR_FS}/prog_{}", id))
                .map_err(BpfmanError::UnableToPinProgram)?;

            Ok(id)
        }
        Program::Fexit(ref mut program) => {
            let fn_name = program.get_fn_name()?;
            let btf = Btf::from_sys_fs()?;
            let fexit: &mut FExit = raw_program.try_into()?;
            fexit
                .load(&fn_name, &btf)
                .map_err(BpfmanError::BpfProgramError)?;
            program.get_data_mut().set_kernel_info(&fexit.info()?)?;

            let id = program.data.get_id()?;
            let link_id = fexit.attach()?;
            let owned_link: FExitLink = fexit.take_link(link_id)?;
            let fd_link: FdLink = owned_link.into();
            fd_link
                .pin(format!("{RTDIR_FS}/prog_{}_link", id))
                .map_err(BpfmanError::UnableToPinLink)?;

            fexit
                .pin(format!("{RTDIR_FS}/prog_{}", id))
                .map_err(BpfmanError::UnableToPinProgram)?;

            Ok(id)
        }
        _ => panic!("not a supported single attach program"),
    };

    match res {
        Ok(id) => {
            // If this program is the map(s) owner pin all maps (except for .rodata and .bss) by name.
            if p.get_data().get_map_pin_path()?.is_none() {
                let map_pin_path = calc_map_pin_path(id);
                p.get_data_mut().set_map_pin_path(&map_pin_path)?;
                create_map_pin_path(&map_pin_path)?;

                for (name, map) in loader.maps_mut() {
                    if !should_map_be_pinned(name) {
                        continue;
                    }
                    debug!(
                        "Pinning map: {name} to path: {}",
                        map_pin_path.join(name).display()
                    );
                    map.pin(map_pin_path.join(name))
                        .map_err(BpfmanError::UnableToPinMap)?;
                }
            }
        }
        Err(_) => {
            // If kernel ID was never set there's no pins to cleanup here so just continue
            if p.get_data().get_id().is_ok() {
                p.delete(root_db)
                    .map_err(BpfmanError::BpfmanProgramDeleteError)?;
            };
        }
    };

    res
}

async fn remove_multi_attach_program(
    root_db: &Db,
    config: &Config,
    did: DispatcherId,
    program_type: ProgramType,
    if_index: Option<u32>,
    if_name: String,
    direction: Option<Direction>,
) -> Result<(), BpfmanError> {
    debug!("BpfManager::remove_multi_attach_program()");
    let mut image_manager = init_image_manager().await;

    let next_available_id = num_attached_programs(&did, root_db) - 1;
    debug!("next_available_id = {next_available_id}");

    let mut old_dispatcher = get_dispatcher(&did, root_db);

    if let Some(ref mut old) = old_dispatcher {
        if next_available_id == 0 {
            // Delete the dispatcher
            return old.delete(root_db, true);
        }
    }

    set_program_positions(root_db, program_type, if_index.unwrap(), direction);

    // Intentionally don't add filter program here
    let mut programs: Vec<Program> = filter(root_db, program_type, if_index, direction).collect();

    let if_config = if let Some(ref i) = config.interfaces() {
        i.get(&if_name)
    } else {
        None
    };
    let next_revision = if let Some(ref old) = old_dispatcher {
        old.next_revision()
    } else {
        1
    };
    debug!("next_revision = {next_revision}");

    Dispatcher::new(
        root_db,
        if_config,
        &mut programs,
        next_revision,
        old_dispatcher,
        &mut image_manager,
    )
    .await?;

    Ok(())
}

// This function checks to see if the user provided map_owner_id is valid.
fn is_map_owner_id_valid(root_db: &Db, map_owner_id: u32) -> Result<PathBuf, BpfmanError> {
    let map_pin_path = calc_map_pin_path(map_owner_id);
    let name: &sled::IVec = &format!("{}{}", MAP_PREFIX, map_owner_id).as_bytes().into();

    if root_db.tree_names().contains(name) {
        // Return the map_pin_path
        return Ok(map_pin_path);
    }
    Err(BpfmanError::Error(
        "map_owner_id does not exists".to_string(),
    ))
}

// This function is called if the program's map directory was created,
// but the eBPF program failed to load. save_map() has not been called,
// so self.maps has not been updated for this program.
// If the user provided a ID of program to share a map with,
// then map the directory is still in use and there is nothing to do.
// Otherwise, the map directory was created so it must
// deleted.
fn cleanup_map_pin_path(map_pin_path: &Path, map_owner_id: Option<u32>) -> Result<(), BpfmanError> {
    if map_owner_id.is_none() && map_pin_path.exists() {
        let _ = remove_dir_all(map_pin_path)
            .map_err(|e| BpfmanError::Error(format!("can't delete map dir: {e}")));
        Ok(())
    } else {
        Ok(())
    }
}

// This function writes the map to the map hash table. If this eBPF
// program is the map owner, then a new entry is add to the map hash
// table and permissions on the directory are updated to grant bpfman
// user group access to all the maps in the directory. If this eBPF
// program is not the owner, then the eBPF program ID is added to
// the Used-By array.
fn save_map(
    root_db: &Db,
    program: &mut Program,
    id: u32,
    map_owner_id: Option<u32>,
) -> Result<(), BpfmanError> {
    let data = program.get_data_mut();

    match map_owner_id {
        Some(m) => {
            if let Some(map) = get_map(m, root_db) {
                push_maps_used_by(map.clone(), id)?;
                let used_by = get_maps_used_by(map)?;

                // This program has no been inserted yet, so set map_used_by to
                // newly updated list.
                data.set_maps_used_by(used_by.clone())?;

                // Update all the programs using the same map with the updated map_used_by.
                for used_by_id in used_by.iter() {
                    if let Some(mut program) = get(root_db, used_by_id) {
                        program.get_data_mut().set_maps_used_by(used_by.clone())?;
                    }
                }
            } else {
                return Err(BpfmanError::Error(
                    "map_owner_id does not exist".to_string(),
                ));
            }
        }
        None => {
            let db_tree = root_db
                .open_tree(format!("{}{}", MAP_PREFIX, id))
                .expect("Unable to open map db tree");

            set_maps_used_by(db_tree, vec![id])?;

            // Update this program with the updated map_used_by
            data.set_maps_used_by(vec![id])?;

            // Set the permissions on the map_pin_path directory.
            if let Some(map_pin_path) = data.get_map_pin_path()? {
                if let Some(path) = map_pin_path.to_str() {
                    debug!("bpf set dir permissions for {}", path);
                    set_dir_permissions(path, MAPS_MODE);
                } else {
                    return Err(BpfmanError::Error(format!(
                        "invalid map_pin_path {} for {}",
                        map_pin_path.display(),
                        id
                    )));
                }
            } else {
                return Err(BpfmanError::Error(format!(
                    "map_pin_path should be set for {}",
                    id
                )));
            }
        }
    }

    Ok(())
}

// This function cleans up a map entry when an eBPF program is
// being unloaded. If the eBPF program is the map owner, then
// the map is removed from the hash table and the associated
// directory is removed. If this eBPF program is referencing a
// map from another eBPF program, then this eBPF programs ID
// is removed from the UsedBy array.
fn delete_map(root_db: &Db, id: u32, map_owner_id: Option<u32>) -> Result<(), BpfmanError> {
    let index = match map_owner_id {
        Some(i) => i,
        None => id,
    };

    if let Some(map) = get_map(index, root_db) {
        let mut used_by = get_maps_used_by(map.clone())?;

        if let Some(index) = used_by.iter().position(|value| *value == id) {
            used_by.swap_remove(index);
        }

        clear_maps_used_by(map.clone());
        set_maps_used_by(map.clone(), used_by.clone())?;

        if used_by.is_empty() {
            let path: PathBuf = calc_map_pin_path(index);
            // No more programs using this map, so remove the entry from the map list.
            root_db
                .drop_tree(MAP_PREFIX.to_string() + &index.to_string())
                .expect("unable to drop maps tree");
            remove_dir_all(path)
                .map_err(|e| BpfmanError::Error(format!("can't delete map dir: {e}")))?;
        } else {
            // Update all the programs still using the same map with the updated map_used_by.
            for id in used_by.iter() {
                if let Some(mut program) = get(root_db, id) {
                    program.get_data_mut().set_maps_used_by(used_by.clone())?;
                }
            }
        }
    } else {
        return Err(BpfmanError::Error(
            "map_pin_path does not exists".to_string(),
        ));
    }

    Ok(())
}

// map_pin_path is a the directory the maps are located. Currently, it
// is a fixed bpfman location containing the map_index, which is a ID.
// The ID is either the programs ID, or the ID of another program
// that map_owner_id references.
pub(crate) fn calc_map_pin_path(id: u32) -> PathBuf {
    PathBuf::from(format!("{RTDIR_FS_MAPS}/{}", id))
}

// Create the map_pin_path for a given program.
pub(crate) fn create_map_pin_path(p: &Path) -> Result<(), BpfmanError> {
    create_dir_all(p).map_err(|e| BpfmanError::Error(format!("can't create map dir: {e}")))
}

// set_maps_used_by differs from other setters in that it's explicitly idempotent.
pub(crate) fn set_maps_used_by(db_tree: sled::Tree, ids: Vec<u32>) -> Result<(), BpfmanError> {
    ids.iter().enumerate().try_for_each(|(i, v)| {
        sled_insert(
            &db_tree,
            format!("{MAPS_USED_BY_PREFIX}{i}").as_str(),
            &v.to_ne_bytes(),
        )
    })
}

// set_maps_used_by differs from other setters in that it's explicitly idempotent.
fn push_maps_used_by(db_tree: sled::Tree, id: u32) -> Result<(), BpfmanError> {
    let existing_maps_used_by = get_maps_used_by(db_tree.clone())?;

    sled_insert(
        &db_tree,
        format!("{MAPS_USED_BY_PREFIX}{}", existing_maps_used_by.len() + 1).as_str(),
        &id.to_ne_bytes(),
    )
}

fn get_maps_used_by(db_tree: sled::Tree) -> Result<Vec<u32>, BpfmanError> {
    db_tree
        .scan_prefix(MAPS_USED_BY_PREFIX)
        .map(|n| n.map(|(_, v)| bytes_to_u32(v.to_vec())))
        .map(|n| {
            n.map_err(|e| {
                BpfmanError::DatabaseError("Failed to get maps used by".to_string(), e.to_string())
            })
        })
        .collect()
}

pub(crate) fn clear_maps_used_by(db_tree: sled::Tree) {
    db_tree.scan_prefix(MAPS_USED_BY_PREFIX).for_each(|n| {
        db_tree
            .remove(n.unwrap().0)
            .expect("unable to clear maps used by");
    });
}

fn get_map(id: u32, root_db: &Db) -> Option<sled::Tree> {
    root_db
        .tree_names()
        .into_iter()
        .find(|n| bytes_to_string(n) == format!("{}{}", MAP_PREFIX, id))
        .map(|n| root_db.open_tree(n).expect("unable to open map tree"))
}
