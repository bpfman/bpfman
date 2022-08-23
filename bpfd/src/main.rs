// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{env, str::FromStr};

use aya::include_bytes_aligned;
use bpfd::server::{config_from_file, programs_from_directory, serve};
use log::{debug, error};
use nix::{
    libc::RLIM_INFINITY,
    sys::resource::{setrlimit, Resource},
    unistd,
};
use systemd_journal_logger::{connected_to_journal, init_with_extra_fields};

const DEFAULT_BPFD_CONFIG_PATH: &str = "/etc/bpfd/bpfd.toml";
const DEFAULT_BPFD_STATIC_PROGRAM_DIR: &str = "/etc/bpfd/programs.d";
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

            let dispatcher_bytes = include_bytes_aligned!(
                "../../target/bpfel-unknown-none/release/xdp_dispatcher.bpf.o"
            );
            setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

            let config = config_from_file(DEFAULT_BPFD_CONFIG_PATH);

            let static_programs = programs_from_directory(DEFAULT_BPFD_STATIC_PROGRAM_DIR)?;

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

fn drop_linux_capabilities() {
    debug!(
        "CAPS: PID {:?} TID {:?} User {:?}",
        unistd::getpid(),
        unistd::gettid(),
        unistd::getuid()
    );
    drop_all_cap(caps::CapSet::Ambient);
    drop_all_cap(caps::CapSet::Bounding);
    drop_all_cap(caps::CapSet::Effective);
    drop_all_cap(caps::CapSet::Inheritable);
    drop_all_cap(caps::CapSet::Permitted);
}
