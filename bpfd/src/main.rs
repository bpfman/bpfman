// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{
    env,
    fs::{create_dir_all, File},
    io::{BufRead, BufReader},
    str::FromStr,
};

use anyhow::{bail, Context};
use aya::include_bytes_aligned;
use bpfd::server::{config_from_file, programs_from_directory, serve};
use log::{debug, error, info};
use nix::{
    libc::RLIM_INFINITY,
    mount::{mount, MsFlags},
    sys::resource::{setrlimit, Resource},
};
use systemd_journal_logger::{connected_to_journal, init_with_extra_fields};

const DEFAULT_BPFD_CONFIG_PATH: &str = "/etc/bpfd/bpfd.toml";
const DEFAULT_BPFD_STATIC_PROGRAM_DIR: &str = "/etc/bpfd/programs.d";
const BPFD_ENV_LOG_LEVEL: &str = "RUST_LOG";
const BPFFS: &str = "/var/run/bpfd/fs";

fn main() -> anyhow::Result<()> {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .on_thread_start(|| {
            drop_linux_capabilities();
        })
        .build()
        .unwrap()
        .block_on(async {
            if connected_to_journal() {
                // If bpfd is running as a service, log to journald.
                init_with_extra_fields(vec![("VERSION", env!("CARGO_PKG_VERSION"))]).unwrap();
                manage_journal_log_level();
                log::info!("Log using journald");
            } else {
                // Otherwise fall back to logging to standard error.
                env_logger::init();
                log::info!("Log using env_logger");
            }

            has_cap(caps::CapSet::Effective, caps::Capability::CAP_BPF);
            has_cap(caps::CapSet::Effective, caps::Capability::CAP_SYS_ADMIN);

            let dispatcher_bytes_xdp = include_bytes_aligned!(
                "../../target/bpfel-unknown-none/release/xdp_dispatcher.bpf.o"
            );
            let dispatcher_bytes_tc = include_bytes_aligned!(
                "../../target/bpfel-unknown-none/release/tc_dispatcher.bpf.o"
            );
            setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

            create_dir_all(BPFFS).context("unable to create mountpoint")?;
            create_dir_all("/var/run/bpfd/programs")?;
            create_dir_all("/var/run/bpfd/dispatchers")?;

            if !is_bpffs_mounted()? {
                debug!("Creating bpffs at /var/run/bpfd/fs");
                let flags = MsFlags::MS_NOSUID
                    | MsFlags::MS_NODEV
                    | MsFlags::MS_NOEXEC
                    | MsFlags::MS_RELATIME;
                mount::<str, str, str, str>(None, BPFFS, Some("bpf"), flags, None)
                    .context("unable to mount bpffs")?;
            }

            let config = config_from_file(DEFAULT_BPFD_CONFIG_PATH);

            let static_programs = programs_from_directory(DEFAULT_BPFD_STATIC_PROGRAM_DIR)?;

            serve(
                config,
                dispatcher_bytes_xdp,
                dispatcher_bytes_tc,
                static_programs,
            )
            .await?;
            Ok(())
        })
}

fn manage_journal_log_level() {
    // env_logger uses the environment variable RUST_LOG to set the log
    // level. Parse RUST_LOG to set the log level for journald.
    log::set_max_level(log::LevelFilter::Error);
    if env::var(BPFD_ENV_LOG_LEVEL).is_ok() {
        let rust_log = log::LevelFilter::from_str(&env::var(BPFD_ENV_LOG_LEVEL).unwrap());
        match rust_log {
            Ok(value) => log::set_max_level(value),
            Err(e) => log::error!("Invalid Log Level: {}", e),
        }
    }
}

fn drop_all_cap(cap: caps::CapSet) {
    match caps::clear(None, cap) {
        Ok(()) => debug!("CAPS:  {:?} Cleared", cap),
        Err(e) => error!("CAPS:  Clear {:?} Error  {}", cap, e),
    }
}

fn has_cap(cset: caps::CapSet, cap: caps::Capability) {
    info!("Has {}: {}", cap, caps::has_cap(None, cset, cap).unwrap());
}

fn drop_linux_capabilities() {
    drop_all_cap(caps::CapSet::Ambient);
    drop_all_cap(caps::CapSet::Bounding);
    drop_all_cap(caps::CapSet::Effective);
    drop_all_cap(caps::CapSet::Inheritable);
    drop_all_cap(caps::CapSet::Permitted);
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
                if parts[0] == "none" && parts[1].contains("bpfd") && parts[2] == "bpf" {
                    return Ok(true);
                }
            }
            Err(e) => bail!("problem reading lines {}", e),
        }
    }
    Ok(false)
}
