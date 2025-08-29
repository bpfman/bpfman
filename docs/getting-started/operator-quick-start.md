# Deploying the bpfman-operator

The `bpfman-operator` repository exists in order to deploy and manage bpfman within a Kubernetes cluster.
This operator was built utilizing some great tooling provided by the
[operator-sdk library](https://sdk.operatorframework.io/).
A great first step in understanding some of the functionality can be to just run `make help`.

## Deploy bpfman Operator

The `bpfman-operator` is running as a Deployment with a ReplicaSet of one.
It runs on the control plane and is composed of a single container.
The operator is responsible for launching the `bpfman-daemon` DaemonSet and the `bpfman-metrics-proxy` DaemonSet, which run on every node.
The `bpfman-daemon` DaemonSet is composed of the containers `bpfman`, `bpfman-agent`, and `node-driver-registrar`.

Described below are two ways to deploy bpfman in a Kubernetes cluster:

* [Deploy Locally via KIND](#deploy-locally-via-kind): Easiest way to deploy bpfman in a Kubernetes cluster
  and great for testing.
* [Deploy To Openshift Cluster](#deploy-to-openshift-cluster): Special steps are needed to deploy on an
  Openshift cluster because SELinux is enabled.

### Deploy Locally via KIND

After reviewing the possible make targets it's quick and easy to get bpfman deployed locally on your system
via a [KIND cluster](https://kind.sigs.k8s.io/) with:

```bash
cd bpfman-operator
make run-on-kind
```

!!! Note
    By default, bpfman-operator deploys bpfman with CSI enabled.
    CSI requires Kubernetes v1.26 due to a PR
    ([kubernetes/kubernetes#112597](https://github.com/kubernetes/kubernetes/pull/112597))
    that addresses a gRPC Protocol Error that was seen in the CSI client code and it doesn't
    appear to have been backported.
    It is recommended to install kind v0.20.0 or later.

### Deploy To Openshift Cluster

The recommended way of deploying bpfman to an OpenShift cluster is via the
OpenShift Console and using Operator Hub.
This is described in
[OperatorHub via OpenShift Console](../developer-guide/develop-operator.md#operatorhub-via-openshift-console).
For other options, see
[Deploy To Existing Cluster](../developer-guide/develop-operator.md#deploy-to-existing-cluster).

## API Types Overview

Refer to  [api-spec.md](../developer-guide/api-spec.md) for a detailed description of all the bpfman Kubernetes API types.

### Cluster Scoped Versus Namespaced Scoped CRDs

For security reasons, cluster admins may want to limit certain applications to only loading eBPF programs
within a given namespace. However, not all eBPF programs make sense to be namespaced scoped.
To provide these controls for eBPF program loading, the bpfman operator includes both cluster-scoped CRDs (identified by the
`Cluster` prefix) and namespace-scoped CRDs.

## Deploy an eBPF Program to the cluster

There are sample yamls for a number of support program types in the
[bpfman-operator/config/samples](https://github.com/bpfman/bpfman-operator/tree/main/config/samples)
directory.

### Deploy Cluster Scoped Sample

Any of the cluster scoped samples can be applied as is.
To test the deployment simply deploy one of the sample `xdpPrograms`:

```bash
cd bpfman-operator/
kubectl apply -f config/samples/bpfman.io_v1alpha1_xdp_pass_xdpprogram.yaml
```

If loading of the XDP Program to the selected nodes was successful it will be reported
back to the user via the `ClusterBpfApplication`'s status.


To see information in listing form simply run:

```bash
$ kubectl get ClusterBpfApplication xdp-pass-all-nodes
NAME                 NODESELECTOR   STATUS    AGE
xdp-pass-all-nodes                  Success   92s
```

For view full information, run:

```bash
$ kubectl get ClusterBpfApplication xdp-pass-all-nodes -o yaml
apiVersion: bpfman.io/v1alpha1
kind: ClusterBpfApplication
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfman.io/v1alpha1","kind":"ClusterBpfApplication","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"clusterbpfapplication"},"name":"xdp-pass-all-nodes"},"spec":{"byteCode":{"image":{"url":"quay.io/bpfman-bytecode/xdp_pass:latest"}},"nodeSelector":{},"programs":[{"name":"pass","type":"XDP","xdp":{"links":[{"interfaceSelector":{"primaryNodeInterface":true},"priority":55}]}}]}}
  creationTimestamp: "2025-07-01T21:46:01Z"
  finalizers:
  - bpfman.io.operator/finalizer
  generation: 1
  labels:
    app.kubernetes.io/name: clusterbpfapplication
  name: xdp-pass-all-nodes
  resourceVersion: "69629"
  uid: 389cdde5-5916-4198-8ce8-2f7409d6c3f8
spec:
  byteCode:
    image:
      imagePullPolicy: IfNotPresent
      url: quay.io/bpfman-bytecode/xdp_pass:latest
  nodeSelector: {}
  programs:
  - name: pass
    type: XDP
    xdp:
      links:
      - interfaceSelector:
          primaryNodeInterface: true
        priority: 55
        proceedOn:
        - Pass
        - DispatcherReturn
status:
  conditions:
  - lastTransitionTime: "2025-07-01T21:46:10Z"
    message: BPF application configuration successfully applied on all nodes
    reason: Success
    status: "True"
    type: Success
```


To view each attachment point on each node, use the `ClusterBpfApplicationState` object:

```bash
$ kubectl get clusterbpfapplicationstates -l bpfman.io/ownedByProgram=xdp-pass-all-nodes -o yaml
apiVersion: bpfman.io/v1alpha1
kind: ClusterBpfApplicationState
metadata:
  creationTimestamp: "2025-07-01T21:46:01Z"
  finalizers:
  - bpfman.io.clbpfapplicationcontroller/finalizer
  generation: 1
  labels:
    bpfman.io/ownedByProgram: xdp-pass-all-nodes
    kubernetes.io/hostname: bpfman-deployment-control-plane
  name: xdp-pass-all-nodes-cb774e3c
  ownerReferences:
  - apiVersion: bpfman.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterBpfApplication
    name: xdp-pass-all-nodes
    uid: 389cdde5-5916-4198-8ce8-2f7409d6c3f8
  resourceVersion: "69628"
  uid: b1436771-8591-490b-acaf-f229d079c336
status:
  appLoadStatus: LoadSuccess
  conditions:
  - lastTransitionTime: "2025-07-01T21:46:10Z"
    message: The BPF application has been successfully loaded and attached
    reason: Success
    status: "True"
    type: Success
  node: bpfman-deployment-control-plane
  programs:
  - name: pass
    programId: 841
    programLinkStatus: Success
    type: XDP
    xdp:
      links:
      - interfaceName: eth0
        linkId: 1718587848
        linkStatus: Attached
        priority: 55
        proceedOn:
        - Pass
        - DispatcherReturn
        shouldAttach: true
        uuid: c36d23bd-8ac2-4579-9e89-f745fd361fdd
  updateCount: 2
```

### Deploy Namespace Scoped Sample

The namespace scoped samples need a namespace and pods to attach to.
A yaml has been created that will create a `Namespace` called "acme" (see
[bpfman-operator/hack/namespace_scoped.yaml](https://github.com/bpfman/bpfman-operator/blob/main/hack/namespace_scoped.yaml)).
The reason for namespace scoped CRDs is to limit an application or user to a namespace.
To this end, this yaml also creates a limited `ServiceAccount`, `Role`, `RoleBinding` and `Secret`.

```bash
$ cd bpfman-operator
$ kubectl apply -f hack/namespace_scoped.yaml 
  namespace/acme created
  serviceaccount/test-account created
  role.rbac.authorization.k8s.io/test-account created
  rolebinding.rbac.authorization.k8s.io/test-account created
  secret/test-account-token created
```

To create a `kubeconfig` file that limits access to the created namespace, use the script 
[bpfman-operator/hack/namespace_scoped.sh](https://github.com/bpfman/bpfman-operator/blob/main/hack/namespace_scoped.sh).
The script needs to know the name of the `Cluster`, `Namespace`, `Service Account` and `Secret`.
The script defaults these fields to what is currently in 
[bpfman-operator/hack/namespace_scoped.yaml](https://github.com/bpfman/bpfman-operator/blob/main/hack/namespace_scoped.yaml).
However, if a file is passed to the script, it will look for the `Secret` object and attempt to extract the values. 
This can be used if the names are changed or a different yaml file is used.
The output of the script is the contents of a `kubeconfig`.
This can be printed to the console or redirected to a file.

```bash
./hack/namespace_scoped.sh hack/namespace_scoped.yaml > /tmp/kubeconfig 
```

To use the `kubeconfig` file, select the session to limit access in and run:

```bash
export KUBECONFIG=/tmp/kubeconfig
```

From within this limited access session, a sample `nginx` deployment can be created in the same namespace.

```bash
kubectl create deployment nginx --image=nginx
```

Finally, load any of the namespaced samples from
[bpfman-operator/config/samples](https://github.com/bpfman/bpfman-operator/tree/main/config/samples).

```bash
kubectl apply -f config/samples/bpfman.io_v1alpha1_bpfapplication.yaml
```

The status for each namespaced program is reported via the BpfApplication status field and further
information can be seen in the BpfApplicationState CRDs.
As an example, the following commands display the information of the program loaded in the acme
namespace with the command above.

```bash
$ kubectl get bpfapplications -n acme
NAME                    NODESELECTOR   STATUS    AGE
bpfapplication-sample                  Success   2d6h

$ kubectl get bpfapplications bpfapplication-sample -n acme -o yaml
apiVersion: bpfman.io/v1alpha1
kind: BpfApplication
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfman.io/v1alpha1","kind":"BpfApplication","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"bpfapplication"},"name":"bpfapplication-sample","namespace":"acme"},"spec":{"byteCode":{"image":{"url":"quay.io/bpfman-bytecode/app-test:latest"}},"globalData":{"GLOBAL_u32":[13,12,11,10],"GLOBAL_u8":[1]},"nodeSelector":{},"programs":[{"name":"tc_pass_test","tc":{"links":[{"direction":"Ingress","interfaceSelector":{"interfaces":["eth0"]},"networkNamespaces":{"pods":{"matchLabels":{"app":"test-target"}}},"priority":55}]},"type":"TC"},{"name":"tcx_next_test","tcx":{"links":[{"direction":"Egress","interfaceSelector":{"interfaces":["eth0"]},"networkNamespaces":{"pods":{"matchLabels":{"app":"test-target"}}},"priority":100}]},"type":"TCX"},{"name":"uprobe_test","type":"UProbe","uprobe":{"links":[{"containers":{"pods":{"matchLabels":{"app":"test-target"}}},"function":"malloc","target":"libc"}]}},{"name":"uretprobe_test","type":"URetProbe","uretprobe":{"links":[{"containers":{"pods":{"matchLabels":{"app":"test-target"}}},"function":"malloc","target":"libc"}]}},{"name":"xdp_pass_test","type":"XDP","xdp":{"links":[{"interfaceSelector":{"interfaces":["eth0"]},"networkNamespaces":{"pods":{"matchLabels":{"app":"test-target"}}},"priority":100}]}}]}}
  creationTimestamp: "2025-07-02T08:58:02Z"
  finalizers:
  - bpfman.io.operator/finalizer
  generation: 2
  labels:
    app.kubernetes.io/name: bpfapplication
  name: bpfapplication-sample
  namespace: acme
  resourceVersion: "74053"
  uid: 0a2dd439-577f-49ae-81ae-54a9a2265ddb
spec:
  byteCode:
    image:
      imagePullPolicy: IfNotPresent
      url: quay.io/bpfman-bytecode/app-test:latest
  globalData:
    GLOBAL_u8: AQ==
    GLOBAL_u32: DQwLCg==
  nodeSelector: {}
  programs:
  - name: tc_pass_test
    tc:
      links:
      - direction: Ingress
        interfaceSelector:
          interfaces:
          - eth0
        networkNamespaces:
          pods:
            matchLabels:
              app: test-target
        priority: 55
        proceedOn:
        - Pipe
        - DispatcherReturn
    type: TC
  - name: tcx_next_test
    tcx:
      links:
      - direction: Egress
        interfaceSelector:
          interfaces:
          - eth0
        networkNamespaces:
          pods:
            matchLabels:
              app: test-target
        priority: 100
    type: TCX
  - name: uprobe_test
    type: UProbe
    uprobe:
      links:
      - containers:
          pods:
            matchLabels:
              app: test-target
        function: malloc
        offset: 0
        target: libc
  - name: uretprobe_test
    type: URetProbe
    uretprobe:
      links:
      - containers:
          pods:
            matchLabels:
              app: test-target
        function: malloc
        offset: 0
        target: libc
  - name: xdp_pass_test
    type: XDP
    xdp:
      links:
      - interfaceSelector:
          interfaces:
          - eth0
        networkNamespaces:
          pods:
            matchLabels:
              app: test-target
        priority: 100
        proceedOn:
        - Pass
        - DispatcherReturn
status:
  conditions:
  - lastTransitionTime: "2025-07-02T08:58:19Z"
    message: BPF application configuration successfully applied on all nodes
    reason: Success
    status: "True"
    type: Success
```

To view each attachment point on each node, use the `BpfApplicationState` object:

```bash
$ kubectl get bpfapplicationstate -n acme -l bpfman.io/ownedByProgram=bpfapplication-sample
NAME                             NODE                              STATUS    AGE
bpfapplication-sample-6eab3078   bpfman-deployment-control-plane   Success   2d7h

$ kubectl get bpfapplicationstate -n acme -l bpfman.io/ownedByProgram=bpfapplication-sample -o yaml
apiVersion: bpfman.io/v1alpha1
kind: BpfApplicationState
metadata:
  creationTimestamp: "2025-07-02T08:58:02Z"
  finalizers:
  - bpfman.io.nsbpfapplicationcontroller/finalizer
  generation: 1
  labels:
    bpfman.io/ownedByProgram: bpfapplication-sample
    kubernetes.io/hostname: bpfman-deployment-control-plane
  name: bpfapplication-sample-6eab3078
  namespace: acme
  ownerReferences:
  - apiVersion: bpfman.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: BpfApplication
    name: bpfapplication-sample
    uid: 0a2dd439-577f-49ae-81ae-54a9a2265ddb
  resourceVersion: "74052"
  uid: 71499d5d-e248-404f-b432-112b395754dd
status:
  appLoadStatus: LoadSuccess
  conditions:
  - lastTransitionTime: "2025-07-02T08:58:19Z"
    message: The BPF application has been successfully loaded and attached
    reason: Success
    status: "True"
    type: Success
  node: bpfman-deployment-control-plane
  programs:
  - name: tc_pass_test
    programId: 851
    programLinkStatus: Success
    tc: {}
    type: TC
  - name: tcx_next_test
    programId: 852
    programLinkStatus: Success
    tcx: {}
    type: TCX
  - name: uprobe_test
    programId: 853
    programLinkStatus: Success
    type: UProbe
    uprobe: {}
  - name: uretprobe_test
    programId: 854
    programLinkStatus: Success
    type: URetProbe
    uretprobe: {}
  - name: xdp_pass_test
    programId: 855
    programLinkStatus: Success
    type: XDP
    xdp: {}
  updateCount: 2
```
