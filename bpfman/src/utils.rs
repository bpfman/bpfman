// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    fs::{create_dir_all, set_permissions, File, OpenOptions},
    io::{BufRead, BufReader, Read},
    os::unix::fs::{OpenOptionsExt, PermissionsExt},
    path::Path,
};

use anyhow::{bail, Context, Result};
use log::{debug, info, warn};
use nix::{
    libc::RLIM_INFINITY,
    mount::{mount, MsFlags},
    net::if_::if_nametoindex,
    sys::resource::{setrlimit, Resource},
};
use sled::Tree;

use crate::{config::Config, directories::*, errors::BpfmanError};

// The bpfman socket should always allow the same users and members of the same group
// to Read/Write to it.
pub const SOCK_MODE: u32 = 0o0660;

// Like tokio::fs::read, but with O_NOCTTY set
pub(crate) fn read<P: AsRef<Path>>(path: P) -> Result<Vec<u8>, BpfmanError> {
    let mut data = vec![];
    OpenOptions::new()
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

pub fn set_file_permissions(path: &Path, mode: u32) {
    // Set the permissions on the file based on input
    if (set_permissions(path, std::fs::Permissions::from_mode(mode))).is_err() {
        debug!(
            "Unable to set permissions on file {}. Continuing",
            path.to_path_buf().display()
        );
    }
}

pub fn set_dir_permissions(directory: &str, mode: u32) {
    // Iterate through the files in the provided directory
    let entries = std::fs::read_dir(directory).unwrap();
    for file in entries.flatten() {
        // Set the permissions on the file based on input
        set_file_permissions(&file.path(), mode);
    }
}

pub fn create_bpffs(directory: &str) -> anyhow::Result<()> {
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

fn has_cap(cset: caps::CapSet, cap: caps::Capability) {
    info!("Has {}: {}", cap, caps::has_cap(None, cset, cap).unwrap());
}

fn is_bpffs_mounted() -> Result<bool, anyhow::Error> {
    let file = File::open("/proc/mounts").context("Failed to open /proc/mounts")?;
    let reader = BufReader::new(file);
    for l in reader.lines() {
        match l {
            Ok(line) => {
                let parts: Vec<&str> = line.split(' ').collect();
                if parts.len() != 6 {
                    bail!("expected 6 parts in proc mount")
                }
                if parts[0] == "none" && parts[1].contains("bpfman") && parts[2] == "bpf" {
                    return Ok(true);
                }
            }
            Err(e) => bail!("problem reading lines {}", e),
        }
    }
    Ok(false)
}

pub(crate) fn initialize_bpfman() -> anyhow::Result<()> {
    has_cap(caps::CapSet::Effective, caps::Capability::CAP_BPF);
    has_cap(caps::CapSet::Effective, caps::Capability::CAP_SYS_ADMIN);

    setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

    // Create directories associated with bpfman
    create_dir_all(RTDIR).context("unable to create runtime directory")?;
    create_dir_all(RTDIR_FS).context("unable to create mountpoint")?;
    create_dir_all(RTDIR_TC_INGRESS_DISPATCHER).context("unable to create dispatcher directory")?;
    create_dir_all(RTDIR_TC_EGRESS_DISPATCHER).context("unable to create dispatcher directory")?;
    create_dir_all(RTDIR_XDP_DISPATCHER).context("unable to create dispatcher directory")?;
    create_dir_all(RTDIR_PROGRAMS).context("unable to create programs directory")?;

    if !is_bpffs_mounted()? {
        create_bpffs(RTDIR_FS)?;
    }
    create_dir_all(RTDIR_FS_XDP).context("unable to create xdp dispatcher directory")?;
    create_dir_all(RTDIR_FS_TC_INGRESS)
        .context("unable to create tc ingress dispatcher directory")?;
    create_dir_all(RTDIR_FS_TC_EGRESS)
        .context("unable to create tc egress dispatcher directory")?;
    create_dir_all(RTDIR_FS_MAPS).context("unable to create maps directory")?;
    create_dir_all(RTDIR_TUF).context("unable to create TUF directory")?;

    create_dir_all(STDIR).context("unable to create state directory")?;

    create_dir_all(CFGDIR_STATIC_PROGRAMS).context("unable to create static programs directory")?;

    set_dir_permissions(CFGDIR, CFGDIR_MODE);
    set_dir_permissions(RTDIR, RTDIR_MODE);
    set_dir_permissions(STDIR, STDIR_MODE);

    Ok(())
}
