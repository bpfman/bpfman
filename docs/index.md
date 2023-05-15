# Welcome to bpfd

bpfd is a system daemon aimed at simplifying the deployment and management of eBPF programs.
It's goal is to enhance the developer-experience as well as provide features to improve security,
visibility and program-cooperation.
bpfd includes a Kubernetes operator to bring those same features to Kubernetes, allowing users to
safely deploy eBPF via custom resources across nodes in a cluster.

## Why eBPF?

eBPF is a powerful general-purpose framework that allows running sandboxed
programs in the kernel.  It can be used for many purposes, including networking,
monitoring, tracing and security.

## Why eBPF in Kubernetes?

Demand is increasing from both Kubernetes developers and users. Examples of eBPF
in Kubernetes include:

- [Cilium](https://cilium.io/)
  [Calico](https://www.tigera.io/project-calico/) CNIs 
- [Pixie](https://px.dev/): Open source observability
- [KubeArmor](https://kubearmor.io/): Container-aware runtime security
  enforcement system
- [Blixt](https://github.com/Kong/blixt): Gateway API L4 conformance
  implementation
- [NetObserv](https://github.com/netobserv): Open source operator for network
  observability 

## Challenges for eBPF in Kubernetes

- Requires privileged pods.
  - eBPF-enabled apps require at least CAP_BPF permissions and potentially more
    depending on the type of program that is being attached.
  - Since the Linux capabilities are very broad it is challenging to constrain a
    pod to the minimum set of privileges required. This can allow them to do
    damage (either unintentionally or intentionally).
- Handling multiple eBPF programs on the same eBPF hooks.
  - Not all eBPF hooks are designed to support multiple programs.
  - Some software using eBPF assumes exclusive use of a eBPF hook and can
    unintentionally eject existing programs when being attached. This can result
    in silent failures and non-deterministic failures.
- Debugging problems with deployments is hard.
  - The cluster administrator may not be aware that eBPF programs are being used
    in a cluster.
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
  - In eBPF enabled K8s deployments today, the eBPF Program is often embedded into
    the userspace binary that loads and interacts with it. This means there's no
    easy way to have fine-grained versioning control of the bpfProgram in
    relation to it's accompanying userspace counterpart.

## What is bpfd?

bpfd is a software stack that aims to make it easy to load, unload, modify and
monitor eBPF programs whether on a single host, or in a Kubernetes cluster.
bpfd includes the following core components:

  - bpfd: A system daemon that supports loading, unloading, modifying and
    monitoring of eBPF programs exposed over a gRPC API.
  - eBPF CRDS: bpfd provides a set of CRDs (`XdpProgram`, `TcProgram`, etc.)
    that provide a way to express intent to load eBPF programs as well as a bpfd
    generated CRD (`BpfProgram`) used to represent the runtime state of loaded
    programs.
  - bpfd-agent: The agent runs in a container in the bpfd daemonset and ensures
    that the requested eBPF programs for a given node are in the desired state.
  - bpfd-operator: An operator, built using [Operator
    SDK](https://sdk.operatorframework.io/), that manages the installation and
    lifecycle of bpfd-agent and the CRDs in a Kubernetes cluster.

bpfd is developed in Rust and built on top of Aya, a Rust eBPF library.

The benefits of this solution include the following:

- Security
  - Improved security because only the bpfd daemon, which can be tightly
    controlled, has the privileges needed to load eBPF programs, while access to
    the API can be controlled via standard RBAC methods.  Within bpfd, only a
    single thread keeps these capabilities while the other threads (serving
    RPCs) do not.
  - Gives the administrators control over who can load programs.
  - Allows administrators to define rules for the ordering of networking eBPF
    programs. (ROADMAP)
- Visibility/Debuggability
  - Improved visibility into what eBPF programs are running on a system, which
    enhances the debuggability for developers, administrators, and customer
    support.
  - The greatest benefit is achieved when all apps use bpfd, but even if they
    don't, bpfd can provide visibility into all the eBPF programs loaded on the
    nodes in a cluster.
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
    pin management, etc.) and use existing eBPF libraries to interact with their
    program maps using well defined pin points which are managed by bpfd.
  - Developers can still use Cilium/libbpf/Aya/etc libraries for eBPF
    development, and load/unload with bpfd.
  - Provides eBPF Bytecode Image Specifications that allows fine-grained separate
    versioning control for userspace and kernelspace programs.  This also allows
    for signing these container images to verify bytecode ownership.

For more details, please see the following:

- [Setup and Building bpfd](https://bpfd.netlify.app/building-bpfd/) for
  instructions on setting up your development environment and building bpfd.
- [Tutorial](https://bpfd.netlify.app/tutorial/) for some examples of starting
  `bpfd`, managing logs, and using `bpfctl`.
- [Example eBPF Programs](https://bpfd.netlify.app/example-ebpf/) for some
  examples of eBPF programs written in Go, interacting with `bpfd`.
- [How to Deploy bpfd on Kubernetes](https://bpfd.netlify.app/k8s-deployment/)

## License

### bpf

Code in this crate is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in this crate by you, as defined in the GPL-2 license, shall be
dual licensed as above, without any additional terms or conditions.

### bpfd, bpfd-common

Rust code in all other crates is distributed under the terms of either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in this crate by you, as defined in the Apache-2.0 license, shall
be dual licensed as above, without any additional terms or conditions.

The `bpfd` crate also contains eBPF code that is distributed under the terms of
the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at
your option. It is packaged, in object form, inside the `bpfd` binary.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL2
[BSD 2 Clause]: LICENSE-BSD2
