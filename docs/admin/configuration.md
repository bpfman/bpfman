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

### Loading Programs at system launch time

Bpfd allows the user to specify certain bpf programs to always be loaded every time the daemon is started. To do so simply
create `.toml` files in the `/etc/bpfd/programs.d` directory with the following syntax:

**Users can specify multiple programs in a single `.toml` file AND multiple `.toml` files** 

```toml

[[programs]]
name = "program0"
interface = "eth0"
path = <PATH TO BPF BYTECODE>
section_name = "pass"
program_type = "xdp"
priority = 50
proceed_on = ["pass", "dispatcher_return"]

[[programs]]
name = "program1"
interface = "eth0"
path = <PATH TO BPF BYTECODE>
section_name = "drop"
program_type = "xdp"
priority = 55
proceed_on = ["pass", "dispatcher_return"]
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
