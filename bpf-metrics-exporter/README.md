# bpf-metrics-exporter

Exports metrics from the kernel's BPF subsystem to OpenTelmetry.
These can later be enriched with other metrics from the system, for example,
to correlate process IDs -> containers -> k8s pods.

## Usage

```bash
./bpf-metrics-exporter --otlp-grpc localhost:4317
```

## Metrics

The following metrics are exported:

- `bpf_program_size_jitted_bytes`: The size of the BPF program in bytes.
- `bpf_program_size_translated_bytes`: The size of the BPF program in bytes.
- `bpf_program_mem_bytes`: The amount of memory used by the BPF program in bytes.
- `bpf_program_verified_instructions_total`: The number of instructions in the BPF program.


The following will be added pending: https://github.com/open-telemetry/opentelemetry-rust/issues/1242

- `bpf_programs_total`: The number of BPF programs loaded into the kernel.

Labels:

- `id`: The ID of the BPF program
- `name`: The name of the BPF program
- `type`: The type of the BPF program
- `tag`: The tag of the BPF program
- `gpl_compatible`: Whether the BPF program is GPL compatible
- `load_time`: The time the BPF program was loaded

## Try it out

You'll need a Grafana stack set up.
You can quickly deploy one using:

```bash
podman play kube metrics-stack.yaml
```

Then, you can deploy the exporter:

```
sudo ./target/debug/bpf-metrics-exporter
```

You can log into grafana at http://localhost:3000/ with the credentials `admin:admin`. Once there, you can explore the metrics in the prometheus
datasource.
