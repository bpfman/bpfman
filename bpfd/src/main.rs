// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use aya::include_bytes_aligned;
use bpfd::server::{config_from_file, programs_from_directory, serve};
use nix::{
    libc::RLIM_INFINITY,
    sys::resource::{setrlimit, Resource},
};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    env_logger::init();
    let dispatcher_bytes =
        include_bytes_aligned!("../../target/bpfel-unknown-none/release/xdp_dispatcher.bpf.o");
    setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

    let config = config_from_file("/etc/bpfd.toml");

    let static_programs = programs_from_directory("/etc/bpfd/programs.d")?;

    serve(config, dispatcher_bytes, static_programs).await?;
    Ok(())
}
