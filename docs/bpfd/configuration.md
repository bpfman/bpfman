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
  enabled = true
  address = "::1"
  port = 50051

[[grpc.endpoints]]
  type = "unix"
  enabled = false
  path = "/run/bpfd/bpfd.sock"
```

`bpfctl` and `bpfd-agent` (which is only used in Kubernetes type deployments) will also read the
bpfd configuration file (`/etc/bpfd/bpfd.toml`) to retrieve the bpfd-client certificate file locations.

#### Config Section: [tls]

This section of the configuration file allows the mTLS certificate authority file locations to be overwritten.
If the given certificates exist, then bpfd will use them.
Otherwise, bpfd will create them.
Default values are shown above.

Valid fields:

- **ca_cert**: Certificate authority file location, intended to be used by bpfd and client.
- **cert**: Certificate file location, intended to be used by bpfd.
- **key**: Certificate key location, intended to be used by bpfd.
- **client_cert**: Client certificate file location, intended to be used by bpfd clients (`bpfctl`, `bpfd-agent`, etc).
- **client_key**: Client certificate key file location, intended to be used by bpfd clients (`bpfctl`, `bpfd-agent`, etc).

If bpfd is running as a systemd service, then the certificates must be accessible by bpfd
(owned by the bpfd User and User Group).

#### Config Section: [interfaces]

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

#### Config Section: [grpc.endpoints]

In this section different endpoints can be configured for bpfd to listen on. We currently support TCP sockets
with IPv4 and Ipv6 addresses and Unix domain sockets.
When using TCP sockets, the tls configuration will be used to secure communication.
Unix domain sockets provide a simpler communication with no encryption. These sockets are owned by the bpfd
user and user group when running as a systemd or non-root process.

Valid fields:

- **type**: Specify if the endpoint will listen on a TCP or Unix domain socket. Valid values: ["tcp"|"unix"]
- **enabled**: Configure whether bpfd should listen on the endpoint. Valid values: ["true"|"false"]
- **address**: Exclusive to TCP sockets. Specify the address the endpoint should listen on. Valid values: Any valid IPv4 or IPv6 address.
- **port**: Exclusive to TCP sockets. Specify the port bpfd should listen on. Valid values: An integer between 1024 and 65535.
- **path**: Exclusive to Unix sockets. Specify the path where the socket will be created. Valid values: A valid unix path.

### Loading Programs at System Launch

bpfd allows the user to specify certain eBPF programs to always be loaded every time the daemon is started.
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
