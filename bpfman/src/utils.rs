use std::{
    io::Read,
    os::unix::fs::{OpenOptionsExt, PermissionsExt},
    path::Path,
    str,
};

use anyhow::{Context, Result};
use bpfman_api::{config::Config, util::directories::CFGPATH_BPFMAN_CONFIG};
use log::{debug, info, warn};
use nix::{
    mount::{mount, MsFlags},
    net::if_::if_nametoindex,
};
use sled::Tree;

use crate::errors::BpfmanError;

// The bpfman socket should always allow the same users and members of the same group
// to Read/Write to it.
pub(crate) const SOCK_MODE: u32 = 0o0660;

// Like tokio::fs::read, but with O_NOCTTY set
pub(crate) fn read<P: AsRef<Path>>(path: P) -> Result<Vec<u8>, BpfmanError> {
    let mut data = vec![];
    std::fs::OpenOptions::new()
        .custom_flags(nix::libc::O_NOCTTY)
        .read(true)
        .open(path)
        .map_err(|e| BpfmanError::Error(format!("can't open file: {e}")))?
        .read_to_end(&mut data)
        .map_err(|e| BpfmanError::Error(format!("can't read file: {e}")))?;
    Ok(data)
}

pub(crate) fn get_ifindex(iface: &str) -> Result<u32, BpfmanError> {
    match if_nametoindex(iface) {
        Ok(index) => {
            debug!("Map {} to {}", iface, index);
            Ok(index)
        }
        Err(_) => {
            info!("Unable to validate interface {}", iface);
            Err(BpfmanError::InvalidInterface)
        }
    }
}

pub(crate) fn set_file_permissions(path: &Path, mode: u32) {
    // Set the permissions on the file based on input
    if (std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode))).is_err() {
        warn!(
            "Unable to set permissions on file {}. Continuing",
            path.to_path_buf().display()
        );
    }
}

pub(crate) fn set_dir_permissions(directory: &str, mode: u32) {
    // Iterate through the files in the provided directory
    let entries = std::fs::read_dir(directory).unwrap();
    for file in entries.flatten() {
        // Set the permissions on the file based on input
        set_file_permissions(&file.path(), mode);
    }
}

pub(crate) fn create_bpffs(directory: &str) -> anyhow::Result<()> {
    debug!("Creating bpffs at {directory}");
    let flags = MsFlags::MS_NOSUID | MsFlags::MS_NODEV | MsFlags::MS_NOEXEC | MsFlags::MS_RELATIME;
    mount::<str, str, str, str>(None, directory, Some("bpf"), flags, None)
        .with_context(|| format!("unable to create bpffs at {directory}"))
}

pub(crate) fn should_map_be_pinned(name: &str) -> bool {
    !(name.contains(".rodata") || name.contains(".bss") || name.contains(".data"))
}

pub(crate) fn bytes_to_u32(bytes: Vec<u8>) -> u32 {
    u32::from_ne_bytes(
        bytes
            .try_into()
            .expect("unable to marshall &[u8] to &[u8; 4]"),
    )
}

pub(crate) fn bytes_to_u16(bytes: Vec<u8>) -> u16 {
    u16::from_ne_bytes(
        bytes
            .try_into()
            .expect("unable to marshall &[u8] to &[u8; 4]"),
    )
}

pub(crate) fn bytes_to_i32(bytes: Vec<u8>) -> i32 {
    i32::from_ne_bytes(
        bytes
            .try_into()
            .expect("unable to marshall &[u8] to &[u8; 4]"),
    )
}

pub(crate) fn bytes_to_string(bytes: &[u8]) -> String {
    String::from_utf8(bytes.to_vec()).expect("failed to convert &[u8] to string")
}

pub(crate) fn bytes_to_bool(bytes: Vec<u8>) -> bool {
    i8::from_ne_bytes(
        bytes
            .try_into()
            .expect("unable to marshall &[u8] to &[i8; 1]"),
    ) != 0
}

pub(crate) fn bytes_to_usize(bytes: Vec<u8>) -> usize {
    usize::from_ne_bytes(
        bytes
            .try_into()
            .expect("unable to marshall &[u8] to &[u8; 8]"),
    )
}

pub(crate) fn bytes_to_u64(bytes: Vec<u8>) -> u64 {
    u64::from_ne_bytes(
        bytes
            .try_into()
            .expect("unable to marshall &[u8] to &[u8; 8]"),
    )
}

// Sled in memory database helper functions which help with error handling and
// data marshalling.

pub(crate) fn sled_get(db_tree: &Tree, key: &str) -> Result<Vec<u8>, BpfmanError> {
    db_tree
        .get(key)
        .map_err(|e| {
            BpfmanError::DatabaseError(
                format!(
                    "Unable to get database entry {key} from tree {}",
                    bytes_to_string(&db_tree.name())
                ),
                e.to_string(),
            )
        })?
        .map(|v| v.to_vec())
        .ok_or(BpfmanError::DatabaseError(
            format!(
                "Database entry {key} does not exist in tree {:?}",
                bytes_to_string(&db_tree.name())
            ),
            "".to_string(),
        ))
}

pub(crate) fn sled_get_option(db_tree: &Tree, key: &str) -> Result<Option<Vec<u8>>, BpfmanError> {
    db_tree
        .get(key)
        .map_err(|e| {
            BpfmanError::DatabaseError(
                format!(
                    "Unable to get database entry {key} from tree {}",
                    bytes_to_string(&db_tree.name()),
                ),
                e.to_string(),
            )
        })
        .map(|v| v.map(|v| v.to_vec()))
}

pub(crate) fn sled_insert(db_tree: &Tree, key: &str, value: &[u8]) -> Result<(), BpfmanError> {
    db_tree.insert(key, value).map(|_| ()).map_err(|e| {
        BpfmanError::DatabaseError(
            format!(
                "Unable to insert database entry {key} into tree {:?}",
                db_tree.name()
            ),
            e.to_string(),
        )
    })
}

// Helper function to get the error message from stderr
pub(crate) fn get_error_msg_from_stderr(stderr: &[u8]) -> String {
    // Convert to lines
    let stderr_lines = String::from_utf8_lossy(stderr)
        .split('\n')
        .map(|s| s.to_string())
        .collect::<Vec<String>>();

    // Remove empty lines
    let stderr_lines = stderr_lines
        .iter()
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string())
        .collect::<Vec<String>>();

    // return the last line if it exists, otherwise return "No message"
    stderr_lines
        .last()
        .unwrap_or(&"No message".to_string())
        .to_string()
}

pub(crate) fn open_config_file() -> Config {
    if let Ok(c) = std::fs::read_to_string(CFGPATH_BPFMAN_CONFIG) {
        c.parse().unwrap_or_else(|_| {
            warn!("Unable to parse config file, using defaults");
            Config::default()
        })
    } else {
        warn!("Unable to read config file, using defaults");
        Config::default()
    }
}
