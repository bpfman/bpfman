# Run bpfman From Release Image

This section describes how to deploy `bpfman` from a given release.
See [Releases](https://github.com/bpfman/bpfman/releases) for the set of bpfman
releases.

> **Note:** Instructions for interacting with bpfman change from release to release, so reference
> release specific documentation. For example:
>
>    [https://bpfman.io/v0.3.1/getting-started/running-release/](https://bpfman.io/v0.3.1/getting-started/running-release/)

Jump to the [Setup and Building bpfman](./building-bpfman.md) section
for help building from the latest code or building from a release branch.

[Start bpfman-rpc](./launching-bpfman.md/#start-bpfman-rpc) contains more details on the different
modes to run `bpfman` in on the host.
Use [Run as a Long Lived Process](#run-as-a-long-lived-process) or [Run as a Systemd Service](#run-as-a-systemd-service)
below for deploying released version of `bpfman` and then use
[Deploying Example eBPF Programs On Local Host](./example-bpf-local.md)
for further information on how to test and interact with `bpfman`. 

[Deploying the bpfman-operator](../developer-guide/operator-quick-start.md) contains
more details on deploying `bpfman` in a Kubernetes deployment and
[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) contains
more details on interacting with `bpfman` running in a Kubernetes deployment.
Use [Deploying Release Version of the bpfman-operator](#deploying-release-version-of-the-bpfman-operator)
below for deploying released version of `bpfman` in Kubernetes and then use the
links above for further information on how to test and interact with `bpfman`. 

## Run as a Long Lived Process

To run `bpfman` in the foreground, download the release binary tar files and unpack them.

```console
export BPFMAN_REL=0.4.0
mkdir -p $HOME/src/bpfman-${BPFMAN_REL}/; cd $HOME/src/bpfman-${BPFMAN_REL}/
wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfman-linux-x86_64.tar.gz
tar -xzvf bpfman-linux-x86_64.tar.gz; rm bpfman-linux-x86_64.tar.gz
wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfman-rpc-linux-x86_64.tar.gz
tar -xzvf bpfmam-rpc-linux-x86_64.tar.gz; rm bpfman-rpc-linux-x86_64.tar.gz

$ tree
.
└── target
    └── x86_64-unknown-linux-musl
        └── release
            ├── bpfman
            └── bpfman-rpc
```

To deploy `bpfman-rpc`:

```console
sudo RUST_LOG=info ./target/x86_64-unknown-linux-musl/release/bpfman-rpc --timeout=0
[INFO  bpfman::utils] Log using env_logger
[INFO  bpfman::utils] Has CAP_BPF: true
[INFO  bpfman::utils] Has CAP_SYS_ADMIN: true
[WARN  bpfman::utils] Unable to read config file, using defaults
[INFO  bpfman_rpc::serve] Using no inactivity timer
[INFO  bpfman_rpc::serve] Using default Unix socket
[INFO  bpfman_rpc::serve] Listening on /run/bpfman-sock/bpfman.sock
:
```

To use the CLI:

```console
sudo ./target/x86_64-unknown-linux-musl/release/bpfman list
 Program ID  Name  Type  Load Time
```

Continue in [Deploying Example eBPF Programs On Local Host](./example-bpf-local.md) if desired.

## Run as a Systemd Service

To run `bpfman` as a systemd service, the binaries will be placed in a well known location
(`/usr/sbin/.`) and a service configuration files will be added
(`/usr/lib/systemd/system/bpfman.service` and `/usr/lib/systemd/system/bpfman.socket`).
There is a script that is used to install the service properly, so the source code needs
to be downloaded to retrieve the script.
Download and unpack the source code, then download and unpack the binaries.

```console
export BPFMAN_REL=0.4.0
mkdir -p $HOME/src/; cd $HOME/src/
wget https://github.com/bpfman/bpfman/archive/refs/tags/v${BPFMAN_REL}.tar.gz
tar -xzvf v${BPFMAN_REL}.tar.gz; rm v${BPFMAN_REL}.tar.gz
cd bpfman-${BPFMAN_REL}

wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfman-linux-x86_64.tar.gz
tar -xzvf bpfmna-linux-x86_64.tar.gz; rm bpfman-linux-x86_64.tar.gz
wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfman-rpc-linux-x86_64.tar.gz
tar -xzvf bpfman-rpc-linux-x86_64.tar.gz; rm bpfman-rpc-linux-x86_64.tar.gz
```

Run the following command to copy the `bpfman-rpc` and `bpfman` binaries to `/usr/sbin/` and copy a
default `bpfman.service` and `bpfman.socket` files to `/usr/lib/systemd/system/`.
This option will also start the systemd service `bpfman.socket` by default.

```console
sudo ./scripts/setup.sh install
```

Continue in [Deploying Example eBPF Programs On Local Host](./example-bpf-local.md) if desired.

## Deploying Release Version of the bpfman-operator

The quickest solution for running `bpfman` in a Kubernetes deployment is to run a
Kubernetes KIND Cluster:

```console
kind create cluster --name=test-bpfman
```

Next, deploy the bpfman CRDs:

```console
export BPFMAN_REL=0.4.0
kubectl apply -f  https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfman-crds-install.yaml
```

Next, deploy the `bpfman-operator`, which will also deploy the `bpfman-daemon`, which contains
`bpfman-rpc`, `bpfman` Library and `bpfman-agent`:

```console
kubectl apply -f https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfman-operator-install-v${BPFMAN_REL}.yaml
```

Finally, deploy an example eBPF program.

```console
kubectl apply -f https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/go-xdp-counter-install-v${BPFMAN_REL}.yaml
```

There are other example programs in the [Releases](https://github.com/bpfman/bpfman/releases)
page.

Continue in [Deploying the bpfman-operator](../developer-guide/operator-quick-start.md) or
[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) if desired.
Keep in mind that prior to v0.4.0, `bpfman` was released as `bpfd`.
So follow the release specific documentation.

Use the following command to teardown the cluster:

```console
kind delete cluster -n test-bpfman
```
