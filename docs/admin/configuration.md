Configuration
=============

## bpfd

bpfd expects a configuration file to be present at `/etc/bpfd.toml`.
If no file is found, defaults are assumed.

```toml
[tls] # REQUIRED
  ca_cert = "/etc/bpfd/certs/ca/ca.pem"
  cert = "/etc/bpfd/certs/bpfd/bpfd.pem"
  key = "/etc/bpfd/certs/bpfd/bpfd.key"

[interfaces]
  [interface.eth0]
  xdp_mode = "hw" # Valid xdp modes are "hw", "skb" and "drv". Default: "skb".
```

## bpfctl

bpfctl expects a configuration file to be present at `/etc/bpfd.toml`.
If no file is found, defaults are assumed.

```toml
[tls] # REQUIRED
  ca_cert = "/etc/bpfd/certs/ca/ca.pem"
  cert = "/etc/bpfd/certs/bpfctl/bpfctl.pem"
  key = "/etc/bpfd/certs/bpfctl/bpfctl.key"
```
