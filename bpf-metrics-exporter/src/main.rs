// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::time::SystemTime;

use aya::{
    maps::{loaded_maps, MapType as AyaMapType},
    programs::{loaded_links, loaded_programs, ProgramType as AyaProgramType},
};
use bpfman::types::{MapType, ProgramType};
use chrono::{prelude::DateTime, Utc};
use clap::Parser;
use opentelemetry::{
    metrics::{MeterProvider as _, Unit},
    KeyValue,
};
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::{metrics::SdkMeterProvider, runtime, Resource};
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

    // GAUGE instruments:

    let bpf_program_info = meter
        .u64_observable_gauge("bpf_program_info")
        .with_description("eBPF Program metadata")
        .init();

    let bpf_map_info = meter
        .u64_observable_gauge("bpf_map_info")
        .with_description("eBPF Map metadata")
        .init();

    let bpf_link_info = meter
        .u64_observable_gauge("bpf_link_info")
        .with_description("eBPF Link metadata")
        .init();

    let bpf_program_load_time = meter
        .i64_observable_gauge("bpf_program_load_time")
        .with_description("BPF program load time")
        .with_unit(Unit::new("seconds"))
        .init();

    // COUNTER instruments:

    let bpf_program_size_translated_bytes = meter
        .u64_observable_counter("bpf_program_size_translated_bytes")
        .with_description("BPF program size in bytes")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_program_size_jitted_bytes = meter
        .u64_observable_counter("bpf_program_size_jitted_bytes")
        .with_description("The size in bytes of the program's JIT-compiled machine code.")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_program_mem_bytes = meter
        .u64_observable_counter("bpf_program_mem_bytes")
        .with_description("BPF program memory usage in bytes")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_program_verified_instructions = meter
        .u64_observable_counter("bpf_program_verified_instructions")
        .with_description("BPF program verified instructions")
        .with_unit(Unit::new("instructions"))
        .init();

    let bpf_map_key_size = meter
        .u64_observable_counter("bpf_map_key_size")
        .with_description("BPF map key size")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_map_value_size = meter
        .u64_observable_counter("bpf_map_value_size")
        .with_description("BPF map value size")
        .with_unit(Unit::new("bytes"))
        .init();

    let bpf_map_max_entries = meter
        .u64_observable_counter("bpf_map_max_entries")
        .with_description("BPF map maxiumum number of entries")
        .with_unit(Unit::new("bytes"))
        .init();

    meter
        .register_callback(
            &[
                bpf_program_info.as_any(),
                bpf_map_info.as_any(),
                bpf_link_info.as_any(),
                bpf_program_load_time.as_any(),
                bpf_program_size_jitted_bytes.as_any(),
                bpf_program_size_translated_bytes.as_any(),
                bpf_program_mem_bytes.as_any(),
                bpf_program_verified_instructions.as_any(),
                bpf_map_key_size.as_any(),
                bpf_map_value_size.as_any(),
                bpf_map_max_entries.as_any(),
            ],
            move |observer| {
                for program in loaded_programs().flatten() {
                    let id = program.id();
                    let name = program.name_as_str().unwrap_or_default().to_string();
                    let ty: ProgramType = ProgramType::from(
                        program
                            .program_type()
                            .unwrap_or(AyaProgramType::Unspecified),
                    );
                    let tag = program.tag().to_string();
                    let gpl_compatible = program.gpl_compatible().unwrap_or(false);
                    let map_ids = program.map_ids().unwrap_or_default().unwrap_or_default();
                    let load_time = DateTime::<Utc>::from(
                        program.loaded_at().unwrap_or(SystemTime::UNIX_EPOCH),
                    );
                    let jitted_bytes = program.size_jitted();
                    let translated_bytes = program.size_translated().unwrap_or(0);
                    let mem_bytes = program.memory_locked().unwrap_or(0);
                    let verified_instructions = program.verified_instruction_count().unwrap_or(0);

                    let prog_info_labels = [
                        KeyValue::new("id", id.to_string()),
                        KeyValue::new("name", name.clone()),
                        KeyValue::new("type", format!("{ty}")),
                        KeyValue::new("tag", tag.clone()),
                        KeyValue::new("gpl_compatible", gpl_compatible),
                        KeyValue::new("map_ids", format!("{map_ids:?}")),
                        KeyValue::new(
                            "load_time",
                            load_time.format("%Y-%m-%d %H:%M:%S").to_string(),
                        ),
                    ];

                    observer.observe_u64(&bpf_program_info, 1, &prog_info_labels);

                    let prog_key_labels = [
                        KeyValue::new("id", id.to_string()),
                        KeyValue::new("name", name),
                        KeyValue::new("type", format!("{ty}")),
                    ];

                    observer.observe_i64(
                        &bpf_program_load_time,
                        load_time.timestamp(),
                        &prog_key_labels,
                    );

                    observer.observe_u64(
                        &bpf_program_size_jitted_bytes,
                        jitted_bytes.into(),
                        &prog_key_labels,
                    );

                    observer.observe_u64(
                        &bpf_program_size_translated_bytes,
                        translated_bytes.into(),
                        &prog_key_labels,
                    );

                    observer.observe_u64(
                        &bpf_program_mem_bytes,
                        mem_bytes.into(),
                        &prog_key_labels,
                    );

                    observer.observe_u64(
                        &bpf_program_verified_instructions,
                        verified_instructions.into(),
                        &prog_key_labels,
                    );
                }

                for link in loaded_links().flatten() {
                    let id = link.id;
                    let prog_id = link.prog_id;
                    let _type = link.type_;
                    // TODO getting more link metadata will require an aya_patch
                    // let link_info = match link.__bindgen_anon_1 {
                    //     // aya_obj::bpf_link_info__bindgen_ty_1::raw_tracepoint(i) => "tracepoint",
                    //     aya_obj::generated::bpf_link_info__bindgen_ty_1{ raw_tracepoint } => format!("tracepoint name: "),
                    // }

                    let link_labels = [
                        KeyValue::new("id", id.to_string()),
                        KeyValue::new("prog_id", prog_id.to_string()),
                        KeyValue::new("type", _type.to_string()),
                    ];

                    observer.observe_u64(&bpf_link_info, 1, &link_labels);
                }

                for map in loaded_maps().flatten() {
                    let map_id = map.id();
                    let name = map.name_as_str().unwrap_or_default().to_string();
                    let ty = MapType::from(map.map_type().unwrap_or(AyaMapType::Unspecified))
                        .to_string();
                    let key_size = map.key_size();
                    let value_size = map.value_size();
                    let max_entries = map.max_entries();
                    let flags = map.map_flags();

                    let map_labels = [
                        KeyValue::new("name", name.clone()),
                        KeyValue::new("id", map_id.to_string()),
                        KeyValue::new("type", ty.clone()),
                        KeyValue::new("key_size", key_size.to_string()),
                        KeyValue::new("value_size", value_size.to_string()),
                        KeyValue::new("max_entries", max_entries.to_string()),
                        KeyValue::new("flags", flags.to_string()),
                    ];

                    observer.observe_u64(&bpf_map_info, 1, &map_labels);

                    let map_key_labels = [
                        KeyValue::new("name", name),
                        KeyValue::new("id", map_id.to_string()),
                        KeyValue::new("type", ty),
                    ];

                    observer.observe_u64(&bpf_map_key_size, key_size.into(), &map_key_labels);

                    observer.observe_u64(&bpf_map_value_size, value_size.into(), &map_key_labels);

                    observer.observe_u64(&bpf_map_max_entries, max_entries.into(), &map_key_labels);
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
