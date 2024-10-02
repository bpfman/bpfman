# Testing

This document describes the automated testing that is done for each pull request
submitted to [bpfman](https://github.com/bpfman/bpfman), and also provides
instructions for running them locally when doing development.

## Unit Testing

Unit testing is executed as part of the `build` job by running the following
command in the top-level bpfman directory.

```
cd bpfman/
cargo test
```

## Go Example Tests

Tests are run for each of the example programs found in directory `examples`

Detailed description TBD

## Basic Integration Tests

The full set of basic integration tests are executed by running the following
command in the top-level bpfman directory.

```bash
cd bpfman/
cargo xtask integration-test
```

Optionally, a subset of the integration tests can be run by adding the "--" and
a list of one or more names at the end of the command as shown below.

```bash
cargo xtask integration-test -- test_load_unload_xdp test_proceed_on_xdp
```

The integration tests start a `bpfman` daemon process, and issue CLI commands
to verify a range of functionality.  For XDP and TC programs that are installed
on network interfaces, the integration test code creates a test network
namespace connected to the host by a veth pair on which the programs are
attached. The test code uses the IP subnet 172.37.37.1/24 for the namespace. If
that address conflicts with an existing network on the host, it can be changed
by setting the `BPFMAN_IP_PREFIX` environment variable to one that is available as
shown below.

```bash
export BPFMAN_IP_PREFIX="192.168.50"
```

If bpfman logs are needed to help debug an integration test, set `RUST_LOG` either
globally or for a given test.

```bash
export RUST_LOG=info
```
OR
```bash
RUST_LOG=info cargo xtask integration-test -- test_load_unload_xdp test_proceed_on_xdp
```

There are two categories of integration tests: basic and e2e.  The basic tests
verify basic CLI functionality such as loading, listing, and unloading
programs.  The e2e tests verify more advanced functionality such as the setting
of global variables, priority, and proceed-on by installing the programs,
creating traffic if needed, and examining logs to confirm that things are
running as expected.

Most eBPF test programs are loaded from container images stored on
[quay.io](https://quay.io/repository/bpfman-bytecode/tc_pass). The source code for
the eBPF test programs can be found in the `tests/integration-test/bpf`
directory.  These programs are compiled by executing `cargo xtask build-ebpf
--libbpf-dir <libbpf dir>`

We also load some tests from local files to test the `bpfman load file` option.

## Kubernetes Operator Tests

### Kubernetes Operator Unit Tests

To run all of the unit tests defined in the bpfman-operator controller code run
`make test` in the bpfman-operator directory.

```bash
cd bpfman-operator/
make test
```

### Kubernetes Operator Integration Tests

To run the Kubernetes Operator integration tests locally:

1. Build the example test code userspace images locally.

    ```bash
    cd bpfman/examples/
    make build-us-images
    ```

2. (optional) build the bytecode images

    In order to rebuild all of the bytecode images for a PR, ask a maintainer to do so,
    they will be built and generate by github actions with the tag
    `quay.io/bpfman-bytecode/<example>:<branch-name>`

3. Build the bpfman images locally with a unique tag, for example: `int-test`

    ```bash
    cd bpfman-operator/
    BPFMAN_AGENT_IMG=quay.io/bpfman/bpfman-agent:int-test BPFMAN_OPERATOR_IMG=quay.io/bpfman/bpfman-operator:int-test make build-images
    ```

4. Run the integration test suite with the images from the previous step:

    ```bash
    cd bpfman-operator/
    BPFMAN_AGENT_IMG=quay.io/bpfman/bpfman-agent:int-test BPFMAN_OPERATOR_IMG=quay.io/bpfman/bpfman-operator:int-test make test-integration
    ```

    If an update `bpfman` image is required, build it separately and pass to `make test-integration` using `BPFMAN_IMG`.
    See [Locally Build bpfman Container Image](./image-build.md#locally-build-bpfman-container-image).

    Additionally the integration test can be configured with the following environment variables:

    * **KEEP_TEST_CLUSTER**: If set to `true` the test cluster will not be torn down
      after the integration test suite completes.
    * **USE_EXISTING_KIND_CLUSTER**: If this is set to the name of the existing kind
      cluster the integration test suite will use that cluster instead of creating a
      new one.
