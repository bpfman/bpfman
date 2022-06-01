use aya::include_bytes_aligned;
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
    bpfd::serve(dispatcher_bytes).await?;
    Ok(())
}
