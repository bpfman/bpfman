// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    fs,
    fs::{create_dir_all, set_permissions, File, OpenOptions},
    io::{BufRead, BufReader, Read},
    option::Option,
    os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt},
    path::{Path, PathBuf},
    process,
};

use anyhow::{anyhow, bail, Context, Result};
use log::{debug, info, warn};
use nix::{
    libc::RLIM_INFINITY,
    mount::{mount, MsFlags},
    net::if_::if_nametoindex,
    sched::{setns, CloneFlags},
    sys::resource::{setrlimit, Resource},
};
use sled::{IVec, Tree};

use crate::{
    config::Config,
    directories::*,
    errors::BpfmanError,
    multiprog::{TC_DISPATCHER_PREFIX, XDP_DISPATCHER_PREFIX},
    types::Direction,
};

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

pub(crate) fn get_ifindex(iface: &str, netns: Option<PathBuf>) -> Result<u32, BpfmanError> {
    debug!("Getting ifindex for iface: {}", iface);
    let ifindex_result = if let Some(netns) = netns {
        let _netns_guard = enter_netns(netns)?;
        if_nametoindex(iface)
    } else {
        if_nametoindex(iface)
    };
    debug!("ifindex result for iface: {} = {:?}", iface, ifindex_result);

    match ifindex_result {
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

pub(crate) fn id_from_tree_name(name: &IVec) -> Result<u32, BpfmanError> {
    let id = bytes_to_string(name)
        .split('_')
        .last()
        .ok_or_else(|| BpfmanError::InvalidTreeName(bytes_to_string(name)))?
        .parse::<u32>()
        .map_err(|_| BpfmanError::InvalidTreeName(bytes_to_string(name)))?;

    Ok(id)
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
        if let Ok(config) = c.parse::<Config>() {
            config
        } else {
            warn!("Unable to parse config file, using defaults");
            Config::default()
        }
    } else {
        debug!("Unable to read config file, using defaults");
        Config::default()
    }
}

fn has_cap(cset: caps::CapSet, cap: caps::Capability) {
    debug!("Has {}: {}", cap, caps::has_cap(None, cset, cap).unwrap());
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

    if setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).is_err() {
        return Err(anyhow!("must be privileged to run bpfman"));
    }

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

pub(crate) struct NetnsGuard {
    original_ns: File,
}

impl Drop for NetnsGuard {
    fn drop(&mut self) {
        // Switch back to the original network namespace
        debug!("Switching back to the original network namespace");
        setns(&self.original_ns, CloneFlags::CLONE_NEWNET)
            .expect("Failed to switch back to the original namespace");
    }
}

pub(crate) fn enter_netns(netns: PathBuf) -> Result<NetnsGuard, BpfmanError> {
    let bpfman_netns_file = File::open(format!("/proc/{}/ns/net", process::id()))
        .map_err(|e| BpfmanError::Error(format!("Failed to open bpfman netns file: {e}")))?;

    let target_netns_file = File::open(netns)
        .map_err(|e| BpfmanError::Error(format!("Failed to open target netns file: {e}")))?;

    // Switch to the target network namespace
    debug!("Switching to the target network namespace");
    setns(target_netns_file, CloneFlags::CLONE_NEWNET)
        .map_err(|e| BpfmanError::Error(format!("setns error: {}", e)))?;

    Ok(NetnsGuard {
        original_ns: bpfman_netns_file,
    })
}

fn nsid(ns_path: Option<PathBuf>) -> Result<u64, BpfmanError> {
    let path = if let Some(p) = ns_path {
        p
    } else {
        PathBuf::from("/proc/self/ns/net")
    };

    let metadata = fs::metadata(path).map_err(|e| {
        BpfmanError::Error(format!("Failed to get metadata for namespace path: {e}"))
    })?;

    Ok(metadata.ino())
}

pub(crate) fn xdp_dispatcher_id(
    netns: Option<PathBuf>,
    if_index: u32,
) -> Result<String, BpfmanError> {
    let nsid = nsid(netns)?;
    Ok(format!("{}_{}_{}", XDP_DISPATCHER_PREFIX, nsid, if_index))
}

pub(crate) fn xdp_dispatcher_db_tree_name(
    netns: Option<PathBuf>,
    if_index: u32,
    revision: u32,
) -> Result<String, BpfmanError> {
    Ok(format!(
        "{}_{}",
        xdp_dispatcher_id(netns.clone(), if_index)?,
        revision
    ))
}

pub(crate) fn xdp_dispatcher_rev_path(
    netns: Option<PathBuf>,
    if_index: u32,
    revision: u32,
) -> Result<String, BpfmanError> {
    let nsid = nsid(netns)?;
    Ok(format!(
        "{}/dispatcher_{}_{}_{}",
        RTDIR_FS_XDP, nsid, if_index, revision
    ))
}

pub(crate) fn xdp_dispatcher_link_path(
    netns: Option<PathBuf>,
    if_index: u32,
) -> Result<String, BpfmanError> {
    let nsid = nsid(netns)?;
    Ok(format!(
        "{}/dispatcher_{}_{}_link",
        RTDIR_FS_XDP, nsid, if_index
    ))
}

pub(crate) fn xdp_dispatcher_link_id_path(
    netns: Option<PathBuf>,
    if_index: u32,
    revision: u32,
    id: u32,
) -> Result<String, BpfmanError> {
    Ok(format!(
        "{}/link_{}",
        xdp_dispatcher_rev_path(netns, if_index, revision)?,
        id
    ))
}

pub(crate) fn tc_dispatcher_id(
    netns: Option<PathBuf>,
    if_index: u32,
    direction: Direction,
) -> Result<String, BpfmanError> {
    let nsid = nsid(netns)?;
    Ok(format!(
        "{}_{}_{}_{}",
        TC_DISPATCHER_PREFIX, nsid, if_index, direction
    ))
}

pub(crate) fn tc_dispatcher_db_tree_name(
    netns: Option<PathBuf>,
    if_index: u32,
    direction: Direction,
    revision: u32,
) -> Result<String, BpfmanError> {
    Ok(format!(
        "{}_{}",
        tc_dispatcher_id(netns.clone(), if_index, direction)?,
        revision
    ))
}

pub(crate) fn tc_dispatcher_rev_path(
    direction: Direction,
    netns: Option<PathBuf>,
    if_index: u32,
    revision: u32,
) -> Result<String, BpfmanError> {
    let tc_base = match direction {
        Direction::Ingress => RTDIR_FS_TC_INGRESS,
        Direction::Egress => RTDIR_FS_TC_EGRESS,
    };
    let nsid = nsid(netns)?;
    Ok(format!(
        "{}/dispatcher_{}_{}_{}",
        tc_base, nsid, if_index, revision
    ))
}

pub(crate) fn tc_dispatcher_link_id_path(
    direction: Direction,
    netns: Option<PathBuf>,
    if_index: u32,
    revision: u32,
    id: u32,
) -> Result<String, BpfmanError> {
    Ok(format!(
        "{}/link_{}",
        tc_dispatcher_rev_path(direction, netns, if_index, revision)?,
        id
    ))
}
