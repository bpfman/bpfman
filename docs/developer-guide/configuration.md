# Configuration

## bpfman Configuration File

bpfman looks for a configuration file to be present at `/etc/bpfman/bpfman.toml`.
If no file is found, defaults are assumed.
There is an example at `scripts/bpfman.toml`, similar to:

```toml
[interfaces]
  [interface.eth0]
  xdp_mode = "hw" # Valid xdp modes are "hw", "skb" and "drv". Default: "skb".
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
