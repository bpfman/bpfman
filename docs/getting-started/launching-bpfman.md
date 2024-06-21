# Launching bpfman

The most basic way to deploy bpfman is to run it directly on a host system.
First `bpfman` needs to be built and then started.

## Build bpfman

Perform the following steps to build `bpfman`.
If this is your first time using bpfman, follow the instructions in
[Setup and Building bpfman](./building-bpfman.md) to setup the prerequisites for building.
To avoid installing the dependencies and having to build bpfman, consider running bpfman
from a packaged release (see [Run bpfman From Release Image](./running-release.md)) or
installing the bpfman RPM (see [Run bpfman From RPM](./running-rpm.md)).

```console
cd bpfman/
cargo build
```

## Install and Start bpfman

Run the following command to copy the `bpfman` CLI and `bpfman-rpc` binaries to `/usr/sbin/` and
copy `bpfman.socket` and `bpfman.service` files to `/usr/lib/systemd/system/`.
This option will also enable and start the systemd services:

```console
cd bpfman/
sudo ./scripts/setup.sh install
```

`bpfman` CLI is now in $PATH and can be used to load, view and unload eBPF programs.

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --name pass xdp --iface eno3 --priority 100

sudo bpfman list
 Program ID  Name  Type  Load Time                
 53885       pass  xdp   2024-08-26T17:41:36-0400 

sudo bpfman unload 53885
```

`bpfman` CLI is a Rust program that calls the `bpfman` library directly.
To view logs while running `bpfman` CLI commands, prepend `RUST_LOG=info` to each command
(see [Logging](../developer-guide/logging.md) for more details):

```console
sudo RUST_LOG=info bpfman list
[INFO  bpfman::utils] Has CAP_BPF: true
[INFO  bpfman::utils] Has CAP_SYS_ADMIN: true
 Program ID  Name  Type  Load Time 
```

The examples (see [Deploying Example eBPF Programs On Local Host](./example-bpf-local.md))
are Go based programs, so they are building and sending RPC messaged to the rust based binary
`bpfman-rpc`, which in turn calls the `bpfman` library.

```console
cd bpfman/examples/go-xdp-counter/
go run -exec sudo . -iface eno3
```

To view bpfman logs for RPC based applications, including all the provided examples, use `journalctl`:

```console
sudo journalctl -f -u bpfman.service -u bpfman.socket
:
  <RUN "go run -exec sudo . -iface eno3">
Aug 26 18:03:54 server-calvin bpfman-rpc[2401725]: Using a Unix socket from systemd
Aug 26 18:03:54 server-calvin bpfman-rpc[2401725]: Using inactivity timer of 15 seconds
Aug 26 18:03:54 server-calvin bpfman-rpc[2401725]: Listening on /run/bpfman-sock/bpfman.sock
Aug 26 18:03:54 server-calvin bpfman-rpc[2401725]: Has CAP_BPF: true
Aug 26 18:03:54 server-calvin bpfman-rpc[2401725]: Has CAP_SYS_ADMIN: true
Aug 26 18:03:54 server-calvin bpfman-rpc[2401725]: Starting Cosign Verifier, downloading data from Sigstore TUF repository
Aug 26 18:03:55 server-calvin bpfman-rpc[2401725]: Loading program bytecode from file: /home/$USER/src/bpfman/bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o
Aug 26 18:03:57 server-calvin bpfman-rpc[2401725]: The bytecode image: quay.io/bpfman/xdp-dispatcher:latest is signed
Aug 26 18:03:57 server-calvin bpfman-rpc[2401725]: Added xdp program with name: xdp_stats and id: 53919
Aug 26 18:04:09 server-calvin bpfman-rpc[2401725]: Shutdown Unix Handler /run/bpfman-sock/bpfman.sock```
```

### Additional Notes

To update the configuration settings associated with running `bpfman` as a service, edit the
service configuration files:

```console
sudo vi /usr/lib/systemd/system/bpfman.socket
sudo vi /usr/lib/systemd/system/bpfman.service
sudo systemctl daemon-reload
```

If `bpfman` CLI or `bpfman-rpc` is rebuilt, the following command can be run to install the update
binaries without tearing down `bpfman`.
The services are automatically restarted.

```console
sudo ./scripts/setup.sh reinstall
```

To unwind all the changes, stop `bpfman` and remove all related files from the system, run the
following script:

```console
sudo ./scripts/setup.sh uninstall
```

### Preferred Method to Start bpfman

In order to call into the `bpfman` Library, the calling process must be privileged.
In order to load and unload eBPF, the kernel requires a set of powerful capabilities.
Long lived privileged processes are more vulnerable to attack than short lived processes.
When `bpfman-rpc` is run as a systemd service, it is leveraging
[socket activation](https://man7.org/linux/man-pages/man1/systemd-socket-activate.1.html).
This means that it loads a `bpfman.socket` and `bpfman.service` file.
The socket service is the long lived process, which doesn't have any special permissions.
The service that runs `bpfman-rpc` is only started when there is a request on the socket,
and then `bpfman-rpc` stops itself after an inactivity timeout.

!!! Note
    For security reasons, it is recommended to run `bpfman-rpc` as a systemd service when running
    on a local host.
    For local development, some may find it useful to run `bpfman-rpc` as a long lived process.

When run as a systemd service, the set of linux capabilities are limited to only the required set.
If permission errors are encountered, see [Linux Capabilities](../developer-guide/linux-capabilities.md)
for help debugging.
