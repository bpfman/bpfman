# Run bpfd From Release Image

This section describes how to deploy `bpfd` from a given release.
See [Releases](https://github.com/bpfd-dev/bpfd/releases) for the set of bpfd
releases.

Jump to the [Setup and Building bpfd](./building-bpfd.md) section
for help building from the latest code or building from a release branch.

[Tutorial](./tutorial.md) contains more details on the different
modes to run `bpfd` in on the host and how to test.
Use [Privileged Mode](#privileged-mode) or [Systemd Service](#systemd-service)
below for deploying released version of `bpfd` and then use [Tutorial](./tutorial.md)
for further information on how to test and interact with `bpfd`. 

[Deploying the bpfd-operator](../developer-guide/operator-quick-start.md) contains
more details on deploying `bpfd` in a Kubernetes deployment and
[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) contains
more details on interacting with `bpfd` running in a Kubernetes deployment.
Use [Deploying Release Version of the bpfd-operator](#deploying-release-version-of-the-bpfd-operator)
below for deploying released version of `bpfd` in Kubernetes and then use the
links above for further information on how to test and interact with `bpfd`. 

## Privileged Mode

To run `bpfd` in the foreground using `sudo`, download the release binary tar
files and unpack them.

```console
export BPFD_REL=0.3.0
mkdir -p $HOME/src/bpfd-${BPFD_REL}/; cd $HOME/src/bpfd-${BPFD_REL}/
wget https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/bpfctl-linux-x86_64.tar.gz
wget https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/bpfd-linux-x86_64.tar.gz

tar -xzvf bpfctl-linux-x86_64.tar.gz; rm bpfctl-linux-x86_64.tar.gz
tar -xzvf bpfd-linux-x86_64.tar.gz; rm bpfd-linux-x86_64.tar.gz

$ tree
.
├── bpfctl-linux-x86_64.tar.gz
├── bpfd-linux-x86_64.tar.gz
└── target
    └── x86_64-unknown-linux-musl
        └── release
            ├── bpfctl
            └── bpfd
```

To deploy `bpfd`:

```console
sudo RUST_LOG=info ./target/x86_64-unknown-linux-musl/release/bpfd 
[2023-10-13T15:53:25Z INFO  bpfd] Log using env_logger
[2023-10-13T15:53:25Z INFO  bpfd] Has CAP_BPF: true
[2023-10-13T15:53:25Z INFO  bpfd] Has CAP_SYS_ADMIN: true
:
```

To use `bpfctl`:

```console
sudo ./target/x86_64-unknown-linux-musl/release/bpfctl list
 Program ID  Name       Type  Load Time                
```

Continue in [Tutorial](./tutorial.md) if desired.

## Systemd Service

To run `bpfd` as a systemd service, the binaries will be placed in a well known location
(`/usr/sbin/.`) and a service configuration file will be added
(`/usr/lib/systemd/system/bpfd.service`).
There is a script that is used to install the service properly, so the source code needs
to be downloaded to retrieve the script.
Download and unpack the source code, then download and unpack the binaries.

```console
export BPFD_REL=0.3.0
mkdir -p $HOME/src/; cd $HOME/src/
wget https://github.com/bpfd-dev/bpfd/archive/refs/tags/v${BPFD_REL}.tar.gz
cd bpfd-${BPFD_REL}

wget https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/bpfctl-linux-x86_64.tar.gz
wget https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/bpfd-linux-x86_64.tar.gz

tar -xzvf bpfctl-linux-x86_64.tar.gz; rm bpfctl-linux-x86_64.tar.gz
tar -xzvf bpfd-linux-x86_64.tar.gz; rm bpfd-linux-x86_64.tar.gz
```

Run the following command to copy the `bpfd` and `bpfctl` binaries to `/usr/sbin/` and set the user
and user group for each, and copy a default `bpfd.service` file to `/usr/lib/systemd/system/`.
This option will also start the systemd service `bpfd.service` by default.

```console
sudo ./scripts/setup.sh install
```

> **NOTE:** If running a release older than `v0.3.0`, the install script is not coded to copy
binaries from the release directory, so the binaries will need to be manually copied.

Then add usergroup bpfd to the desired user if not already run and logout/login to apply.

```console
sudo usermod -a -G bpfd $USER
exit
<LOGIN>
```

Continue in [Tutorial](./tutorial.md) if desired.

## Deploying Release Version of the bpfd-operator

The quickest solution for running `bpfd` in a Kubernetes deployment is to run a
Kubernetes KIND Cluster:

```console
kind create cluster --name=test-bpfd
```

Next, deploy the bpfd CRDs:

```console
export BPFD_REL=0.3.0
kubectl apply -f  https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/bpfd-crds-install-v${BPFD_REL}.yaml
```

Next, deploy the `bpfd-operator`, which will also deploy the `bpfd-daemon`, which contains `bpfd` and `bpfd-agent`:

```console
kubectl apply -f https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/bpfd-operator-install-v${BPFD_REL}.yaml
```

Finally, deploy an example eBPF program.

```console
kubectl apply -f https://github.com/bpfd-dev/bpfd/releases/download/v${BPFD_REL}/go-xdp-counter-install-v${BPFD_REL}.yaml
```

There are other example programs in the [Releases](https://github.com/bpfd-dev/bpfd/releases)
page.

Continue in [Deploying the bpfd-operator](../developer-guide/operator-quick-start.md) or
[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) if desired.
