# Deploying the bpfd-operator

The `bpfd-operator` repository exists in order to deploy and manage bpfd within a Kubernetes cluster.

## Getting Started

This operator was built utilizing some great tooling provided by the
[operator-sdk library](https://sdk.operatorframework.io/).
A great first step in understanding some of the functionality can be to just run `make help`.

### Deploy Locally via KIND

After reviewing the possible make targets it's quick and easy to get bpfd deployed locally on your system
via a [KIND cluster](https://kind.sigs.k8s.io/) with:

```bash
make run-on-kind
```

### Deploy To Openshift Cluster

First install cert-manager (if not already deployed) to the cluster with:

```bash
make deploy-cert-manager
```

Then deploy the operator with one of the following two options:

#### 1. Manually with Kustomize

To install manually with Kustomize and raw manifests simply run:

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

### Verify the Installation

If the bpfd-operator came up successfully you will see the bpfd-daemon and bpfd-operator pods running without errors:

```bash
kubectl get pods -n bpfd
NAME                             READY   STATUS    RESTARTS   AGE
bpfd-daemon-bt5xm                2/2     Running   0          130m
bpfd-daemon-ts7dr                2/2     Running   0          129m
bpfd-daemon-w24pr                2/2     Running   0          130m
bpfd-operator-78cf9c44c6-rv7f2   2/2     Running   0          132m
```

### Deploy an eBPF Program to the cluster

To test the deployment simply deploy one of the sample `xdpPrograms`:

```bash
kubectl apply -f config/samples/bpfd.io_v1alpha1_xdp_pass_xdpprogram.yaml
```

If loading of the XDP Program to the selected nodes was successful it will be reported
back to the user via the `xdpProgram`'s status field:

```bash
kubectl get xdpprogram xdp-pass-all-nodes -o yaml
apiVersion: bpfd.dev/v1alpha1
  kind: XdpProgram
  metadata:
    creationTimestamp: "2023-05-04T18:28:46Z"
    finalizers:
    - bpfd.dev.operator/finalizer
    generation: 1
    labels:
      app.kubernetes.io/name: xdpprogram
    name: xdp-pass-all-nodes
    resourceVersion: "11205"
    uid: 8246b56c-b78e-43fc-bb78-3b46b1490a0c
  spec:
    bytecode:
      image:
        imagepullpolicy: IfNotPresent
        url: quay.io/bpfd-bytecode/xdp_pass:latest
    interfaceselector:
      primarynodeinterface: true
    nodeselector: {}
    priority: 0
    proceedon:
    - pass
    - dispatcher_return
    sectionname: pass
  status:
    conditions:
    - lastTransitionTime: "2023-05-04T18:28:46Z"
      message: bpfProgramReconciliation Succeeded on all nodes
      reason: ReconcileSuccess
      status: "True"
      type: ReconcileSuccess
kind: List
metadata:
  resourceVersion: ""
```

To see more information in listing form simply run:

```bash
kubectl get xdpprogram -o wide
NAME                 SECTIONNAME   BYTECODE                                                                                     NODESELECTOR   PRIORITY   INTERFACESELECTOR               PROCEEDON
xdp-pass-all-nodes   pass          {"image":{"imagepullpolicy":"IfNotPresent","url":"quay.io/bpfd-bytecode/xdp_pass:latest"}}   {}             0          {"primarynodeinterface":true}   ["pass","dispatcher_return"]
```

### API Types Overview

See [api-spec.md](./api-spec.md) for a more detailed description of all the bpfd Kubernetes API types.

#### Multiple Program CRDs

The multiple `*Program` CRDs are the bpfd Kubernetes API objects most relevant to users and can be used to
understand clusterwide state for an eBPF program.
It's designed to express how, and where eBPF programs are to be deployed within a Kubernetes cluster.
Currently bpfd supports the use of `xdpProgram`, `tcProgram` and `tracepointProgram` objects.

### BpfProgram CRD

The `BpfProgram` CRD is used internally by the bpfd-deployment to keep track of per node bpfd state
such as map pin points, and to report node specific errors back to the user.
Kubernetes users/controllers are only allowed to view these objects, NOT create or edit them.

Applications wishing to use bpfd to deploy/manage their eBPF programs in Kubernetes will make use of this
object to find references to the bpfMap pin points (`spec.maps`) in order to configure their eBPF programs.
