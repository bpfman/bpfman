use aya::{loaded_programs, maps::loaded_maps, programs::loaded_links};
use bpfman_api::ProgramType;
use chrono::{prelude::DateTime, Utc};
use clap::Parser;
use opentelemetry::{
    metrics::{MeterProvider as _, Unit},
    KeyValue,
};
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::{metrics::MeterProvider as SdkMeterProvider, runtime, Resource};
use tokio::signal::ctrl_c;

fn init_meter_provider(grpc_endpoint: &str) -> SdkMeterProvider {
    opentelemetry_otlp::new_pipeline()
        .metrics(runtime::Tokio)
        .with_exporter(
            opentelemetry_otlp::new_exporter()
                .tonic()
                .with_endpoint(grpc_endpoint),
        )
        .with_resource(Resource::new(vec![KeyValue::new(
            opentelemetry_semantic_conventions::resource::SERVICE_NAME,
            "bpf-metrics-exporter",
        )]))
        .with_period(std::time::Duration::from_secs(5))
        .build()
        .expect("unable to create a new provider")
}

#[derive(Parser)]
struct Cli {
    #[clap(long, default_value = "http://localhost:4317")]
    otel_grpc: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();

    // Initialize the MeterProvider with the OTLP exporter.
    let meter_provider = init_meter_provider(&cli.otel_grpc);

    // Create a meter from the above MeterProvider.
    let meter = meter_provider.meter("bpf-metrics");

    // TODO(astoycos) add nodename label to these gauges
    let bpf_programs = meter
        .u64_observable_gauge("bpf_programs")
        .with_description("The total number of eBPF Programs on a node")
        .with_unit(Unit::new("programs"))
        .init();

    let bpf_maps = meter
        .u64_observable_gauge("bpf_maps")
        .with_description("The total number of eBPF Maps on a node")
        .with_unit(Unit::new("maps"))
        .init();

    let bpf_links = meter
        .u64_observable_gauge("bpf_links")
        .with_description("The total number of eBPF links on a node")
        .with_unit(Unit::new("links"))
        .init();

    let bpf_program_size_jitted_bytes = meter
        .u64_observable_counter("bpf_program_size_jitted_bytes")
        .with_description("BPF program size in bytes")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_program_size_translated_bytes = meter
        .u64_observable_counter("bpf_program_size_translated_bytes")
        .with_description("BPF program size in bytes")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_program_mem_bytes = meter
        .u64_observable_counter("bpf_program_mem_bytes")
        .with_description("BPF program memory usage in bytes")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_program_verified_instructions_total = meter
        .u64_observable_counter("bpf_program_verified_instructions_total")
        .with_description("BPF program verified instructions")
        .with_unit(Unit::new("instructions"))
        .init();

    meter
        .register_callback(
            &[
                bpf_programs.as_any(),
                bpf_maps.as_any(),
                bpf_links.as_any(),
                bpf_program_size_jitted_bytes.as_any(),
                bpf_program_size_translated_bytes.as_any(),
                bpf_program_mem_bytes.as_any(),
                bpf_program_verified_instructions_total.as_any(),
            ],
            move |observer| {
                for program in loaded_programs().flatten() {
                    let id = program.id();
                    let name = program.name_as_str().unwrap_or_default().to_string();
                    let ty: ProgramType = program.program_type().try_into().unwrap();
                    let tag = program.tag().to_string();
                    let gpl_compatible = program.gpl_compatible();
                    let load_time = DateTime::<Utc>::from(program.loaded_at());
                    let map_ids = program.map_ids().unwrap_or_default();

                    let jitted_bytes = program.size_jitted();
                    let translated_bytes = program.size_translated();
                    let mem_bytes = program.memory_locked().unwrap_or_default();
                    let verified_instructions = program.verified_instruction_count();

                    let prog_labels = [
                        KeyValue::new("id", id.to_string()),
                        KeyValue::new("name", name),
                        KeyValue::new("type", format!("{ty}")),
                        KeyValue::new("tag", tag),
                        KeyValue::new("gpl_compatible", gpl_compatible),
                        KeyValue::new(
                            "load_time",
                            load_time.format("%Y-%m-%d %H:%M:%S").to_string(),
                        ),
                        KeyValue::new("map_ids", format!("{map_ids:?}")),
                    ];

                    observer.observe_u64(&bpf_programs, 1, &prog_labels);

                    observer.observe_u64(&bpf_program_size_jitted_bytes, jitted_bytes.into(), &[]);

                    observer.observe_u64(
                        &bpf_program_size_translated_bytes,
                        translated_bytes.into(),
                        &[],
                    );

                    observer.observe_u64(&bpf_program_mem_bytes, mem_bytes.into(), &[]);

                    observer.observe_u64(
                        &bpf_program_verified_instructions_total,
                        verified_instructions.into(),
                        &[],
                    );
                }

                for link in loaded_links().flatten() {
                    let id = link.id;
                    let prog_id = link.prog_id;
                    let _type = link.type_;
                    // TODO this really needs an aya_patch
                    // let link_info = match link.__bindgen_anon_1 {
                    //     // aya_obj::bpf_link_info__bindgen_ty_1::raw_tracepoint(i) => "tracepoint",
                    //     aya_obj::generated::bpf_link_info__bindgen_ty_1{ raw_tracepoint } => format!("tracepoint name: "),
                    // }

                    let link_labels = [
                        KeyValue::new("id", id.to_string()),
                        KeyValue::new("prog_id", prog_id.to_string()),
                        KeyValue::new("type", _type.to_string()),
                    ];

                    observer.observe_u64(&bpf_links, 1, &link_labels);
                }

                for map in loaded_maps().flatten() {
                    let map_id = map.id();
                    let name = map.name_as_str().unwrap_or_default().to_string();
                    let ty = map.map_type().to_string();
                    let key_size = map.key_size();
                    let value_size = map.value_size();
                    let max_entries = map.max_entries();
                    let flags = map.map_flags();

                    let map_labels = [
                        KeyValue::new("name", name),
                        KeyValue::new("id", map_id.to_string()),
                        KeyValue::new("type", ty),
                        KeyValue::new("key_size", key_size.to_string()),
                        KeyValue::new("value_size", value_size.to_string()),
                        KeyValue::new("max_entries", max_entries.to_string()),
                        KeyValue::new("flags", flags.to_string()),
                    ];

                    observer.observe_u64(&bpf_maps, 1, &map_labels);
                }
            },
        )
        .expect("failed to register callback");

    println!("Listening for Ctrl-C...");
    ctrl_c().await.expect("failed to listen for event");
    println!("Ctrl-C received, shutting down...");

    // Explicitly shutdown the provider to flush any remaining metrics data.
    // There is no guarantee that this will be successful, so ignore the error.
    let _ = meter_provider.shutdown();

    Ok(())
}
