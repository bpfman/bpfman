# Configuration

## bpfman Configuration File

bpfman looks for a configuration file to be present at `/etc/bpfman/bpfman.toml`.
If no file is found, defaults are assumed.
There is an example at `scripts/bpfman.toml`, similar to:

```toml
[interfaces]
  [interface.eth0]
  xdp_mode = "hw" # Valid xdp modes are "hw", "skb" and "drv". Default: "skb".

[[grpc.endpoints]]
  type = "tcp"
  enabled = true
  address = "::1"
  port = 50051

[[grpc.endpoints]]
  type = "unix"
  enabled = false
  path = "/run/bpfman/bpfman.sock"
```

`bpfctl` and `bpfman-agent` (which is only used in Kubernetes type deployments) will also read the
bpfman configuration file (`/etc/bpfman/bpfman.toml`) to retrieve the bpfman-client certificate file locations.

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

### Config Section: [grpc.endpoints]

In this section different endpoints can be configured for bpfman to listen on. We currently only support Unix sockets.
Unix domain sockets provide a simpler communication with no encryption. These sockets are owned by the bpfman
user and user group when running as a systemd or non-root process.

Valid fields:

- **type**: Specify if the endpoint will listen on a TCP or Unix domain socket. Valid values: ["unix"]
- **enabled**: Configure whether bpfman should listen on the endpoint. Valid values: ["true"|"false"]
- **path**: Exclusive to Unix sockets. Specify the path where the socket will be created. Valid values: A valid unix path.
