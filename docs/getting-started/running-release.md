# Run bpfman From Release Image

This section describes how to deploy `bpfman` from a given release.
See [Releases](https://github.com/bpfman/bpfman/releases) for the set of bpfman
releases.

Jump to the [Setup and Building bpfman](./building-bpfman.md) section
for help building from the latest code or building from a release branch.

[Tutorial](./tutorial.md) contains more details on the different
modes to run `bpfman` in on the host and how to test.
Use [Local Host](#local-host) or [Systemd Service](#systemd-service)
below for deploying released version of `bpfman` and then use [Tutorial](./tutorial.md)
for further information on how to test and interact with `bpfman`. 

[Deploying the bpfman-operator](../developer-guide/operator-quick-start.md) contains
more details on deploying `bpfman` in a Kubernetes deployment and
[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) contains
more details on interacting with `bpfman` running in a Kubernetes deployment.
Use [Deploying Release Version of the bpfman-operator](#deploying-release-version-of-the-bpfman-operator)
below for deploying released version of `bpfman` in Kubernetes and then use the
links above for further information on how to test and interact with `bpfman`. 

> **NOTE:**
> The latest release, v0.3.1, was before the rename of `bpfd` to `bpfman`.
> So the commands below still refer to bpfd.

## Local Host

To run `bpfd` in the foreground using `sudo`, download the release binary tar
files and unpack them.

```console
export BPFMAN_REL=0.3.1
mkdir -p $HOME/src/bpfman-${BPFMAN_REL}/; cd $HOME/src/bpfman-${BPFMAN_REL}/
wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfd-linux-x86_64.tar.gz
tar -xzvf bpfd-linux-x86_64.tar.gz; rm bpfd-linux-x86_64.tar.gz
wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfctl-linux-x86_64.tar.gz
tar -xzvf bpfctl-linux-x86_64.tar.gz; rm bpfctl-linux-x86_64.tar.gz

$ tree
.
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

To use the CLI:

```console
sudo ./target/x86_64-unknown-linux-musl/release/bpfctl list
 Program ID  Name       Type  Load Time                
```

Continue in [Tutorial](./tutorial.md) if desired.
Use the `bpfctl` commands in place of the `bpfman` commands described in
[Tutorial](./tutorial.md).

## Systemd Service

To run `bpfd` as a systemd service, the binaries will be placed in a well known location
(`/usr/sbin/.`) and a service configuration file will be added
(`/usr/lib/systemd/system/bpfd.service`).
There is a script that is used to install the service properly, so the source code needs
to be downloaded to retrieve the script.
Download and unpack the source code, then download and unpack the binaries.

```console
export BPFMAN_REL=0.3.1
mkdir -p $HOME/src/; cd $HOME/src/
wget https://github.com/bpfman/bpfman/archive/refs/tags/v${BPFMAN_REL}.tar.gz
tar -xzvf v${BPFMAN_REL}.tar.gz; rm v${BPFMAN_REL}.tar.gz
cd bpfman-${BPFMAN_REL}

wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfd-linux-x86_64.tar.gz
tar -xzvf bpfd-linux-x86_64.tar.gz; rm bpfd-linux-x86_64.tar.gz
wget https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfctl-linux-x86_64.tar.gz
tar -xzvf bpfctl-linux-x86_64.tar.gz; rm bpfctl-linux-x86_64.tar.gz
```

Run the following command to copy the `bpfd` and `bpfctl` binaries to `/usr/sbin/` and copy a
default `bpfd.service` file to `/usr/lib/systemd/system/`.
This option will also start the systemd service `bpfd.service` by default.

```console
sudo ./scripts/setup.sh install
```

> **NOTE:**
> If running a release older than `v0.3.1`, the install script is not coded to copy
> binaries from the release directory, so the binaries will need to be manually copied.

Continue in [Tutorial](./tutorial.md) if desired.

## Deploying Release Version of the bpfman-operator

The quickest solution for running `bpfman` in a Kubernetes deployment is to run a
Kubernetes KIND Cluster:

```console
kind create cluster --name=test-bpfman
```

Next, deploy the bpfman CRDs:

```console
export BPFMAN_REL=0.3.1
kubectl apply -f  https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfd-crds-install-v${BPFMAN_REL}.yaml
```

Next, deploy the `bpfman-operator`, which will also deploy the `bpfman-daemon`, which contains `bpfman` and `bpfman-agent`:

```console
kubectl apply -f https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/bpfd-operator-install-v${BPFMAN_REL}.yaml
```

Finally, deploy an example eBPF program.

```console
kubectl apply -f https://github.com/bpfman/bpfman/releases/download/v${BPFMAN_REL}/go-xdp-counter-install-v${BPFMAN_REL}.yaml
```

There are other example programs in the [Releases](https://github.com/bpfman/bpfman/releases)
page.

Continue in [Deploying the bpfman-operator](../developer-guide/operator-quick-start.md) or
[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) if desired.
Keep in mind that the documentation describes `bpfman` while Release v0.3.1 is still using
`bpfd`.

Use the following command to teardown the cluster:

```console
kind delete cluster -n test-bpfman
```
