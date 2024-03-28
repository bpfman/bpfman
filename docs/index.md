![bpfman logo](./img/bpfman_logo_256.png) <!-- markdownlint-disable-line first-line-heading -->

_Formerly know as `bpfd`_

# bpfman: An eBPF Manager

bpfman operates as an eBPF manager, focusing on simplifying the deployment and administration of eBPF programs. Its notable features encompass:

- **System Overview**: Provides insights into how eBPF is utilized in your system.
- **eBPF Program Loader**: Includes a built-in program loader that supports program cooperation for XDP and TC programs, as well as deployment of eBPF programs from OCI images.
- **eBPF Filesystem Management**: Manages the eBPF filesystem, facilitating the deployment of eBPF applications without requiring additional privileges.

Our program loader and eBPF filesystem manager ensure the secure deployment of eBPF applications.
Furthermore, bpfman includes a Kubernetes operator, extending these capabilities to Kubernetes. This allows users to confidently deploy eBPF through custom resource definitions across nodes in a cluster.

## Why eBPF?

eBPF is a powerful general-purpose framework that allows running sandboxed
programs in the kernel. It can be used for many purposes, including networking,
monitoring, tracing and security.

## Why eBPF in Kubernetes?

Demand is increasing from both Kubernetes developers and users. Examples of eBPF
in Kubernetes include:

- [Cilium](https://cilium.io/) and [Calico](https://www.tigera.io/project-calico/)
  CNIs
- [Pixie](https://px.dev/): Open source observability
- [KubeArmor](https://kubearmor.io/): Container-aware runtime security
  enforcement system
- [Blixt](https://github.com/Kong/blixt): Gateway API L4 conformance
  implementation
- [NetObserv](https://github.com/netobserv): Open source operator for network
  observability

## Challenges for eBPF in Kubernetes

- Requires privileged pods.
  - eBPF-enabled apps require at least CAP_BPF permissions and potentially
    more depending on the type of program that is being attached.
  - Since the Linux capabilities are very broad it is challenging to constrain
    a pod to the minimum set of privileges required. This can allow them to do
    damage (either unintentionally or intentionally).
- Handling multiple eBPF programs on the same eBPF hooks.
  - Not all eBPF hooks are designed to support multiple programs.
  - Some software using eBPF assumes exclusive use of an eBPF hook and can
    unintentionally eject existing programs when being attached. This can
    result in silent failures and non-deterministic failures.
- Debugging problems with deployments is hard.
  - The cluster administrator may not be aware that eBPF programs are being
    used in a cluster.
  - It is possible for some eBPF programs to interfere with others in
    unpredictable ways.
  - SSH access or a privileged pod is necessary to determine the state of eBPF
    programs on each node in the cluster.
- Lifecycle management of eBPF programs.
  - While there are libraries for the basic loading and unloading of eBPF
    programs, a lot of code is often needed around them for lifecycle
    management.
- Deployment on Kubernetes is not simple.
  - It is an involved process that requires first writing a daemon that loads
    your eBPF bytecode and deploying it using a DaemonSet.
  - This requires careful design and intricate knowledge of the eBPF program
    lifecycle to ensure your program stays loaded and that you can easily
    tolerate pod restarts and upgrades.
  - In eBPF enabled K8s deployments today, the eBPF Program is often embedded
    into the userspace binary that loads and interacts with it. This means
    there's no easy way to have fine-grained versioning control of the
    bpfProgram in relation to it's accompanying userspace counterpart.

## What is bpfman?

bpfman is a software stack that aims to make it easy to load, unload, modify and
monitor eBPF programs whether on a single host, or in a Kubernetes cluster. bpfman
includes the following core components:

- bpfman: A system daemon that supports loading, unloading, modifying and
  monitoring of eBPF programs exposed over a gRPC API.
- eBPF CRDS: bpfman provides a set of CRDs (`XdpProgram`, `TcProgram`, etc.) that
  provide a way to express intent to load eBPF programs as well as a bpfman
  generated CRD (`BpfProgram`) used to represent the runtime state of loaded
  programs.
- bpfman-agent: The agent runs in a container in the bpfman daemonset and ensures
  that the requested eBPF programs for a given node are in the desired state.
- bpfman-operator: An operator, built using [Operator
  SDK](https://sdk.operatorframework.io/), that manages the installation and
  lifecycle of bpfman-agent and the CRDs in a Kubernetes cluster.

bpfman is developed in Rust and built on top of Aya, a Rust eBPF library.

The benefits of this solution include the following:

- Security
  - Improved security because only the bpfman daemon, which can be tightly
    controlled, has the privileges needed to load eBPF programs, while access
    to the API can be controlled via standard RBAC methods. Within bpfman, only
    a single thread keeps these capabilities while the other threads (serving
    RPCs) do not.
  - Gives the administrators control over who can load programs.
  - Allows administrators to define rules for the ordering of networking eBPF
    programs. (ROADMAP)
- Visibility/Debuggability
  - Improved visibility into what eBPF programs are running on a system, which
    enhances the debuggability for developers, administrators, and customer
    support.
  - The greatest benefit is achieved when all apps use bpfman, but even if they
    don't, bpfman can provide visibility into all the eBPF programs loaded on
    the nodes in a cluster.
- Multi-program Support
  - Support for the coexistence of multiple eBPF programs from multiple users.
  - Uses the [libxdp multiprog
    protocol](https://github.com/xdp-project/xdp-tools/blob/master/lib/libxdp/protocol.org)
    to allow multiple XDP programs on single interface
  - This same protocol is also supported for TC programs to provide a common
    multi-program user experience across both TC and XDP.
- Productivity
  - Simplifies the deployment and lifecycle management of eBPF programs in a
    Kubernetes cluster.
  - developers can stop worrying about program lifecycle (loading, attaching,
    pin management, etc.) and use existing eBPF libraries to interact with
    their program maps using well defined pin points which are managed by
    bpfman.
  - Developers can still use Cilium/libbpf/Aya/etc libraries for eBPF
    development, and load/unload with bpfman.
  - Provides eBPF Bytecode Image Specifications that allows fine-grained
    separate versioning control for userspace and kernelspace programs. This
    also allows for signing these container images to verify bytecode
    ownership.

For more details, please see the following:

- [bpfman Overview](./getting-started/overview.md) for an overview of bpfman.
- [Deploying Example eBPF Programs On Local Host](./getting-started/example-bpf-local.md)
  for some examples of running `bpfman` on local host and using the CLI to install
  eBPF programs on the host.
- [Deploying Example eBPF Programs On Kubernetes](./getting-started/example-bpf-k8s.md)
  for some examples of deploying eBPF programs through `bpfman` in a Kubernetes deployment.
- [Setup and Building bpfman](./getting-started/building-bpfman.md) for instructions
  on setting up your development environment and building bpfman.
- [Example eBPF Programs](./getting-started/example-bpf.md) for some examples of
  eBPF programs written in Go, interacting with `bpfman`.
- [Deploying the bpfman-operator](./developer-guide/operator-quick-start.md) for
  details on launching bpfman in a Kubernetes cluster.
- [Meet the Community](./governance/MEETINGS.md) for details on community
  meeting details.
