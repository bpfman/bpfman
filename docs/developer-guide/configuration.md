# Configuration

## bpfman Configuration File

bpfman looks for a configuration file to be present at `/etc/bpfman/bpfman.toml`.
If no file is found, defaults are assumed.
There is an example at `scripts/bpfman.toml`, similar to:

```toml
[interfaces]
  [interfaces.eth0]
  xdp_mode = "drv" # Valid xdp modes are "hw", "skb" and "drv". Default: "drv", but will fall back to "skb" on failure.

[signing]
allow_unsigned = true
verify_enabled = true

[database]
max_retries = 10
millisec_delay = 1000
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
