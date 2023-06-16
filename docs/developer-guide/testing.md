# Testing

This document describes the automated testing that is done for each pull request
submitted to [bpfd](https://github.com/bpfd-dev/bpfd).

## Unit Testing

Unit testing is executed as part of the `build` job  by running the `cargo test`
command in the top-level bpfd directory.

## Go Example Tests

Tests are run for each of the example programs found in directory `examples`

Detailed description TBD

## Basic Integration Tests

Basic integration tests are executed by running the `cargo xtask
integration-test` command in the top-level bpfd directory.

The integration tests start a `bpfd` daemon process, and issue `bpfctl` commands
to verify a range of functionality.  For XDP and TC programs that are installed
on network interfaces, the integration test code creates a test network
namespace connected to the host by a veth pair on which the programs are
attached. The test code uses the IP subnet 172.37.37.1/24 for the namespace. If
that address conflicts with an existing network on the host, it can be changed
by setting the `BPFD_IP_PREFIX` environment variable to one that is available as
shown below.

```bash
export BPFD_IP_PREFIX="192.168.50"
```

There are two categories of integration tests: basic and e2e.  The basic tests
verify basic `bpfctl` functionality such as loading, listing, and unloading
programs.  The e2e tests verify more advanced functionality such as the setting
of global variables, priority, and proceed-on by installing the programs,
creating traffic if needed, and examining logs to confirm that things are
running as expected.

eBPF test programs are loaded from container images stored on
[quay.io](https://quay.io/repository/bpfd-bytecode/tc_pass). The source code for
the eBPF test programs can be found in the `tests/integration-test/bpf`
directory.  These programs are compiled by executing `cargo xtask build-ebpf
--libbpf-dir <libbpf dir>`

The `bpf` directory also contains a script called `build_push_images.sh` that
can be used to build and push new images to quay if the code is changed.  We
plan to push the images automatically when code gets merged ([issue
\#533](<https://github.com/bpfd-dev/bpfd/issues/533>)). However, it's still
useful to be able to push them manually sometimes. For example, when a new test
case requires that both the eBPF and integration code be changed together.  It
is also a useful template for new eBPF test code that needs to be pushed.
However, as a word of caution, be aware that existing integration tests will
start using the new programs immediately, so this should only be done if the
modified program is backward compatible.

## Kubernetes Integration Tests

Detailed decription TBD
