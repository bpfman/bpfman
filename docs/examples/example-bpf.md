# Example BPF Programs

Example applications that use the `bpfd-go` bindings can be found in the
[examples/](https://github.com/bpfd-dev/bpfd/tree/main/examples/) directory.
Current examples include:

* [examples/go-tc-counter/](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tc-counter)
* [examples/go-tracepoint-counter/](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tracepoint-counter)
* [examples/go-xdp-counter/](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-xdp-counter)

These examples and the associated documentation is intended to provide the basics on how to deploy
and manage a BPF program using bpfd. Each of the examples contain a BPF Program written in C
([tc_counter.c](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tc-counter/bpf/tc_counter.c),
[tracepoint_counter.c](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tracepoint-counter/bpf/tracepoint_counter.c) and
[xdp_counter.c](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-xdp-counter/bpf/xdp_counter.c))
that is compiled into BPF bytecode.
Each time the BPF program is called, it increments the packet and byte counts in a map that is accessible
by the userspace portion.

Each of the examples also have a userspace portion written in GO.
When run locally, the userspace program makes gRPC calls to `bpfd` requesting `bpfd` to load the BPF program
at the requested hook point (XDP hook point, TC hook point or Tracepoint).
When run in a Kubernetes deployment, the `bpfd-agent` makes gRPC calls to `bpfd` requesting `bpfd` to load
the BPF program based on a Custom Resource Definition (CRD), which is described in more detail in that section.
Independent of the deployment, the userspace program then polls the BPF map every 3 seconds and logs the
current counts.
The userspace code is leveraging the [cilium/ebpf library](https://github.com/cilium/ebpf)
to manage the maps shared with the BPF program.
The example BPF programs are very similar in functionality, and only vary where in the Linux networking stack
they are inserted.
Read more about XDP and TC programs [here](https://docs.cilium.io/en/latest/bpf/progtypes/).

There are two ways to deploy these example applications:

* Run locally on one machine: [Deploying Example BPF Programs On Local Host](./example-bpf-local.md)
* Deploy to multiple nodes in a Kubernetes cluster: [Deploying Example BPF Programs On Kubernetes](./example-bpf-k8s.md)

## Notes

Notes regarding this document:

- Source of images used in the example documentation can be found in
  [bpfd Upstream Images](https://docs.google.com/presentation/d/1wU4xu6xeyk9cB3G-Nn-dzkf90j1-EI4PB167G7v-Xl4/edit?usp=sharing).
  Request access if required.
