# Daemonless bpfd

## Introduction

The bpfd daemon is a userspace daemon that runs on the host and responds to
gRPC API requests over a unix socket, to load, unload and list the eBPF
programs on a host.

The rationale behind running as a daemon was because something needs to be
listening on the unix socket for API requests, and that we also maintain
some state in-memory about the programs that have been loaded.
However, since this daemon requires root privileges to load and unload eBPF
programs it is a security risk for this to be a long-running - even with the
mitigations we have in place to drop privileges and run as a non-root user.
This risk is equivalent to that of something like [Docker].

[Docker]: https://docs.docker.com/engine/security/#docker-daemon-attack-surface

This document describes the design of a daemonless bpfd, which is a
bpfd that runs only runs when required, for example, to load
or unload an eBPF program.

## Design

The daemonless bpfd is a single binary that collects some of the functionality
from both bpfd and bpfctl.

> :note: Daemonless, not rootless. Since CAP_BPF (and more) is required to
> load and unload eBPF programs, we will still need to run as root. But at
> least we can run as root for a shorter period of time.

### Command: bpfd system service

This command will run the bpfd gRPC API server - for one or more of the
gRPC API services we support.

It will listen on a unix socket (or tcp socket) for API requests - provided via
a positional argument, defaulting to `unix:///var/run/bpfd.sock`.
It will shutdown after a timeout of inactivity - provided by a `--timeout` flag
defaulting to 5 seconds.

It will support being run as a systemd service, via socket activation, which
will allow it to be started on demand when a request is made to the unix socket.
When in this mode it will not create the unix socket itself, but will instead
use LISTEN_FDS to determine the file descriptor of the unix socket to use.

Usage in local development (or packaged in a container) is still possible
by running as follows:

```console
sudo bpfd --timeout=0 unix:///var/run/bpfd.sock
```

> :note: The bpfd user and group will be deprecated.
> We will also remove some of the unit-file complexity (i.e directories)
> and handle this in bpfd itself.

### Command: bpfd load file

As the name suggests, this command will load an eBPF program from a file.
This was formerly `bpfctl load-from-file`.

### Command: bpfd load image

As the name suggests, this command will load an eBPF program from a container
image. This was formerly `bpfctl load-from-image`.

### Command: bpfd unload

This command will unload an eBPF program. This was formerly `bpfctl unload`.

### Command: bpfd list

This command will list the eBPF programs that are currently loaded.
This was formerly `bpfctl list`.

### Command: bpfd pull

This command will pull the bpfd container image from a registry.
This was formerly `bpfctl pull`.

### Command: bpfd images

This command will list the bpfd container images that are available.
This command didn't exist, but makes sense to add.

### Command: bpfd version

This command will print the version of bpfd.
This command didn't exist, but makes sense to add.

### State Management

This is perhaps the most significant change from how bpfd currently works.

Currently bpfd maintains state in-memory about the programs that have been
loaded (by bpfd, and the kernel).
Some of this state is flushed to disk, so if bpfd is restarted it can
reconstruct it.

Flushing to disk and state reconstruction is cumbersome at present and having
to move all state management out of in-memory stores is a forcing function
to improve this.
We will replace the existing state management with [sled], which gives us
a familiar API to work with while also being fast, reliable and persistent.

[sled]: https://github.com/spacejam/sled

### Metrics and Monitoring

While adding metrics and monitoring is not a goal of this design, it should
nevertheless be a consideration. In order to provide metrics to Prometheus
or OpenTelemetry we will require an additional exporter process.

We can either:

1. Use the bpfd socket and retrieve metrics via the gRPC API
1. Place state access + metrics gathering functions in a library, such that
   they could be used directly by the exporter process without requiring the
   bpfd socket.

The latter would be more inline with how podman-prometheus-exporter works.
The benefit here is that, the metrics exporter process can be long running
with less privileges - whereas if it were to hit the API over the socket it
would effectively negate the point of being daemonless in the first place since
collection will likley occur more frequently than the timeout on the socket.

## Benefits

The benefits of this design are:

- No long-running daemon with root privileges
- No need to run as a non-root user, this is important since the number of
  capabilities required is only getting larger.
- We only need to ship a single binary.
- We can use systemd socket activation to start bpfd on demand + timeout
  after a period of inactivity.
- Forcs us to fix state management, since we can never rely on in-memory state.
- Bpfd becomes more modular - if we wish to add programs for runtime enforcement,
  metrics, or any other purpose then it's design is decoupled from that of
  bpfd. It could be another binary, or a subcommand on the CLI etc...

## Drawbacks

None yet.

## Backwards Compatibility

- The `bpfctl` command will be removed and all functionality folded into `bpfd`
- The `bpfd` command will be renamed to `bpfd system service`
