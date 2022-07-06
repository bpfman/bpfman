// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use aya::include_bytes_aligned;
use bpfd::server::{config_from_file, serve};
use nix::{
    libc::RLIM_INFINITY,
    sys::resource::{setrlimit, Resource},
};
use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    TermLogger::init(
        LevelFilter::Debug,
        ConfigBuilder::new()
            .set_target_level(LevelFilter::Error)
            .set_location_level(LevelFilter::Error)
            .add_filter_ignore("h2".to_string())
            .add_filter_ignore("rustls".to_string())
            .add_filter_ignore("hyper".to_string())
            .add_filter_ignore("aya".to_string())
            .build(),
        TerminalMode::Mixed,
        ColorChoice::Auto,
    )?;
    #[cfg(debug_assertions)]
    let dispatcher_bytes =
        include_bytes_aligned!("../../target/bpfel-unknown-none/debug/xdp_dispatcher.bpf.o");
    #[cfg(not(debug_assertions))]
    let dispatcher_bytes =
        include_bytes_aligned!("../../target/bpfel-unknown-none/release/xdp-dispatcher");
    setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

    let config = config_from_file("/etc/bpfd.toml");
    serve(config, dispatcher_bytes).await?;
    Ok(())
}
