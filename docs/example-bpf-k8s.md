# Deploying Example BPF Programs On Kubernetes

This section will describe loading bytecode on a Kubernetes cluster and launching the userspace
program.
The approach is slightly different when running on a Kubernetes cluster.
The BPF bytecode should be loaded by an administrator, not the userspace program itself.

This section assumes there is already a Kubernetes cluster running and `bpfd` is running in the cluster.
See [How to Manually Deploy bpfd on Kubernetes](./k8s-deployment.md) for details on deploying
bpfd on a Kubernetes cluster, but the quickest solution is to run a Kubernetes KIND Cluster:

```console
cd bpfd/bpfd-operator/
make run-on-kind
```

### Loading BPF Bytecode On Kubernetes

![go-xdp-counter On Kubernetes](./img/gocounter-on-k8s.png)

Instead of using the userspace program or `bpfctl` to load the BPF bytecode as done in previous sections,
the bytecode will be loaded by creating a Kubernetes CRD object.
There is a CRD object for each BPF program type bpfd supports.
Edit the sample yaml files to customize any configuration values:

* TcProgram CRD: [go-tc-counter-bytecode.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tc-counter/kubernetes-deployment/go-tc-counter-bytecode.yaml)
* TracepointProgram CRD: [go-tracepoint-counter-bytecode.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter-bytecode.yaml)
* XdpProgram CRD: [go-xdp-counter-bytecode.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter-bytecode.yaml)

Sample bytecode yaml with XdpProgram CRD:
```console
    vi examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter-bytecode.yaml
    apiVersion: bpfd.io/v1alpha1
    kind: XdpProgram
    metadata:
      labels:
        app.kubernetes.io/name: xdpprogram
      name: go-xdp-counter-example
    spec:
      sectionname: stats
      # Select all nodes
      nodeselector: {}
      interfaceselector:
        primarynodeinterface: true
      priority: 55
      bytecode:
        image:
          url: quay.io/bpfd-bytecode/go-xdp-counter:latest
```

Note that all the sample yaml files are configured with the bytecode running on all nodes
(`nodeselector: {}`).
This can be change to run on specific nodes, but the DaemonSet yaml for the userspace program, which
is described below, should have an equivalent change.
Make any changes to the `go-xdp-counter-bytecode.yaml`, then repeat for `go-tc-counter-bytecode.yaml`
and `go-tracepoint-counter-bytecode.yaml` and then apply the updated yamls:

```console
    kubectl apply -f examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter-bytecode.yaml
     xdpprogram.bpfd.io/go-xdp-counter-example created

    kubectl apply -f examples/go-tc-counter/kubernetes-deployment/go-tc-counter-bytecode.yaml
     tcprogram.bpfd.io/go-tc-counter-example created

    kubectl apply -f examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter-bytecode.yaml
     tracepointprogram.bpfd.io/go-tracepoint-counter-example created
```

Following the diagram for XDP example (Blue numbers):

1. The user creates a `XdpProgram` object with the parameters
associated with the BPF bytecode, like interface, priority and BFP bytecode image.
The name of the `XdpProgram` object in this example is `go-xdp-counter-example`.
2. `bpfd-agent`, running on each node, is watching for all changes to `XdpProgram` objects.
When it sees a `XdpProgram` object created or modified, it makes sure a `BpfProgram` object for that
node exists.
The name of the `BpfProgram` object is the `XdpProgram` object name with the node name appended.
3. `bpfd-agent` then determines if it should be running on the given node, loads or unloads as needed
by making gRPC calls the `bpfd`.
`bpfd` behaves the same as described in the running locally example.
4. `bpfd-agent` finally updates the status of the `BpfProgram` object.
5. `bpfd-operator` watches all `BpfProgram` objects, and updates the status of the `XdpProgram`
object indicating if the BPF program has been applied to all the desired nodes or not.

To retrieve information on the `XdpProgram` objects:

```console
    kubectl get xdpprograms
    NAME                     PRIORITY   DIRECTION
    go-xdp-counter-example   55


    kubectl get xdpprograms go-xdp-counter-example -o yaml
    apiVersion: bpfd.io/v1alpha1
    kind: XdpProgram
    metadata:
      creationTimestamp: "2023-05-04T15:41:45Z"
      finalizers:
      - bpfd.io.operator/finalizer
      generation: 1
      labels:
        app.kubernetes.io/name: xdpprogram
      name: go-xdp-counter-example
      resourceVersion: "1786"
      uid: 19a64cf8-3909-4a61-a5c0-5a3ddb95769c
    spec:
      bytecode:
        image:
          imagepullpolicy: IfNotPresent
          url: quay.io/bpfd-bytecode/go-xdp-counter:latest
      interfaceselector:
        primarynodeinterface: true
      nodeselector: {}
      priority: 55
      proceedon:
      - pass
      - dispatcher_return
      sectionname: stats
    status:
      conditions:
      - lastTransitionTime: "2023-05-04T15:41:45Z"
        message: Waiting for BpfProgramConfig Object to be reconciled to all nodes
        reason: ProgramsNotYetLoaded
        status: "True"
        type: NotYetLoaded
      - lastTransitionTime: "2023-05-04T15:41:45Z"
        message: bpfProgramReconciliation Succeeded on all nodes
        reason: ReconcileSuccess
        status: "True"
        type: ReconcileSuccess
```

To retrieve information on the `BpfProgram` objects:

```console
    kubectl get bpfprograms
    NAME                                                          AGE
    go-tc-counter-example-bpfd-deployment-control-plane           8m52s
    go-tc-counter-example-bpfd-deployment-worker                  8m53s
    go-tc-counter-example-bpfd-deployment-worker2                 8m53s
    go-tracepoint-counter-example-bpfd-deployment-control-plane   8m52s
    go-tracepoint-counter-example-bpfd-deployment-worker          8m53s
    go-tracepoint-counter-example-bpfd-deployment-worker2         8m53s
    go-xdp-counter-example-bpfd-deployment-control-plane          8m54s
    go-xdp-counter-example-bpfd-deployment-worker                 8m54s
    go-xdp-counter-example-bpfd-deployment-worker2                8m54s


    kubectl get bpfprograms go-xdp-counter-example-bpfd-deployment-worker -o yaml
    apiVersion: bpfd.io/v1alpha1
    kind: BpfProgram
    metadata:
      creationTimestamp: "2023-05-04T15:41:45Z"
      finalizers:
      - bpfd.io.xdpprogramcontroller-finalizer
      generation: 2
      labels:
        ownedByProgram: go-xdp-counter-example
      name: go-xdp-counter-example-bpfd-deployment-worker
      ownerReferences:
      - apiVersion: bpfd.io/v1alpha1
        blockOwnerDeletion: true
        controller: true
        kind: XdpProgram
        name: go-xdp-counter-example
        uid: 19a64cf8-3909-4a61-a5c0-5a3ddb95769c
      resourceVersion: "1869"
      uid: 93a0f736-4a7a-48c2-b6ff-bc715b3580d6
    spec:
      node: bpfd-deployment-worker
      programs:
        ff121084-1211-4fa4-bb16-ddd18e3c63d5:
          xdp_stats_map: /run/bpfd/fs/maps/ff121084-1211-4fa4-bb16-ddd18e3c63d5/xdp_stats_map
      type: xdp
    status:
      conditions:
      - lastTransitionTime: "2023-05-04T15:41:46Z"
        message: Successfully loaded bpfProgram
        reason: bpfdLoaded
        status: "True"
        type: Loaded
```

### Loading Userspace Container On Kubernetes

#### Loading A Userspace Container Image

The userspace programs have been pre-built and can be found here:

* `quay.io/bpfd-userspace/go-tc-counter:latest`
* `quay.io/bpfd-userspace/go-tracepoint-counter:latest`
* `quay.io/bpfd-userspace/go-xdp-counter:latest`

The example yaml files below are loading from these image.

* [go-tc-counter.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tc-counter/kubernetes-deployment/go-tc-counter.yaml)
* [go-tracepoint-counter.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter.yaml)
* [go-xdp-counter.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter.yaml)

The userspace program in a Kubernetes Deployment no longer interacts directly with `bpfd`.
Instead, the userspace program running on each node reads the `BpfProgram` to determine the
map location.
For example, the output above shows the maps as:

```console
    kubectl get bpfprograms go-xdp-counter-example-bpfd-deployment-worker -o yaml
    :
    spec:
      node: bpfd-deployment-worker
      programs:
        ff121084-1211-4fa4-bb16-ddd18e3c63d5:
          xdp_stats_map: /run/bpfd/fs/maps/ff121084-1211-4fa4-bb16-ddd18e3c63d5/xdp_stats_map
      type: xdp
    :
```

To interact with the KubeApiServer, RBAC must be setup properly to access the `BpfProgram`
object.
The `bpfd-operator` defined the yaml for several ClusterRoles that can be used to access the
different `bpfd` related CRD objects with different access rights.
The example userspace containers will use the `bpfprogram-viewer-role`, which allows Read-Only
access to the `BpfProgram` object.
This ClusterRole is created automatically by the `bpfd-operator`.

The remaining objects (NameSpace, ServiceAccount, ClusterRoleBinding and examples DaemonSet)
also need to be created.

```console
    cd bpfd/
    kubectl create -f examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter.yaml
    kubectl create -f examples/go-tc-counter/kubernetes-deployment/go-tc-counter.yaml
    kubectl create -f examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter.yaml
```

Following the diagram for the XDP example (Green numbers):

1. The userspace program queries the KubeApiServer for a specific `BpfProgram` object.
2. The userspace program pulls the file location of the shared map out of the `BpfProgram`
object and uses the file to periodically read the counter values.

To see if the userspace programs are working, view the logs:

```console
    kubectl get pods -A
    NAMESPACE               NAME                             READY   STATUS    RESTARTS   AGE
    :
    go-tc-counter           go-tc-counter-ds-2dfn8           1/1     Running   0          16m
    go-tc-counter           go-tc-counter-ds-mn82s           1/1     Running   0          16m
    go-tc-counter           go-tc-counter-ds-qbf9w           1/1     Running   0          16m
    go-tracepoint-counter   go-tracepoint-counter-ds-686g5   1/1     Running   0          16m
    go-tracepoint-counter   go-tracepoint-counter-ds-tzj2r   1/1     Running   0          16m
    go-tracepoint-counter   go-tracepoint-counter-ds-zfz6k   1/1     Running   0          16m
    go-xdp-counter          go-xdp-counter-ds-c626t          1/1     Running   0          16m
    go-xdp-counter          go-xdp-counter-ds-kskgh          1/1     Running   0          16m
    go-xdp-counter          go-xdp-counter-ds-xx6dp          1/1     Running   0          16m
    :

    kubectl logs -n go-xdp-counter go-xdp-counter-ds-5q4hz
    2023/01/08 08:47:55 908748 packets received
    2023/01/08 08:47:55 631463477 bytes received

    2023/01/08 08:47:58 908757 packets received
    2023/01/08 08:47:58 631466099 bytes received

    2023/01/08 08:48:01 908778 packets received
    2023/01/08 08:48:01 631472201 bytes received

    2023/01/08 08:48:04 908791 packets received
    2023/01/08 08:48:04 631480013 bytes received
    :
```

To cleanup:

```console
    kubectl delete -f examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter.yaml
    kubectl delete -f examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter-bytecode.yaml

    kubectl delete -f examples/go-tc-counter/kubernetes-deployment/go-tc-counter.yaml
    kubectl delete -f examples/go-tc-counter/kubernetes-deployment/go-tc-counter-bytecode.yaml

    kubectl delete -f examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter.yaml
    kubectl delete -f examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter-bytecode.yaml
```

There are two scripts that will automate the steps described above:

```console
    cd bpfd
    ./scripts/cr-k8s-examples.sh
    xdpprogram.bpfd.io/go-xdp-counter-example created
    namespace/go-xdp-counter created
    serviceaccount/bpfd-app-go-xdp-counter created
    clusterrolebinding.rbac.authorization.k8s.io/privileged-scc created
    clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-xdp-counter created
    daemonset.apps/go-xdp-counter-ds created
    tcprogram.bpfd.io/go-tc-counter-example created
    namespace/go-tc-counter created
    serviceaccount/bpfd-app-go-tc-counter created
    clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-tc-counter created
    daemonset.apps/go-tc-counter-ds created
    tracepointprogram.bpfd.io/go-tracepoint-counter-example created
    namespace/go-tracepoint-counter created
    serviceaccount/bpfd-app-go-tracepoint-counter created
    clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-tracepoint-counter created
    daemonset.apps/go-tracepoint-counter-ds created

    # Test Away ...

    ./scripts/del-k8s-examples.sh
    serviceaccount "bpfd-app-go-xdp-counter" deleted
    clusterrolebinding.rbac.authorization.k8s.io "privileged-scc" deleted
    clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-xdp-counter" deleted
    daemonset.apps "go-xdp-counter-ds" deleted
    xdpprogram.bpfd.io "go-xdp-counter-example" deleted
    namespace "go-tc-counter" deleted
    serviceaccount "bpfd-app-go-tc-counter" deleted
    clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-tc-counter" deleted
    daemonset.apps "go-tc-counter-ds" deleted
    tcprogram.bpfd.io "go-tc-counter-example" deleted
    namespace "go-tracepoint-counter" deleted
    serviceaccount "bpfd-app-go-tracepoint-counter" deleted
    clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-tracepoint-counter" deleted
    daemonset.apps "go-tracepoint-counter-ds" deleted
    tracepointprogram.bpfd.io "go-tracepoint-counter-example" deleted
```

#### Building A Userspace Container Image

To build the userspace examples in a container instead of using the pre-built ones,
from the bpfd code source directory, run the following build commands:

```console
    cd bpfd/
    docker build -f examples/go-xdp-counter/container-deployment/Containerfile.go-xdp-counter . -t quay.io/$USER/go-xdp-counter:latest
    docker build -f examples/go-tc-counter/container-deployment/Containerfile.go-tc-counter . -t quay.io/$USER/go-tc-counter:latest
    docker build -f examples/go-tracepoint-counter/container-deployment/Containerfile.go-tracepoint-counter . -t quay.io/$USER/go-tracepoint-counter:latest
```

Then push images to a remote repository:

```console
    docker login quay.io
    docker push quay.io/$USER/go-xdp-counter:latest
    docker push quay.io/$USER/go-tc-counter:latest
    docker push quay.io/$USER/go-tracepoint-counter:latest
```

Update the yaml to use the private images.
