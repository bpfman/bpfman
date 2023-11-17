use aya::loaded_programs;
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
                bpf_program_size_jitted_bytes.as_any(),
                bpf_program_size_translated_bytes.as_any(),
                bpf_program_mem_bytes.as_any(),
                bpf_program_verified_instructions_total.as_any(),
            ],
            move |observer| {
                for program in loaded_programs().flatten() {
                    let name = program.name_as_str().unwrap_or_default().to_string();
                    let ty = program.program_type().to_string();
                    let tag = program.tag().to_string();
                    let gpl_compatible = program.gpl_compatible();
                    let load_time = DateTime::<Utc>::from(program.loaded_at());

                    let jitted_bytes = program.size_jitted();
                    let translated_bytes = program.size_translated();
                    let mem_bytes = program.memory_locked().unwrap_or_default();
                    let verified_instructions = program.verified_instruction_count();

                    let labels = [
                        KeyValue::new("name", name),
                        KeyValue::new("type", ty),
                        KeyValue::new("tag", tag),
                        KeyValue::new("gpl_compatible", gpl_compatible),
                        KeyValue::new(
                            "load_time",
                            load_time.format("%Y-%m-%d %H:%M:%S").to_string(),
                        ),
                    ];

                    observer.observe_u64(
                        &bpf_program_size_jitted_bytes,
                        jitted_bytes.into(),
                        &labels,
                    );

                    observer.observe_u64(
                        &bpf_program_size_translated_bytes,
                        translated_bytes.into(),
                        &labels,
                    );

                    observer.observe_u64(&bpf_program_mem_bytes, mem_bytes.into(), &labels);

                    observer.observe_u64(
                        &bpf_program_verified_instructions_total,
                        verified_instructions.into(),
                        &labels,
                    );
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
