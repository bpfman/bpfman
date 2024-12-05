# Example eBPF Programs

Example applications that use the `bpfman-go` bindings can be found in the
[bpfman/examples/](https://github.com/bpfman/bpfman/tree/main/examples/) directory.
Current examples include:

* [bpfman/examples/go-app-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-app-counter)
* [bpfman/examples/go-kprobe-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-kprobe-counter)
* [bpfman/examples/go-target/](https://github.com/bpfman/bpfman/tree/main/examples/go-target)
* [bpfman/examples/go-tc-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-tc-counter)
* [bpfman/examples/go-tcx-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-tcx-counter)
* [bpfman/examples/go-tracepoint-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-tracepoint-counter)
* [bpfman/examples/go-uprobe-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-uprobe-counter)
* [bpfman/examples/go-uretprobe-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-uretprobe-counter)
* [bpfman/examples/go-xdp-counter/](https://github.com/bpfman/bpfman/tree/main/examples/go-xdp-counter)

## Example Code Breakdown

These examples and the associated documentation are intended to provide the basics on how to deploy
and manage an eBPF program using bpfman. Each of the examples contains an eBPF Program(s) written in C
([app_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-app-counter/bpf/app_counter.c),
[kprobe_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-kprobe-counter/bpf/kprobe_counter.c),
[tc_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-tc-counter/bpf/tc_counter.c),
[tcx_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-tcx-counter/bpf/tcx_counter.c),
[tracepoint_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-tracepoint-counter/bpf/tracepoint_counter.c),
[uprobe_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-uprobe-counter/bpf/uprobe_counter.c),
[uretprobe_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-uretprobe-counter/bpf/uretprobe_counter.c),
and
[xdp_counter.c](https://github.com/bpfman/bpfman/tree/main/examples/go-xdp-counter/bpf/xdp_counter.c))
that is compiled into eBPF bytecode for each supported architecture (bpf_arm64_bpfel.o, bpf_powerpc_bpfel.o,
bpf_s390_bpfeb.o and bpf_x86_bpfel.o).
Each time the eBPF program is called, it increments the packet and byte counts in a map that is accessible
by the userspace portion.

Each of the examples also have a userspace portion written in GO.
The userspace code is leveraging the [cilium/ebpf library](https://github.com/cilium/ebpf)
to manage the maps shared with the eBPF program.
The example eBPF programs are very similar in functionality, and only vary where in the Linux networking stack
they are inserted.
The userspace program then polls the eBPF map every 3 seconds and logs the current counts.

The examples were written to either run locally on a host or run in a container in a Kubernetes
deployment.
The userspace code flow is slightly different depending on the deployment, so input parameters
dictate the deployment method.

### Examples in Local Deployment

When run locally, the userspace program makes gRPC calls to `bpfman-rpc` requesting `bpfman` to load
the eBPF program at the requested hook point (TC hook point, Tracepoint, XDP hook point, etc).
Data sent in the RPC request is either defaulted or passed in via input parameters.
To make the examples as simple as possible to run, all input data is defaulted (except the interface
TC and XDP programs need to attach to) but can be overwritten if desired. All example programs have
the following common parameters (kprobe does not have any command specific parameters):

```console
cd bpfman/examples/go-kprobe-counter/

./go-kprobe-counter --help
Usage of ./go-kprobe-counter:
  -crd
      Flag to indicate all attributes should be pulled from the BpfProgram CRD.
      Used in Kubernetes deployments and is mutually exclusive with all other
      parameters.
  -file string
      File path of bytecode source. "file" and "image"/"id" are mutually exclusive.
      Example: -file /home/$USER/src/bpfman/examples/go-kprobe-counter/bpf_x86_bpfel.o
  -id uint
      Optional Program ID of bytecode that has already been loaded. "id" and
      "file"/"image" are mutually exclusive.
      Example: -id 28341
  -image string
      Image repository URL of bytecode source. "image" and "file"/"id" are
      mutually exclusive.
      Example: -image quay.io/bpfman-bytecode/go-kprobe-counter:latest
  -map_owner_id int
      Program Id of loaded eBPF program this eBPF program will share a map with.
      Example: -map_owner_id 9785
```

The location of the eBPF bytecode can be provided four different ways:

* Defaulted: If nothing is passed in, the code scans the local directory for
  a `bpf_x86_bpfel.o` file. If found, that is used. If not, it errors out.
* **file**: Fully qualified path of the bytecode object file.
* **image**: Image repository URL of bytecode source.
* **id**: Kernel program Id of a bytecode that has already been loaded. This
  program could have been loaded using `bpftool`, or `bpfman`. 

If two userspace programs need to share the same map, **map_owner_id** is the Program
ID of the first loaded program that has the map the second program wants to share.

The examples require `sudo` to run because they require access the Unix socket `bpfman-rpc`
is listening on.
[Deploying Example eBPF Programs On Local Host](./example-bpf-local.md) steps through launching
`bpfman` locally and running some of the examples. 

### Examples in Kubernetes Deployment

When run in a Kubernetes deployment, all the input data is passed to Kubernetes through yaml files.
To indicate to the userspace code that it is in a Kubernetes deployment and not to try to load
the eBPF bytecode, the example is launched in the container with the **crd** flag.
Example: `./go-kprobe-counter -crd`

For these examples, the bytecode is loaded via one yaml file which creates a *Program CRD Object
(KprobeProgram, TcProgram, TracepointProgram, etc.) and the userspace pod is loaded via another yaml
file.
In a more realistic deployment, the userspace pod may have the logic to send the \*Program CRD Object
create request to the KubeAPI Server, but the two yaml files are load manually for simplicity in the
example code.
The [examples directory](https://github.com/bpfman/bpfman/tree/main/examples) contain yaml files to
load each example, leveraging [Kustomize](https://kustomize.io/) to modify the yaml to load the latest
images from Quay.io, to load custom images or released based images.
It is recommended to use the commands built into the Makefile, which run kustomize, to apply and remove
the yaml files to a Kubernetes cluster.
Use `make help` to see all the make options.
For example:

```console
cd bpfman/examples/

# Deploy then undeploy all the examples
make deploy
make undeploy

OR

# Deploy then undeploy just the TC example
make deploy-tc
make undeploy-tc
```

[Deploying Example eBPF Programs On Kubernetes](./example-bpf-k8s.md) steps through deploying
bpfman to multiple nodes in a Kubernetes cluster and loading the examples.

## Building Example Code

All the examples can be built locally as well as packaged in a container for Kubernetes
deployment.

### Building Locally

To build directly on a system, make sure all the prerequisites are met, then build.

#### Prerequisites

_This assumes bpfman is already installed and running on the system.
If not, see [Setup and Building bpfman](./building-bpfman.md)._

1. All [requirements defined by the `cilium/ebpf` package](https://github.com/cilium/ebpf#requirements)
2. libbpf development package to get the required eBPF c headers

    **Fedora:** `sudo dnf install libbpf-devel`

    **Ubuntu:** `sudo apt-get install libbpf-dev`

#### Build

To build all the C based eBPF counter bytecode, run:

```console
cd bpfman/examples/
make generate
```

To build all the Userspace GO Client examples, run:

```console
cd bpfman/examples/
make build
```

To build only a single example:

```console
cd bpfman/examples/go-tracepoint-counter/
go generate
go build
```

_Other program types are the same._

### Building eBPF Bytecode Container Image

[eBPF Bytecode Image Specifications](../developer-guide/shipping-bytecode.md) provides detailed
instructions on building and shipping bytecode in a container image.
Pre-built eBPF container images for the examples can be loaded from:

- `quay.io/bpfman-bytecode/go-app-counter:latest`
- `quay.io/bpfman-bytecode/go-kprobe-counter:latest`
- `quay.io/bpfman-bytecode/go-tc-counter:latest`
- `quay.io/bpfman-bytecode/go-tcx-counter:latest`
- `quay.io/bpfman-bytecode/go-tracepoint-counter:latest`
- `quay.io/bpfman-bytecode/go-uprobe-counter:latest`
- `quay.io/bpfman-bytecode/go-uretprobe-counter:latest`
- `quay.io/bpfman-bytecode/go-xdp-counter:latest`

To build the example eBPF bytecode container images, first generate the bytecode (the `generate`
commands require the [Prerequisites](#prerequisites) described above in the
[Building Locally](#building-locally) section).

To generate the bytecode for all the examples:

```console
cd bpfman/examples/
make generate
```

OR to generate the bytecode for a single example (XDP in this case):

```console
cd bpfman/examples/go-xdp-counter/
go generate
```

The preferred method for building the container image is to use the `bpfman image build`
command.
See [bpfman image build](./cli-guide.md#bpfman-image-build) in the CLI Guide for more details.

```console
cd bpfman/examples/go-xdp-counter/
bpfman image build -f ../../Containerfile.bytecode -t quay.io/$QUAY_USER/go-xdp-counter-bytecode:test -b bpf_x86_bpfel.o
```

The examples [Makefile](https://github.com/bpfman/bpfman/blob/main/examples/Makefile) has
commands to build all the example images if needed.
See [Locally Build Example Container Images](../developer-guide/image-build.md#locally-build-example-container-images)
for more details.

`bpfman` currently does not provide a method for pre-loading bytecode images
(see [issue #603](https://github.com/bpfman/bpfman/issues/603)), so push the bytecode image to an image
repository.

For example:

```console
docker login quay.io
docker push quay.io/$QUAY_USER/go-xdp-counter-bytecode:test
```

## Running Examples

Below are some examples of how to run the bpfman examples on a host where bpfman is already installed.

```console
cd bpfman/examples/go-xdp-counter/
sudo ./go-xdp-counter -iface <INTERNET INTERFACE NAME>
```

or (**NOTE:** TC programs also require a direction, ingress or egress)

```console
cd bpfman/examples/go-tc-counter/
sudo ./go-tc-counter -direction ingress -iface <INTERNET INTERFACE NAME>
```

or

```console
cd bpfman/examples/go-tracepoint-counter/
sudo ./go-tracepoint-counter
```

bpfman can load eBPF bytecode from a container image built following the spec described in
[eBPF Bytecode Image Specifications](../developer-guide/shipping-bytecode.md).

To use the container image, pass the URL to the userspace program:

```console
sudo ./go-xdp-counter -iface eno3 -image quay.io/bpfman-bytecode/go-xdp-counter:latest
2022/12/02 16:28:32 Using Input: Interface=eno3 Priority=50 Source=quay.io/bpfman-bytecode/go-xdp-counter:latest
2022/12/02 16:28:34 Program registered with id 6223
2022/12/02 16:28:37 4 packets received
2022/12/02 16:28:37 580 bytes received

2022/12/02 16:28:40 4 packets received
2022/12/02 16:28:40 580 bytes received

^C2022/12/02 16:28:42 Exiting...
2022/12/02 16:28:42 Unloading Program: 6223
```

Or to run with the privately built bytecode container image:

```console
sudo ./go-xdp-counter -iface eno3 -image quay.io/$QUAY_USER/go-xdp-counter-bytecode:test
2022/12/02 16:38:44 Using Input: Interface=eno3 Priority=50 Source=quay.io/$QUAY_USER/go-xdp-counter-bytecode:test
2022/12/02 16:38:45 Program registered with id 6225
2022/12/02 16:38:48 4 packets received
2022/12/02 16:38:48 580 bytes received

2022/12/02 16:38:51 4 packets received
2022/12/02 16:38:51 580 bytes received

^C2022/12/02 16:38:51 Exiting...
2022/12/02 16:38:51 Unloading Program: 6225
```
