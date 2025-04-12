# Configuration

## bpfman Configuration File

bpfman looks for a configuration file to be present at `/etc/bpfman/bpfman.toml`.
If no file is found, defaults are assumed.
There is an example at `scripts/bpfman.toml`, similar to:

```toml
[interfaces]
  [interfaces.eth0]
  xdp_mode = "drv"
  [interfaces.eth1]
  xdp_mode = "hw"
  [interfaces.eth2]
  xdp_mode = "skb"

[signing]
allow_unsigned = true
verify_enabled = true

[database]
max_retries = 10
millisec_delay = 1000

[registry]
xdp_dispatcher_image = "quay.io/bpfman/xdp-dispatcher@sha256:61c34aa2df86d3069aa3c53569134466203c6227c5333f2e45c906cd02e72920" 
tc_dispatcher_image = "quay.io/bpfman/tc-dispatcher@sha256:daa5b8d936caf3a8c94c19592cee7f55445d1e38addfd8d3af846873b8ffc831"

[container_runtime]
enabled = true
preferred_runtime = "docker" # Optional: Specify preferred runtime ("docker" or "podman")
```

### Config Section: [interfaces]

This section of the configuration file allows the XDP Mode for a given interface to be set.
If not set, the default value of `skb` will be used.
Multiple interfaces can be configured.

```toml
[interfaces]
  [interfaces.eth0]
  xdp_mode = "drv"
  [interfaces.eth1]
  xdp_mode = "hw"
  [interfaces.eth2]
  xdp_mode = "skb"
```

Valid fields:

- **xdp_mode**: XDP Mode for a given interface. Valid values: ["drv"|"hw"|"skb"]

### Config Section: [signing]

This section of the configuration file allows control over whether signatures on
OCI packaged eBPF bytecode as container images are verified, and whether they
are required to be signed via
[cosign](https://docs.sigstore.dev/signing/overview/).

By default, images are verified, and unsigned images are allowed. See [eBPF
Bytecode Image Specifications](./shipping-bytecode.md) for more details on
building and shipping bytecode in a container image.

Valid fields:

- **allow_unsigned**: Flag indicating whether unsigned images are allowed.
  Valid values: ["true"|"false"]

- **verify_enabled**: Flag indicating whether signatures should be verified.
  Valid values: ["true"|"false"]

### Config Section: [database]

`bpfman` uses an embedded database to store state and persistent data on disk which
can only be accessed synchronously by a single process at a time.
To avoid returning database lock errors and enhance the user experience, bpfman performs
retries when opening of the database.
The number of retries and the time between retries is configurable.

Valid fields:

- **max_retries**: The number of times to retry opening the database on a given request.
- **millisec_delay**: Time in milliseconds to wait between retry attempts.

### Config Section: [registry]

`bpfman` uses the latest public container images for the xdp and tc dispatchers by default.
Optionally, the configuration values for these images are user-configurable. For example, it may
be desirable in certain enterprise environments to source the xdp and tc dispatcher images from
a self-hosted OCI image registry.
In this case, the default values for the xdp and tc dispatcher images can be overridden below.

Valid fields:

- **xdp_dispatcher_image**: The locator of the xdp dispatcher image in the format `quay.io/bpfman/xdp-dispatcher@sha256:61c34aa2df86d3069aa3c53569134466203c6227c5333f2e45c906cd02e72920`
- **tc_dispatcher_image**: The locator of the tc dispatcher image in the format `quay.io/bpfman/tc-dispatcher@sha256:daa5b8d936caf3a8c94c19592cee7f55445d1e38addfd8d3af846873b8ffc831`

### Config Section: [container_runtime]

This section of the configuration file allows control over the container runtime used by `bpfman`.
By default, the container runtime is enabled, and the preferred runtime is set to `docker`.

Valid fields:

- **enabled**: Flag indicating whether the container runtime is enabled.
  Valid values: ["true"|"false"]

- **preferred_runtime**: Specify the preferred container runtime.
  Valid values: ["docker"|"podman"]
