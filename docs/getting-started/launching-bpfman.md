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

## Start bpfman-rpc

When running bpfman, the RPC Server `bpfman-rpc` can be run as a long running process or a
systemd service.
Examples run the same, independent of how bpfman is deployed.

### Run as a Long Lived Process

While learning and experimenting with `bpfman`, it may be useful to run `bpfman` in the foreground
(which requires a second terminal to run the `bpfman` CLI commands).
When run in this fashion, logs are dumped directly to the terminal.
For more details on how logging is handled in bpfman, see [Logging](../developer-guide/logging.md).

```console
sudo RUST_LOG=info ./target/debug/bpfman-rpc --timeout=0
[INFO  bpfman::utils] Has CAP_BPF: true
[INFO  bpfman::utils] Has CAP_SYS_ADMIN: true
[WARN  bpfman::utils] Unable to read config file, using defaults
[INFO  bpfman_rpc::serve] Using no inactivity timer
[INFO  bpfman_rpc::serve] Using default Unix socket
[INFO  bpfman_rpc::serve] Listening on /run/bpfman-sock/bpfman.sock
```

When a build is run for bpfman, built binaries can be found in `./target/debug/`.
So when launching `bpfman-rpc` and calling `bpfman` CLI commands, the binary must be in the $PATH
or referenced directly:

```console
sudo ./target/debug/bpfman list
```

For readability, the remaining sample commands will assume the `bpfman` CLI binary is in the $PATH,
so `./target/debug/` will be dropped.

### Run as a systemd Service

Run the following command to copy the `bpfman` CLI and `bpfman-rpc` binaries to `/usr/sbin/` and
copy `bpfman.socket` and `bpfman.service` files to `/usr/lib/systemd/system/`.
This option will also enable and start the systemd services:

```console
sudo ./scripts/setup.sh install
```

`bpfman` CLI is now in $PATH, so `./targer/debug/` is not needed:

```console
sudo bpfman list
```

To view logs, use `journalctl`:

```console
sudo journalctl -f -u bpfman.service -u bpfman.socket
Mar 27 09:13:54 server-calvin systemd[1]: Listening on bpfman.socket - bpfman API Socket.
  <RUN "sudo ./go-kprobe-counter">
Mar 27 09:15:43 server-calvin systemd[1]: Started bpfman.service - Run bpfman as a service.
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Has CAP_BPF: true
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Has CAP_SYS_ADMIN: true
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Unable to read config file, using defaults
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Using a Unix socket from systemd
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Using inactivity timer of 15 seconds
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Listening on /run/bpfman-sock/bpfman.sock
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Unable to read config file, using defaults
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Unable to read config file, using defaults
Mar 27 09:15:43 server-calvin bpfman-rpc[2548091]: Starting Cosign Verifier, downloading data from Sigstore TUF repository
Mar 27 09:15:45 server-calvin bpfman-rpc[2548091]: Loading program bytecode from file: /home/<USER>/src/bpfman/examples/go-kprobe-counter/bpf_bpfel.o
Mar 27 09:15:45 server-calvin bpfman-rpc[2548091]: Added probe program with name: kprobe_counter and id: 7568
Mar 27 09:15:48 server-calvin bpfman-rpc[2548091]: Unable to read config file, using defaults
Mar 27 09:15:48 server-calvin bpfman-rpc[2548091]: Removing program with id: 7568
Mar 27 09:15:58 server-calvin bpfman-rpc[2548091]: Shutdown Unix Handler /run/bpfman-sock/bpfman.sock
Mar 27 09:15:58 server-calvin systemd[1]: bpfman.service: Deactivated successfully.
```

#### Additional Notes

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

> For security reasons, it is recommended to run `bpfman-rpc` as a systemd service when running
on a local host.
For local development, some may find it useful to run `bpfman-rpc` as a long lived process.

When run as a systemd service, the set of linux capabilities are limited to only the required set.
If permission errors are encountered, see [Linux Capabilities](../developer-guide/linux-capabilities.md)
for help debugging.
