# Deploying the bpfman-operator

The `bpfman-operator` repository exists in order to deploy and manage bpfman within a Kubernetes cluster.
This operator was built utilizing some great tooling provided by the
[operator-sdk library](https://sdk.operatorframework.io/).
A great first step in understanding some of the functionality can be to just run `make help`.

## Deploy bpfman Operation

The `bpfman-operator` is running as a Deployment with a ReplicaSet of one.
It runs on the control plane and is composed of the containers `bpfman-operator` and
`kube-rbac-proxy`.
The operator is responsible for launching the bpfman Daemonset, which runs on every node.
The bpfman Daemonset is composed of the containers `bpfman`, `bpfman-agent`, and `node-driver-registrar`.

### Deploy Locally via KIND

After reviewing the possible make targets it's quick and easy to get bpfman deployed locally on your system
via a [KIND cluster](https://kind.sigs.k8s.io/) with:

```bash
cd bpfman/bpfman-operator
make run-on-kind
```

>> **NOTE:** By default, bpfman-operator deploys bpfman with CSI enabled.
CSI requires Kubernetes v1.26 due to a PR
([kubernetes/kubernetes#112597](https://github.com/kubernetes/kubernetes/pull/112597))
that addresses a gRPC Protocol Error that was seen in the CSI client code and it doesn't appear to have
been backported.
It is recommended to install kind v0.20.0 or later.

### Deploy To Openshift Cluster

First deploy the operator with one of the following two options:

#### 1. Manually with Kustomize

To install manually with Kustomize and raw manifests simply run the following
commands.
The Openshift cluster needs to be up and running and specified in `~/.kube/config`
file.

```bash
cd bpfman/bpfman-operator
make deploy-openshift
```

Which can then be cleaned up at a later time with:

```bash
make undeploy-openshift
```

#### 2. Via the OLM bundle

The other option for installing the bpfman-operator is to install it using
[OLM bundle](https://www.redhat.com/en/blog/deploying-operators-olm-bundles).

First setup the namespace and certificates for the operator with:

```bash
cd bpfman/bpfman-operator
oc apply -f ./hack/ocp-scc-hacks.yaml
```

Then use `operator-sdk` to install the bundle like so:

```bash
operator-sdk run bundle quay.io/bpfman/bpfman-operator-bundle:latest --namespace openshift-bpfman
```

Which can then be cleaned up at a later time with:

```bash
operator-sdk cleanup bpfman-operator
```

followed by

```bash
oc delete -f ./hack/ocp-scc-hacks.yaml
```

## Verify the Installation

Independent of the method used to deploy, if the bpfman-operator came up successfully
you will see the bpfman-daemon and bpfman-operator pods running without errors:

```bash
kubectl get pods -n bpfman
NAME                             READY   STATUS    RESTARTS   AGE
bpfman-daemon-w24pr                3/3     Running   0          130m
bpfman-operator-78cf9c44c6-rv7f2   2/2     Running   0          132m
```

## Deploy an eBPF Program to the cluster

To test the deployment simply deploy one of the sample `xdpPrograms`:

```bash
cd bpfman/bpfman-operator/
kubectl apply -f config/samples/bpfman.io_v1alpha1_xdp_pass_xdpprogram.yaml
```

If loading of the XDP Program to the selected nodes was successful it will be reported
back to the user via the `xdpProgram`'s status field:

```bash
kubectl get xdpprogram xdp-pass-all-nodes -o yaml
apiVersion: bpfman.io/v1alpha1
kind: XdpProgram
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfman.io/v1alpha1","kind":"XdpProgram","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"xdpprogram"},"name":"xdp-pass-all-nodes"},"spec":{"bpffunctionname":"pass","bytecode":{"image":{"url":"quay.io/bpfman-bytecode/xdp_pass:latest"}},"globaldata":{"GLOBAL_u32":[13,12,11,10],"GLOBAL_u8":[1]},"interfaceselector":{"primarynodeinterface":true},"nodeselector":{},"priority":0}}
  creationTimestamp: "2023-11-07T19:16:39Z"
  finalizers:
  - bpfman.io.operator/finalizer
  generation: 2
  labels:
    app.kubernetes.io/name: xdpprogram
  name: xdp-pass-all-nodes
  resourceVersion: "157187"
  uid: 21c71a61-4e73-44eb-9b49-07af2866d25b
spec:
  bpffunctionname: pass
  bytecode:
    image:
      imagepullpolicy: IfNotPresent
      url: quay.io/bpfman-bytecode/xdp_pass:latest
  globaldata:
    GLOBAL_u8: AQ==
    GLOBAL_u32: DQwLCg==
  interfaceselector:
    primarynodeinterface: true
  mapownerselector: {}
  nodeselector: {}
  priority: 0
  proceedon:
  - pass
  - dispatcher_return
status:
  conditions:
  - lastTransitionTime: "2023-11-07T19:16:42Z"
    message: bpfProgramReconciliation Succeeded on all nodes
    reason: ReconcileSuccess
    status: "True"
    type: ReconcileSuccess
```

To see information in listing form simply run:

```bash
kubectl get xdpprogram -o wide
NAME                 BPFFUNCTIONNAME   NODESELECTOR   PRIORITY   INTERFACESELECTOR               PROCEEDON
xdp-pass-all-nodes   pass              {}             0          {"primarynodeinterface":true}   ["pass","dispatcher_return"]
```

## API Types Overview

See [api-spec.md](./api-spec.md) for a more detailed description of all the bpfman Kubernetes API types.

### Multiple Program CRDs

The multiple `*Program` CRDs are the bpfman Kubernetes API objects most relevant to users and can be used to
understand clusterwide state for an eBPF program.
It's designed to express how, and where eBPF programs are to be deployed within a Kubernetes cluster.
Currently bpfman supports:

* `fentryProgram`
* `fexitProgram`
* `kprobeProgram`
* `tcProgram`
* `tracepointProgram`
* `uprobeProgram`
* `xdpProgram`

## BpfProgram CRD

The `BpfProgram` CRD is used internally by the bpfman-deployment to keep track of per node bpfman state
such as map pin points, and to report node specific errors back to the user.
Kubernetes users/controllers are only allowed to view these objects, NOT create or edit them.

Applications wishing to use bpfman to deploy/manage their eBPF programs in Kubernetes will make use of this
object to find references to the bpfMap pin points (`spec.maps`) in order to configure their eBPF programs.
