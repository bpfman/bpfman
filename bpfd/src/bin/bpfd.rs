use aya::include_bytes_aligned;
use bpfd::config_from_file;
use nix::{sys::resource::{setrlimit, Resource}, libc::RLIM_INFINITY};
use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    TermLogger::init(
        LevelFilter::Debug,
        ConfigBuilder::new()
            .set_target_level(LevelFilter::Error)
            .set_location_level(LevelFilter::Error)
            .add_filter_ignore("h2".to_string())
            .add_filter_ignore("aya".to_string())
            .build(),
        TerminalMode::Mixed,
        ColorChoice::Auto,
    )?;
    let dispatcher_bytes =
        include_bytes_aligned!("../../../bpfd-ebpf/.output/xdp_dispatcher.bpf.o");

    setrlimit(Resource::RLIMIT_MEMLOCK, RLIM_INFINITY, RLIM_INFINITY).unwrap();

    let config = config_from_file("/etc/bpfd.toml");
    bpfd::serve(config, dispatcher_bytes).await?;
    Ok(())
}
