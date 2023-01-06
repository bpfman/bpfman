# bpfd-operator

The Bpfd-Operator repository exists in order to deploy and manage bpfd within a kubernetes cluster.

![bpfd on K8s](./bpfd-on-k8s.jpg)

## Description

This repository houses two main processes, the `bpfd-agent` and the `bpfd-operator` along with CRD api definitions for `BpfProgram` and `BpfProgramConfig` Objects. In the below sections we'll dive a bit deeper into the API and functionality of both processes.

But first try it out!

## Getting started

> **Warning**: The bpfProgramConfig name **MUST** match the program's section name.

This operator was built utilizing some great tooling provided by the [operator-sdk library](https://sdk.operatorframework.io/). A great first step in understanding some
of the functionality can be to just run `make help`.

### Deploy locally via KIND

After reviewing the possible make targets it's quick and easy to get bpfd deployed locally on your system via a [KIND cluster](https://kind.sigs.k8s.io/). with:

```bash
make run-on-kind
```

The container images used for `bpfd`,`bpfd-agent`, and `bpfd-operator` can also be
configured, by default local image builds will be used for the kind deployment.

```bash
BPFD_IMG=<your/image/url> BPFD_AGENT_IMG=<your/image/url> BPFD_OPERATOR_IMG=<your/image/url> make run-on-kind
```

Then to push and test out any local changes simply run:

```bash
make kind-reload-images
```

### Deploy To Openshift Cluster

First install cert-manager (if not already deployed) to the cluster with:

```bash
make deploy-cert-manager
```

Then deploy the operator with one of the following two options:

#### 1. Manually with Kustomize

Then to install manually with Kustomize and raw manifests simply run:

```bash
make deploy-openshift
```

Which can then be cleaned up with:

```bash
make undeploy-openshift
```

#### 2. Via the OLM bundle

The bpfd-operator can also be installed via it's [OLM bundle](https://www.redhat.com/en/blog/deploying-operators-olm-bundles).

First setup the namespace and certificates for the operator with:

```bash
oc apply -f ./hack/ocp-scc-hacks.yaml
```

Then use `operator-sdk` to install the bundle like so:

```bash
operator-sdk run bundle quay.io/bpfd/bpfd-operator-bundle:latest --namespace openshift-bpfd
```

To clean everything up run:

```bash
operator-sdk cleanup bpfd-operator
```

followed by

```bash
oc delete -f ./hack/ocp-scc-hacks.yaml
```

### Verify the installation

If the bpfd-operator came up successfully you will see the bpfd-daemon and bpfd-operator pods running without errors:

```bash
kubectl get pods -n bpfd
NAME                             READY   STATUS    RESTARTS   AGE
bpfd-daemon-bt5xm                2/2     Running   0          130m
bpfd-daemon-ts7dr                2/2     Running   0          129m
bpfd-daemon-w24pr                2/2     Running   0          130m
bpfd-operator-78cf9c44c6-rv7f2   2/2     Running   0          132m
```

To test the deployment simply configure the `bpfprogramconfig` (i.e determine the node's main interface name, in kind it will be `eth0`) and deploy one of the sample `bpfprogramconfigs`:

```bash
kubectl apply -f ./config/samples/bpfd.io_v1alpha1_xdp_pass_bpfprogramconfig.yaml
```

If loading of the bpfprogram to the selected nodes was successful it will be reported
back to the user via the `bpfprogramconfig`'s status field:

```bash
kubectl get bpfprogramconfig xdp-pass-all-nodes -o yaml
apiVersion: bpfd.io/v1alpha1
  kind: BpfProgramConfig
  metadata:
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: |
        {"apiVersion":"bpfd.io/v1alpha1","kind":"BpfProgramConfig","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"BpfProgramConfig"},"name":"xdp-pass-all-nodes"},"spec":{"attachpoint":{"networkmultiattach":{"interface":"eth0","priority":0}},"bytecode":"image://quay.io/bpfd/bytecode:xdp_pass","name":"pass","nodeselector":{},"type":"XDP"}}
    creationTimestamp: "2023-01-03T22:07:15Z"
    finalizers:
    - bpfd.io.operator/finalizer
    generation: 1
    labels:
      app.kubernetes.io/name: BpfProgramConfig
    name: xdp-pass-all-nodes
    resourceVersion: "18891"
    uid: ac3b2518-a26b-49cd-a7d9-5230c9999f7c
  spec:
    attachpoint:
      networkmultiattach:
        direction: NONE
        interface: eth0
        priority: 0
    bytecode: image://quay.io/bpfd/bytecode:xdp_pass
    name: pass
    nodeselector: {}
    type: XDP
  status:
    conditions:
    - lastTransitionTime: "2023-01-03T22:07:15Z"
      message: bpfProgramReconciliation Succeeded on all nodes
      reason: ReconcileSuccess
      status: "True"
      type: ReconcileSuccess
```

### API Types Overview

#### BpfProgramConfig

The `BpfProgramConfig` crd is the bpfd K8s API object most relevant to users and can be used to understand clusterwide state for an ebpf program. It's designed to express how, and where bpf programs are to be deployed within a kubernetes cluster.  An example BpfProgramConfig which loads a basic `xdp-pass` program to all nodes can be seen below:

**NOTE: Currently the bpfprogram's bytecode section-name MUST match the `spec.name` field in the BpfProgramConfig Object.**

```yaml
apiVersion: bpfd.io/v1alpha1
kind: BpfProgramConfig
metadata:
  labels:
    app.kubernetes.io/name: BpfProgramConfig
  name: xdp-pass-all-nodes
spec:
  ## Must correspond to image section name
  name: pass
  type: XDP
  # Select all nodes
  nodeselector: {}
  priority: 0
  attachpoint: 
    interface: eth0
  bytecode:
    imageurl: quay.io/bpfd/bytecode:xdp_pass
```

### BpfProgram

The `BpfProgram` crd is used internally by the bpfd-deployment to keep track of per node bpfd state such as program UUIDs and map pin points, and to report node specific errors back to the user. K8s users/controllers are only allowed to view these objects, NOT create or edit them.  Below is an example ebpfProgram Object which was automatically generated in response to the above BpfProgramConfig Object.

```yaml
apiVersion: bpfd.io/v1alpha1
  kind: BpfProgram
  metadata:
    creationTimestamp: "2022-12-07T22:41:29Z"
    finalizers:
    - bpfd.io.agent/finalizer
    generation: 2
    labels:
      owningConfig: xdp-pass-all-nodes
    name: xdp-pass-all-nodes-bpfd-deployment-worker2
    ownerReferences:
    - apiVersion: bpfd.io/v1alpha1
      blockOwnerDeletion: true
      controller: true
      kind: BpfProgramConfig
      name: xdp-pass-all-nodes
      uid: 6e3f5851-97b1-4772-906b-3ac69c6a4057
    resourceVersion: "1506"
    uid: 384d3d5c-e62b-4be3-9bf0-c6cf0e315acf
  spec:
    programs:
      bdeac6d3-4128-464e-9161-6010684eca27:
        attachpoint:
          interface: eth0
        maps: {}
  status:
    conditions:
    - lastTransitionTime: "2022-12-07T22:41:30Z"
      message: Successfully loaded ebpfProgram
      reason: bpfdLoaded
      status: "True"
      type: Loaded
```

Applications wishing to use bpfd to deploy/manage their bpf programs in kubernetes will make use of this
object to find references to the bpfMap pin points (`spec.maps`) in order to configure their bpf programs.

### Controllers

The Bpfd-Operator performs a few major functions and houses two major controllers the `bpfd-agent` and `bpfd-operator`.

#### bpfd-agent

The bpfd-agent controller is deployed alongside bpfd in a daemonset.  It's main purpose is to watch user intent (in BpfProgramConfig Objects) and communicate with
bpfd via a mTLS secured connection in order to translate the cluster-wide user-intent to per node state.

#### bpfd-operator

The bpfd-operator performs the following functionality:

- Create and Reconcile the bpfd daemonset (including both the `bpfd` and `bpfd-agent` processes) so that no manual edits can be completed.
- Report cluster wide state back the the user with each BpfProgramConfig's status field.

## More useful commands

1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your bpfd-agent and bpd-operator container images to the location specified by `BPFD_AGENT_IMG` and `BPFD_OPERATOR_IMG`:

```sh
make build-images push-images BPFD_OPERATOR_IMG=<some-registry>/bpfd-operator:tag BPFD_AGENT_IMAGE=<some-registry>/bpfd-agent:tag
```

3. Deploy the operator and agent to a cluster with the image specified by `BPFD_AGENT_IMG` and `BPFD_OPERATOR_IMG`:

```sh
make deploy BPFD_OPERATOR_IMG=<some-registry>/bpfd-operator:tag BPFD_AGENT_IMAGE=<some-registry>/bpfd-agent:tag
```

### Uninstall CRDs

To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller

UnDeploy the controller to the cluster:

```sh
make undeploy
```

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

## Contributing
// TODO(astoycos): Add detailed information on how you would like others to contribute to this project

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.


## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
