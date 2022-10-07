# Go

An example application that uses the `bpfd-go` bindings can be found in
[examples/gocounter/](https://github.com/redhat-et/bpfd/tree/main/examples/gocounter).
This example and associated documentation is intended to provide basics on deploying a BPF
program using bpfd with a go gRPC client.
There are two ways to deploy this example application, simply running locally, or on a Kubernetes
cluster.

The `gocounter` example contains a BPF Program written in C
([xdp_counter.c](https://github.com/redhat-et/bpfd/tree/main/examples/gocounter/bpf/xdp_counter.c))
that is compiled into BPF bytecode.
The BPF program counts packets that are received on an interface and stores the packet and byte
counts in a map that is accessible by the userspace portion.
The `gocounter` example also has a userspace portion written in GO that is responsible for loading
the BPF program at the XDP hook point, via gRPC calls to `bpfd`, and then every 3 seconds polls the
BPF map and logs the current counts.
The `gocounter` userspace code is leveraging the [cilium/ebpf library](https://github.com/cilium/ebpf)
to manage the maps shared with the BPF program.

## Table of Contents

- [Building](#building)
  - [Prerequisites](#prerequisites)
  - [Building Locally](#building-locally)
- [Running On Host](#running-on-host)
   - [Generate Certificates](#generate-certificates)
   - [Running Privileged](#running-privileged)
   - [Running Unprivileged](#running-unprivileged)
- [Running In A Container](#running-in-a-container)
   - [Passing BPF Bytecode In A Container Image](#passing-bpf-bytecode-in-a-container-image)
   - [Building gocounter BPF Bytecode Container Image](#building-gocounter-bpf-bytecode-container-image)
   - [Building A gocounter Userspace Container Image](#building-a-gocounter-userspace-container-image)
   - [Preloading gocounter BPF Bytecode](#preloading-gocounter-bpf-bytecode)
- [Deploying On Kubernetes](#deploying-on-kubernetes)
   - [Loading gocounter BPF Bytecode On Kubernetes](#loading-gocounter-bpf-bytecode-on-kubernetes)
   - [Loading gocounter Userspace Container On Kubernetes](#loading-gocounter-userspace-container-on-kubernetes)
   Loading gocounter Userspace Container On Kubernetes
- [Notes](#notes)

## Building

To build directly on a system, make sure all the prerequisites are met, then build.

### Prerequisites

**Assuming bpfd is already installed and running on the system**

1. All [requirements defined by the `cilium/ebpf` package](https://github.com/cilium/ebpf#requirements)
2. libbpf development package to get the required bpf c headers

    Fedora:

    `sudo dnf install libbpf-devel`

    Ubuntu:

    `sudo apt-get install libbpf-dev`

3. Cilium's `bpf2go` binary

    `go install github.com/cilium/ebpf/cmd/bpf2go@master`


### Building Locally

To build the C based BPF counter bytecode, run:

```console
    cd src/bpfd/examples/gocounter/
    go generate
```

To build the Userspace GO Client run:

```console
    cd src/bpfd/examples/gocounter/
    go build
```

## Running On Host

The most basic way to deploy this example is running directly on a host system.
First, start or ensure `bpfd` is up and running.
[tutorial.md](../admin/tutorial.md) will guide you through deploying `bpfd`.
In all the examples of running `gocounter` on a host system, a certificate must be generated.
Once generated, `gocounter ` can be run.

![gocounter On Host](../img/gocounter-on-host.png)

In this example, when `gocounter` is started, it will send a gRPC request over mTLS to `bpfd`
requesting `bpfd` to load the gocounter BPF bytecode located at
`src/bpfd/src/examples/gocounter/bpf_bpfel.o` at a priority of 50 and on interface `ens3`.
These values are configurable as we will see later, but for now we will use the defaults
(except interface, which is required to be entered).
`bpfd` will load it's `dispatcher` BPF program, which links to the `gocounter` BPF program
which was requested to be loaded.
`bpfctl list` can be used to show that the BPF program was loaded.
`bpfctl` also sends gRPC requests over mTLS to perform actions and request data from `bpfd`.

Once the gocounter BPF bytecode is loaded, the BPF program will write packet counts and byte
counts to a shared map that gocounter Userspace program will read from.

### Generate Certificates

`bpfd` uses mTLS for mutual authentication.
To generate a client certificate for the `gocounter` example run:

```console
    cd src/bpfd/
    sudo ./scripts/setup.sh gocounter
```

This creates the certificate in a sub-directory of the `bpfctl` user (`/etc/bpfctl/certs/gocounter/`).

### Running Privileged

The most basic example, just use `sudo` to start the `gocounter` program.
Determine the host interface to attach the BPF program to and then start the go program with:

```console
    cd src/bpfd/examples/gocounter/
    sudo ./gocounter -iface <INTERNET INTERFACE NAME>
```

The output should show the count and total bytes of packets as they pass through the
interface as shown below:

```console
    sudo ./gocounter -iface ens3
    2022/09/23 09:18:48 Program registered with de12e48a-222d-4abc-ac02-c006d327268b id
    2022/09/23 09:18:51 0 packets received
    2022/09/23 09:18:51 0 bytes received

    2022/09/23 09:18:54 5 packets received
    2022/09/23 09:18:54 1191 bytes received

    :
```

Use `bpfctl` to show the gocounter BPF bytecode was loaded.

```console
    bpfctl list -i ens3
    ens3
    xdp_mode: skb

    0: de12e48a-222d-4abc-ac02-c006d327268b
        section-name: "stats"
        priority: 50
        path: /home/$USER/src/bpfd/examples/gocounter/bpf_bpfel.o
        proceed-on:
```

Finally, press `<CTRL>+c` when finished with `gocounter`.

```console
    :

    2022/09/23 09:18:57 5 packets received
    2022/09/23 09:18:57 1191 bytes received

    2022/09/23 09:19:00 7 packets received
    2022/09/23 09:19:00 1275 bytes received

    ^C2022/09/23 09:19:00 Exiting...
    2022/09/23 09:19:01 Unloading Program: de12e48a-222d-4abc-ac02-c006d327268b
```

### Running Unprivileged

To run the `gocounter` example unprivileged (without `sudo`), the following three steps must be
performed.

#### Step 1: Create `bpfd` User Group

The [tutorial.md](../admin/tutorial.md) guide describes the different modes `bpfd` and be run in.
Specifically, [Unprivileged Mode](../admin/tutorial.md#unprivileged-mode) and
[Systemd Service](../admin/tutorial.md#systemd-service) sections describe how to start `bpfd` with
the `bpfd` and `bpfctl` Users and `bpfd` User Group.
`bpfd` must be started in one of these two ways and `gocounter` must be run from a User that is a
member of the `bpfd` User Group.

```console
    sudo usermod -a -G bpfd $USER
    exit
    <LOGIN>
```

The socket that is created by `gocounter` and shared between `gocounter` and `bpfd` is created in the
`bpfd` User Group and `gocounter` must have read-write access to it:

```console
    $ ls -al /etc/bpfd/sock/gocounter.sock
    srwxrwx---+ 1 <USER> bpfd 0 Aug 26 11:07 /etc/bpfd/sock/gocounter.sock

```

#### Step 2: Grant `gocounter` CAP_BPF Linux Capability

`gocounter` uses a map to share data between the userspace side of the program and the BPF portion.
Accessing this map requires access to the CAP_BPF capability.
Run the following command to grant `gocounter` access to the CAP_BPF capability:

```console
    sudo /sbin/setcap cap_dac_override,cap_bpf=ep ./gocounter
```

**Reminder:** The capability must be re-granted each time `gocounter` is rebuilt.

#### Step 3: Start `gocounter` without `sudo`

Start `gocounter` without `sudo`:

```console
    ./gocounter -iface ens3
    2022/09/23 09:18:48 Program registered with de12e48a-222d-4abc-ac02-c006d327268b id
    2022/09/23 09:18:51 0 packets received
    2022/09/23 09:18:51 0 bytes received

    2022/09/23 09:18:54 5 packets received
    2022/09/23 09:18:54 1191 bytes received

    2022/09/23 09:18:57 5 packets received
    2022/09/23 09:18:57 1191 bytes received

    2022/09/23 09:19:00 7 packets received
    2022/09/23 09:19:00 1275 bytes received

    ^C2022/09/23 09:19:00 Exiting...
    2022/09/23 09:19:01 Unloading Program: de12e48a-222d-4abc-ac02-c006d327268b
```

## Running In A Container

Running the `gocounter` example in a container can take many forms.
This will step through the different options and lay the groundwork to running the application on
Kubernetes.

### Passing BPF Bytecode In A Container Image

Whether or not the Userspace portion of `gocounter` is running on a host or in a container, the
bpfd can load BPF bytecode from a container image built following the spec described in
[shipping-bytecode.md](../../docs/admin/shipping-bytecode.md).
A pre-built `gocounter` BPF container image can be loaded from `quay.io/bpfd/bytecode:gocounter`.
To use the container image, pass the URL to `gocounter`:

```console
    ./gocounter -iface ens3 -url quay.io/bpfd/bytecode:gocounter
    2022/09/23 16:11:45 Program registered with 6211ffff-4457-40a1-890d-44df316c39fb id
    2022/09/23 16:11:48 0 packets received
    2022/09/23 16:11:48 0 bytes received

    2022/09/23 16:11:51 0 packets received
    2022/09/23 16:11:51 0 bytes received
    ^C2022/09/23 16:11:51 Exiting...
    2022/09/23 16:11:52 Unloading Program: 6211ffff-4457-40a1-890d-44df316c39fb
```

### Building gocounter BPF Bytecode Container Image

[shipping-bytecode.md](../../docs/admin/shipping-bytecode.md) provides detailed instructions on
building and shipping bytecode in a container image.
To build a `gocounter` BPF bytecode container image, first make sure the bytecode has been built
(i.e. `bpf_bpfel.o` has been built - see [Building](#building]), then run the build command below:

```console
    cd src/bpfd/examples/gocounter/
    go generate

    podman build \
      --build-arg PROGRAM_NAME=gocounter \
      --build-arg SECTION_NAME=stats \
      --build-arg PROGRAM_TYPE=xdp \
      --build-arg BYTECODE_FILENAME=bpf_bpfel.o \
      --build-arg KERNEL_COMPILE_VER=$(uname -r) \
      -f Containerfile.bytecode . -t quay.io/$USER/bytecode:gocounter
```

`bpfd` currently only supports pulling a remote container image, so push the image to a remote
repository.
For example:

```console
    podman login quay.io
    podman push quay.io/$USER/bytecode:gocounter
```

Then run with the privately built bytecode container image:

```console
    ./gocounter -iface ens3 -url quay.io/$USER/bytecode:gocounter
    2022/09/23 16:11:45 Program registered with 6211ffff-4457-40a1-890d-44df316c39fb id
    2022/09/23 16:11:48 0 packets received
    2022/09/23 16:11:48 0 bytes received

    2022/09/23 16:11:51 0 packets received
    2022/09/23 16:11:51 0 bytes received
    ^C2022/09/23 16:11:51 Exiting...
    2022/09/23 16:11:52 Unloading Program: 6211ffff-4457-40a1-890d-44df316c39fb
```

### Building A gocounter Userspace Container Image

See [image-build.md](image-build.md) for details on building `bpfd` and `bpfctl` in a container image.
To build the `gocounter` Userspace example in a container, from the bpfd code source directory, run
the following build command:

```console
    cd src/bpfd/
    podman build -f examples/gocounter/container-deployment/Containerfile.gocounter . -t gocounter:local
```

To run the `gocounter` example in a container, run the following command:

```console
    podman run -it --privileged --net=host \
      -v /etc/bpfd/certs/ca/:/etc/bpfd/certs/ca/ \
      -v /etc/bpfctl/certs/gocounter/:/etc/bpfctl/certs/gocounter/ \
      -v /var/run/bpfd/fs/maps/:/var/run/bpfd/fs/maps/ \
      -e BPFD_INTERFACE=ens3 \
      -e BPFD_BYTECODE_URL=quay.io/bpfd/bytecode:gocounter \
      gocounter:local

    2022/09/30 15:41:23 Program registered with 42a4acf4-0412-463d-b948-d27db955efae id
    2022/09/30 15:41:26 4 packets received
    2022/09/30 15:41:26 580 bytes received

    2022/09/30 15:41:29 4 packets received
    2022/09/30 15:41:29 580 bytes received

    2022/09/30 15:41:32 8 packets received
    2022/09/30 15:41:32 1160 bytes received

    ^C2022/09/30 15:41:33 Exiting...
    2022/09/30 15:41:33 Unloading Program: 42a4acf4-0412-463d-b948-d27db955efae
```

`gocounter` can use the following environment variables to manage how it is started:
* **BPFD_INTERFACE:** Required: Interface to load bytecode on.
  Example: `-e BPFD_INTERFACE=ens3`
* **BPFD_PRIORITY:** Optional: Priority to load bytecode (lower value has higher priority).
  Example: `-e BPFD_PRIORITY=35`
* **BPFD_BYTECODE_URL:** Optional and mutually exclusive with BPFD_BYTECODE_UUID and
  BPFD_BYTECODE_PATH: URL of BPF bytecode container image.
  Example: `-e BPFD_BYTECODE_URL=quay.io/bpfd/bytecode:gocounter`
* **BPFD_BYTECODE_UUID:** Optional and mutually exclusive with BPFD_BYTECODE_URL and
  BPFD_BYTECODE_PATH: On a Kubernetes type environment, the BPF bytecode should be loaded by
  an administrator and using the `EbpfProgram` CRD.
  The UUID is the bpfd index to the loaded bytecode.
  Example: `-e BPFD_BYTECODE_UUID=5d843e8b-dd84-48bd-b34c-a5bc690bf7b2`
* **BPFD_BYTECODE_PATH:** Optional and mutually exclusive with BPFD_BYTECODE_URL and
  BPFD_BYTECODE_UUID: Not useful in the containerized deployment, specifies the location of the
  bytecode file a host system to load from.
  Example: `-e BPFD_BYTECODE_PATH=/var/bpfd/bytecode/bpf_bpfel.o`

The volume mounts provide access to the generated certificates and location of the shared map on the
host, where the counts are shared.

### Preloading gocounter BPF Bytecode

Another way to load the BPF bytecode is to pre-load the `gocounter` BPF bytecode and
pass the associated `bpfd` UUID to `gocounter` Userspace program.
This is similar to how BPF programs will be loaded in Kubernetes, except `kubectl` commands will be
used to create `EbpfProgram` CRD objects instead of using `bpfctl`, but that is covered in the next
section.
The `gocounter` Userspace program will skip the loading portion and use the UUID to find the shared
map and continue from there.

First, use `bpfctl` to load the `gocounter` BPF bytecode:

```console
    bpfctl load --iface vethb2795c7 --priority 50 --from-image quay.io/bpfd/bytecode:gocounter
    2609e15f-ed83-495f-9ff4-b2a08ba01eb6
```

Then run the `gocounter` Userspace program, passing in the UUID:

```console
    ./gocounter -iface vethb2795c7 -uuid 2609e15f-ed83-495f-9ff4-b2a08ba01eb6
    2022/09/30 13:51:35 100 packets received
    2022/09/30 13:51:35 14500 bytes received

    2022/09/30 13:51:38 100 packets received
    2022/09/30 13:51:38 14500 bytes received

    2022/09/30 13:51:41 104 packets received
    2022/09/30 13:51:41 15080 bytes received

    ^C2022/09/30 13:51:44 Exiting...
```

Then use `bpfctl` to unload the `gocounter` BPF bytecode:

```console
    bpfctl unload --iface vethb2795c7 2609e15f-ed83-495f-9ff4-b2a08ba01eb6
```

## Deploying On Kubernetes

This section will describe loading bytecode on a Kubernetes cluster and launching the Userspace
program.
The approach is slightly different when running on a Kubernetes cluster.
The BPF bytecode should be loaded by an administrator, not the Userspace program itself.

![gocounter On Kubernetes](../img/gocounter-on-k8s.png)


### Loading gocounter BPF Bytecode On Kubernetes

This assumes there is already a Kubernetes cluster running and `bpfd` is running in the cluster
(see [How to Manually Deploy bpfd on Kubernetes](../admin/k8s-deployment.md)).
Instead of using `bpfctl` to load the `gocounter` BPF bytecode, it will be loaded via the
`EbpfProgram` CRD type. Edit the
[gocounter-bytecode.yaml](../../examples/gocounter/kubernetes-deployment/gocounter-bytecode.yaml)
file to customize, primarily setting the interface.

```console
    vi examples/gocounter/kubernetes-deployment/gocounter-bytecode.yaml
    apiVersion: bpfd.io/v1alpha1
    kind: EbpfProgram
    metadata:
      name: gocounter
    spec:
      type: xdp
      name: gocounter
      interface: ens3                          # INTERFACE
      image: quay.io/bpfd/bytecode:gocounter
      sectionname: stats
      priority: 55
```

Then apply the update yaml:

```console
    kubectl apply -f examples/gocounter/kubernetes-deployment/gocounter-bytecode.yaml
    ebpfprogram.bpfd.io/gocounter created
```

To retrieve information on the `EbpfProgram` objects:

```console
    kubectl get EbpfPrograms -A
    NAMESPACE     NAME                  AGE
    kube-system   gocounter             9s

    kubectl describe EbpfPrograms -n kube-system gocounter
    Name:         gocounter
    Namespace:    kube-system
    Labels:       <none>
    Annotations:  ebpf-k8s-1/attach_point: ens3
                  ebpf-k8s-1/uuid: 49284c27-c623-4aa7-88b4-99116815e78e
                  ebpf-k8s-2/attach_point: ens3
                  ebpf-k8s-2/uuid: 62c2a443-9beb-4261-9c46-33a29612643d
                  ebpf-k8s-3/attach_point: ens3
                  ebpf-k8s-3/uuid: dff67fcf-10dc-4b49-8c49-d74a43f16a4c
    API Version:  bpfd.io/v1alpha1
    Kind:         EbpfProgram
    Metadata:
      Creation Timestamp:  2022-09-29T20:40:54Z
    :
```

Using the annotations, the UUID of each node can be determined.

### Loading gocounter Userspace Container On Kubernetes

As described above, the Userspace program can be built into a container.
In this case, it can be pushed to a upstream repository to use across multiple systems.

```console
    cd src/bpfd/
    podman build -f examples/gocounter/container-deployment/Containerfile.gocounter . -t quay.io/$USER/gocounter:latest

    podman login quay.io
    podman push quay.io/$USER/gocounter:latest
```

Using the UUID from annotation collected in the previous section, update the
[gocounter.yaml](../../examples/gocounter/kubernetes-deployment/gocounter.yaml)
with the interface, UUID, Node Selector and private image:

```console
    vi src/bpfd/examples/gocounter/kubernetes-deployment/gocounter.yaml
    ---
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: gocounter-config
      namespace: kube-system
    data:
      gocounter.toml: |
        [tls]
          ca_cert = "/etc/bpfd/certs/ca/ca.crt"
          cert = "/etc/bpfctl/certs/gocounter/tls.crt"
          key = "/etc/bpfctl/certs/gocounter/tls.key"
        [config]
          interface = "ens3"                                       # INTERFACE
          bytecode_uuid = "62c2a443-9beb-4261-9c46-33a29612643d"   # UUID
    ---
    :
    ---
    apiVersion: v1
    kind: Pod
    metadata:
      name: gocounter-pod
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      terminationGracePeriodSeconds: 30
      nodeSelector:
        kubernetes.io/hostname: ebpf-k8s-2                         # NODE SELECTOR
      containers:
      - name: gocounter
        image: quay.io/$USER/gocounter:latest                      # IMAGE
        imagePullPolicy: IfNotPresent
    :
```

The `gocounter` Userspace Program also takes input from a `/etc/bpfctl/gocounter.toml` file.
The [gocounter.yaml](../../examples/gocounter/kubernetes-deployment/gocounter.yaml)
creates a configMap which is mapped to a `/etc/bpfctl/gocounter.toml` file on the node.
This allows the specific attributes to be passed to the `gocounter` Userspace Program.
It also allows the name of the certificates to be named in the CertManager format.

Once the [gocounter.yaml](../../examples/gocounter/kubernetes-deployment/gocounter.yaml)
is tailored to a specific deployment, it can be applied:

```console
    kubectl apply -f examples/gocounter/kubernetes-deployment/gocounter.yaml
    configmap/gocounter-config created
    certificate.cert-manager.io/gocounter-cert created
    pod/gocounter-pod created
```

View the logs of `gocounter-pod` to see the counters:

```console
    kubectl logs -n kube-system gocounter-pod

    2022/09/30 18:30:50 188000 packets received
    2022/09/30 18:30:50 86781557 bytes received

    2022/09/30 18:30:53 188011 packets received
    2022/09/30 18:30:53 86788674 bytes received

    2022/09/30 18:30:56 188017 packets received
    2022/09/30 18:30:56 86789701 bytes received

    :
```

To tear down:

```console
    kubectl delete -f examples/gocounter/kubernetes-deployment/gocounter.yaml
    configmap "gocounter-config" deleted
    certificate.cert-manager.io "gocounter-cert" deleted
    pod "gocounter-pod" deleted

    kubectl delete -f examples/gocounter/kubernetes-deployment/gocounter-bytecode.yaml
    ebpfprogram.bpfd.io "gocounter" deleted
```

## Notes

Notes regarding this document:
* Source of images used in this document can be found in
  [bpfd Upstream Images](https://docs.google.com/presentation/d/1wU4xu6xeyk9cB3G-Nn-dzkf90j1-EI4PB167G7v-Xl4/edit?usp=sharing).
  Request access if required.
