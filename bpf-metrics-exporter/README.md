# bpf-metrics-exporter

Exports metrics from the kernel's BPF subsystem to OpenTelmetry.
These can later be enriched with other metrics from the system, for example,
to correlate process IDs -> containers -> k8s pods.

## Usage

```bash
./bpf-metrics-exporter --otlp-grpc localhost:4317
```

## Metrics

The following metrics are currently exported, this list will continue to expand:

### [Gauges](https://opentelemetry.io/docs/specs/otel/metrics/api/#gauge)

- `bpf_program_info`: Information on each loaded BPF Program
    - Labels:
        - `id`: The ID of the BPF program
        - `name`: The name of the BPF program
        - `type`: The type of the BPF program as a readable string
        - `tag`: The tag of the BPF program
        - `gpl_compatible`: Whether the BPF program is GPL compatible
        - `load_time`: The time the BPF program was loaded
- `bpf_map_info`: Information of each loaded BPF Map
    - Labels:
        - `id`: The ID of the BPF map
        - `name`: The name of the BPF map
        - `type`: The type of the BPF map as an `u32` which corresponds to the following [kernel enumeration](https://elixir.bootlin.com/linux/v6.6.3/source/include/uapi/linux/bpf.h#L906)
        - `key_size`: The key size in bytes for the BPF map
        - `value_size`: The value size for the BPF map
        - `max_entries`: The maximum number of entries for the BPF map.
        - `flags`: Loadtime specific flags for the BPF map
- `bpf_link_info`: Information on each of the loaded BPF Link
    - Labels:
        - `id`: The ID of the bpf Link
        - `prog_id`: The Program ID of the BPF program which is using the Link.
        - `type`: The BPF Link type as a `u32` which corresponds to the following [kernel enumeration](https://elixir.bootlin.com/linux/v6.6.3/source/include/uapi/linux/bpf.h#L1048)
- `bpf_program_load_time`: The standard UTC time the program was loaded in seconds
    - Labels:
        - `id`: The ID of the BPF program
        - `name`: The name of the BPF program
        - `type`: The type of the BPF program as a readable string

### [Counters](https://opentelemetry.io/docs/specs/otel/metrics/api/#counter)

**Note**: All counters will have the [suffix `_total` appended](https://github.com/OpenObservability/OpenMetrics/blob/main/specification/OpenMetrics.md#counter-1).

- `bpf_program_size_jitted_bytes`: The size in bytes of the program's JIT-compiled machine code.
    - Labels:
        - `id`: The ID of the BPF program
        - `name`: The name of the BPF program
        - `type`: The type of the BPF program as a readable string
- `bpf_program_size_translated_bytes`: The size of the BPF program in bytes.
    - Labels:
        - `id`: The ID of the BPF program
        - `name`: The name of the BPF program
        - `type`: The type of the BPF program as a readable string
- `bpf_program_mem_bytes`: The amount of memory used by the BPF program in bytes.
    - Labels:
        - `id`: The ID of the BPF program
        - `name`: The name of the BPF program
        - `type`: The type of the BPF program as a readable string
- `bpf_program_verified_instructions`: The number of instructions in the BPF program.
    - Labels:
        - `id`: The ID of the BPF program
        - `name`: The name of the BPF program
        - `type`: The type of the BPF program as a readable string
- `bpf_map_key_size`: The size of the BPF map key
    - Labels:
        - `id`: The ID of the BPF map
        - `name`: The name of the BPF map
        - `type`: The type of the BPF map as an `u32` which corresponds to the following [kernel enumeration](https://elixir.bootlin.com/linux/v6.6.3/source/include/uapi/linux/bpf.h#L906)
- `bpf_map_value_size`: The size of the BPF map value
    - Labels:
        - `id`: The ID of the BPF map
        - `name`: The name of the BPF map
        - `type`: The type of the BPF map as an `u32` which corresponds to the following [kernel enumeration](https://elixir.bootlin.com/linux/v6.6.3/source/include/uapi/linux/bpf.h#L906)
- `bpf_map_max_entries`: The maximum number of entries allowed for the BPF map
    - Labels:
        - `id`: The ID of the BPF map
        - `name`: The name of the BPF map
        - `type`: The type of the BPF map as an `u32` which corresponds to the following [kernel enumeration](https://elixir.bootlin.com/linux/v6.6.3/source/include/uapi/linux/bpf.h#L906)

## Try it out

You'll need a Grafana stack set up.
You can quickly deploy one using:

```bash
podman play kube metrics-stack.yaml
```

Then, you can deploy the exporter:

```bash
sudo ./target/debug/bpf-metrics-exporter
```

You can log into grafana at `http://localhost:3000/` using the default user:password
`admin:admin`.

From there simply select the default dashboard titled `eBPF Subsystem Metrics`:

![eBPF Subsystem Metrics](dashboard-example.png)

In order to clean everything up simply exit the bpf-metrics-exporter process with
`ctrl+c` and run:

```bash
podman kube down metrics-stack.yaml
```
