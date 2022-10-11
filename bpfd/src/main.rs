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
use bpfd_api::util::directories::*;
use log::{debug, error, info};
use nix::{
    libc::RLIM_INFINITY,
    mount::{mount, MsFlags},
    sys::resource::{setrlimit, Resource},
    unistd::{getuid, User},
};
use systemd_journal_logger::{connected_to_journal, init_with_extra_fields};

const BPFD_ENV_LOG_LEVEL: &str = "RUST_LOG";

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

            let dispatcher_bytes = include_bytes_aligned!(
                "../../target/bpfel-unknown-none/release/xdp_dispatcher.bpf.o"
            );
            setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

            create_dir_all(RTDIR).context("unable to create runtime directory")?;
            create_dir_all(RTDIR_FS).context("unable to create mountpoint")?;
            create_dir_all(RTDIR_FS_MAPS).context("unable to create maps directory")?;
            create_dir_all(RTDIR_BYTECODE).context("unable to create bytecode directory")?;
            create_dir_all(RTDIR_DISPATCHER).context("unable to create dispatcher directory")?;
            create_dir_all(RTDIR_PROGRAMS).context("unable to create programs directory")?;

            if !is_bpffs_mounted()? {
                debug!("Creating bpffs at {}", RTDIR_FS);
                let flags = MsFlags::MS_NOSUID
                    | MsFlags::MS_NODEV
                    | MsFlags::MS_NOEXEC
                    | MsFlags::MS_RELATIME;
                mount::<str, str, str, str>(None, RTDIR_FS, Some("bpf"), flags, None)
                    .context("unable to mount bpffs")?;
            }

            create_dir_all(CFGDIR_BPFD_CERTS).context("unable to create bpfd certs directory")?;
            create_dir_all(CFGDIR_CA_CERTS).context("unable to create ca certs directory")?;
            create_dir_all(CFGDIR_STATIC_PROGRAMS)
                .context("unable to create static programs directory")?;

            create_dir_all(STDIR_SOCKET).context("unable to create socket directory")?;

            let config = config_from_file(CFGPATH_BPFD_CONFIG);

            let static_programs = programs_from_directory(CFGDIR_STATIC_PROGRAMS)?;

            serve(config, dispatcher_bytes, static_programs).await?;
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
    let res = User::from_uid(getuid()).unwrap().unwrap();
    if res.name == "bpfd" {
        debug!(
            "Running as user={}, dropping all capabilities for spawned threads",
            res.name
        );
        drop_all_cap(caps::CapSet::Ambient);
        drop_all_cap(caps::CapSet::Bounding);
        drop_all_cap(caps::CapSet::Effective);
        drop_all_cap(caps::CapSet::Inheritable);
        drop_all_cap(caps::CapSet::Permitted);
    } else {
        debug!(
            "Running as user={}, skip dropping all capabilities for spawned threads",
            res.name
        );
    }
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
