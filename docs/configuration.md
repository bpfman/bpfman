Configuration
=============

## bpfd

bpfd looks for a configuration file to be present at `/etc/bpfd/bpfd.toml`.
If no file is found, defaults are assumed.
There is an example at `scripts/bpfd.toml`, similar to:

```toml
[tls] # REQUIRED
  ca_cert = "/etc/bpfd/certs/ca/ca.pem"
  cert = "/etc/bpfd/certs/bpfd/bpfd.pem"
  key = "/etc/bpfd/certs/bpfd/bpfd.key"
  client_cert = "/etc/bpfd/certs/bpfd-client/bpfd-client.pem"
  client_key = "/etc/bpfd/certs/bpfd-client/bpfd-client.key"

[interfaces]
  [interface.eth0]
  xdp_mode = "hw" # Valid xdp modes are "hw", "skb" and "drv". Default: "skb".

[[grpc.endpoints]]
  type = "tcp"
  address = "::1"
  port = 50051
```

`bpfctl` and `bpfd-agent` (which is only used in Kubernetes type deployments) will also read the
bpfd configuration file (`/etc/bpfd/bpfd.toml`) to retrieve the bpfd-client certificate file locations.

### Loading Programs at system launch time

bpfd allows the user to specify certain bpf programs to always be loaded every time the daemon is started.
To do so simply create `.toml` files in the `/etc/bpfd/programs.d` directory with the following syntax:

**NOTE:** Users can specify multiple programs in a single `.toml` file OR multiple `.toml` files

```toml

[[programs]]
name = "program0"
path = "/usr/local/src/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o"
section_name = "xdp"
program_type = "xdp"
network_attach = { interface = "eth0", priority = 50, proceed_on = ["pass", "dispatcher_return"] }

[[programs]]
name = "program1"
path = "/usr/local/src/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o"
section_name = "xdp"
program_type = "xdp"
network_attach = { interface = "eth0", priority = 55, proceed_on = ["pass", "dispatcher_return"] }
```
