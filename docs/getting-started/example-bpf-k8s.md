# Deploying Example eBPF Programs On Kubernetes

This section will describe loading bytecode on a Kubernetes cluster and launching the userspace
program.
The approach is slightly different when running on a Kubernetes cluster.
The eBPF bytecode should be loaded by an administrator, not the userspace program itself.

This section assumes there is already a Kubernetes cluster running and `bpfd` is running in the cluster.
See [Deploying the bpfd-operator](../developer-guide/operator-quick-start.md) for details on
deploying bpfd on a Kubernetes cluster, but the quickest solution is to run a Kubernetes KIND Cluster:

```console
cd bpfd/bpfd-operator/
make run-on-kind
```

### Loading eBPF Bytecode On Kubernetes

![go-xdp-counter On Kubernetes](../img/gocounter-on-k8s.png)

Instead of using the userspace program or `bpfctl` to load the eBPF bytecode as done in previous sections,
the bytecode will be loaded by creating a Kubernetes CRD object.
There is a CRD object for each eBPF program type bpfd supports.
Edit the sample yaml files to customize any configuration values:

* TcProgram CRD: [go-tc-counter/bytecode.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-tc-counter/bytecode.yaml)
* TracepointProgram CRD: [go-tracepoint-counter/bytecode.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-tracepoint-counter/bytecode.yaml)
* XdpProgram CRD: [go-xdp-counter/bytecode.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-xdp-counter/bytecode.yaml)

Sample bytecode yaml with XdpProgram CRD:
```console
    vi examples/config/base/go-xdp-counter/bytecode.yaml
    apiVersion: bpfd.dev/v1alpha1
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
    kubectl apply -f examples/config/base/go-xdp-counter/bytecode.yaml
     xdpprogram.bpfd.dev/go-xdp-counter-example created

    kubectl apply -f examples/config/base/go-tc-counter/bytecode.yaml
     tcprogram.bpfd.dev/go-tc-counter-example created

    kubectl apply -f examples/config/base/go-tracepoint-counter/bytecode.yaml
     tracepointprogram.bpfd.dev/go-tracepoint-counter-example created
```

Following the diagram for XDP example (Blue numbers):

1. The user creates a `XdpProgram` object with the parameters
associated with the eBPF bytecode, like interface, priority and BFP bytecode image.
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
object indicating if the eBPF program has been applied to all the desired nodes or not.

To retrieve information on the `XdpProgram` objects:

```console
    kubectl get xdpprograms
    NAME                     PRIORITY   DIRECTION
    go-xdp-counter-example   55


    kubectl get xdpprograms go-xdp-counter-example -o yaml
    apiVersion: bpfd.dev/v1alpha1
    kind: XdpProgram
    metadata:
      creationTimestamp: "2023-05-04T15:41:45Z"
      finalizers:
      - bpfd.dev.operator/finalizer
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
    apiVersion: bpfd.dev/v1alpha1
    kind: BpfProgram
    metadata:
      creationTimestamp: "2023-05-04T15:41:45Z"
      finalizers:
      - bpfd.dev.xdpprogramcontroller-finalizer
      generation: 2
      labels:
        ownedByProgram: go-xdp-counter-example
      name: go-xdp-counter-example-bpfd-deployment-worker
      ownerReferences:
      - apiVersion: bpfd.dev/v1alpha1
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

* [go-tc-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-tc-counter/deployment.yaml)
* [go-tracepoint-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-tracepoint-counter/deployment.yaml)
* [go-xdp-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-xdp-counter/deployment.yaml)

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
    kubectl create -f examples/config/base/go-xdp-counter/deployment.yaml
    kubectl create -f examples/config/base/go-tc-counter/deployment.yaml
    kubectl create -f examples/config/base/go-tracepoint-counter/deployment.yaml
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
    kubectl delete -f examples/config/base/go-xdp-counter/deployment.yaml
    kubectl delete -f examples/config/base/go-xdp-counter/bytecode.yaml

    kubectl delete -f examples/config/base/go-tc-counter/deployment.yaml
    kubectl delete -f examples/config/base/go-tc-counter/bytecode.yaml

    kubectl delete -f examples/config/base/go-tracepoint-counter/deployment.yaml
    kubectl delete -f examples/config/base/go-tracepoint-counter/bytecode.yaml
```

#### Automated Deployment

The steps above are automated in the `Makefile` in the examples directory.
Run `make deploy` to load each of the example bytecode and userspace yaml files, then
`make undeploy` to unload them.

```console
    cd bpfd/examples/
    make deploy
     sed 's@URL_BC@quay.io/bpfd-bytecode/go-tc-counter:latest@' config/default/go-tc-counter/patch.yaml.env > config/default/go-tc-counter/patch.yaml
     cd config/default/go-tc-counter && /home/$USER/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tc-counter=quay.io/bpfd-userspace/go-tc-counter:latest
     /home/$USER/src/bpfd/examples/bin/kustomize build config/default/go-tc-counter | kubectl apply -f -
     namespace/go-tc-counter created
     serviceaccount/bpfd-app-go-tc-counter created
     daemonset.apps/go-tc-counter-ds created
     tcprogram.bpfd.dev/go-tc-counter-example created
     sed 's@URL_BC@quay.io/bpfd-bytecode/go-tracepoint-counter:latest@' config/default/go-tracepoint-counter/patch.yaml.env > config/default/go-tracepoint-counter/patch.yaml
     cd config/default/go-tracepoint-counter && /home/$USER/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tracepoint-counter=quay.io/bpfd-userspace/go-tracepoint-counter:latest
     /home/$USER/src/bpfd/examples/bin/kustomize build config/default/go-tracepoint-counter | kubectl apply -f -
     namespace/go-tracepoint-counter created
     serviceaccount/bpfd-app-go-tracepoint-counter created
     clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-tracepoint-counter created
     daemonset.apps/go-tracepoint-counter-ds created
     tracepointprogram.bpfd.dev/go-tracepoint-counter-example created
     sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter:latest@' config/default/go-xdp-counter/patch.yaml.env > config/default/go-xdp-counter/patch.yaml
     cd config/default/go-xdp-counter && /home/$USER/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
     /home/$USER/src/bpfd/examples/bin/kustomize build config/default/go-xdp-counter | kubectl apply -f -
     namespace/go-xdp-counter created
     serviceaccount/bpfd-app-go-xdp-counter created
     clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-xdp-counter created
     clusterrolebinding.rbac.authorization.k8s.io/privileged-scc created
     daemonset.apps/go-xdp-counter-ds created
     xdpprogram.bpfd.dev/go-xdp-counter-example created

    # Test Away ...

    make undeploy
     sed 's@URL_BC@quay.io/bpfd-bytecode/go-tc-counter:latest@' config/default/go-tc-counter/patch.yaml.env > config/default/go-tc-counter/patch.yaml
     cd config/default/go-tc-counter && /home/$USER/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tc-counter=quay.io/bpfd-userspace/go-tc-counter:latest
     /home/$USER/src/bpfd/examples/bin/kustomize build config/default/go-tc-counter | kubectl delete --ignore-not-found=false -f -
     namespace "go-tc-counter" deleted
     serviceaccount "bpfd-app-go-tc-counter" deleted
     clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-tc-counter" deleted
     daemonset.apps "go-tc-counter-ds" deleted
     tcprogram.bpfd.dev "go-tc-counter-example" deleted
     sed 's@URL_BC@quay.io/bpfd-bytecode/go-tracepoint-counter:latest@' config/default/go-tracepoint-counter/patch.yaml.env > config/default/go-tracepoint-counter/patch.yaml
     cd config/default/go-tracepoint-counter && /home/$USER/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tracepoint-counter=quay.io/bpfd-userspace/go-tracepoint-counter:latest
     /home/$USER/src/bpfd/examples/bin/kustomize build config/default/go-tracepoint-counter | kubectl delete --ignore-not-found=false -f -
     namespace "go-tracepoint-counter" deleted
     serviceaccount "bpfd-app-go-tracepoint-counter" deleted
     clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-tracepoint-counter" deleted
     daemonset.apps "go-tracepoint-counter-ds" deleted
     tracepointprogram.bpfd.dev "go-tracepoint-counter-example" deleted
     sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter:latest@' config/default/go-xdp-counter/patch.yaml.env > config/default/go-xdp-counter/patch.yaml
     cd config/default/go-xdp-counter && /home/$USER/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
     /home/$USER/src/bpfd/examples/bin/kustomize build config/default/go-xdp-counter | kubectl delete --ignore-not-found=false -f -
     namespace "go-xdp-counter" deleted
     serviceaccount "bpfd-app-go-xdp-counter" deleted
     clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-xdp-counter" deleted
     clusterrolebinding.rbac.authorization.k8s.io "privileged-scc" deleted
     daemonset.apps "go-xdp-counter-ds" deleted
     xdpprogram.bpfd.dev "go-xdp-counter-example" deleted
```

Individual examples can be loaded and unloaded as well, for example `make deploy-xdp` and
`make undeploy-xdp`.
To see the full set of available commands, run `make help`:

```console
  make help

    Usage:
      make <target>
      make deploy TAG=v0.2.0
      make deploy-xdp IMAGE_XDP_US=quay.io/user1/go-xdp-counter-userspace:test

    General
      help             Display this help.

    Local Dependencies
      kustomize        Download kustomize locally if necessary.

    Development
      fmt              Run go fmt against code.
      verify           Verify all the autogenerated code
      lint             Run golang-ci linter

    Build
      build            Build all the userspace example code.
      generate         Run `go generate` to build the bytecode for each of the examples.
      build-us-images  Build all example userspace images
      push-us-images   Push all example userspace images
      load-us-images-kind  Build and load all example userspace images into kind

    Deployment Variables (not commands)
      TAG              Used to set all images to a fixed tag. Example: make deploy TAG=v0.2.0
      IMAGE_TC_BC      TC Bytecode image. Example: make deploy-tc IMAGE_TC_BC=quay.io/user1/go-tc-counter-bytecode:test
      IMAGE_TC_US      TC Userspace image. Example: make deploy-tc IMAGE_TC_US=quay.io/user1/go-tc-counter-userspace:test
      IMAGE_TP_BC      Tracepoint Bytecode image. Example: make deploy-tracepoint IMAGE_TP_BC=quay.io/user1/go-tracepoint-counter-bytecode:test
      IMAGE_TP_US      Tracepoint Userspace image. Example: make deploy-tracepoint IMAGE_TP_US=quay.io/user1/go-tracepoint-counter-userspace:test
      IMAGE_XDP_BC     XDP Bytecode image. Example: make deploy-xdp IMAGE_XDP_BC=quay.io/user1/go-xdp-counter-bytecode:test
      IMAGE_XDP_US     XDP Userspace image. Example: make deploy-xdp IMAGE_XDP_US=quay.io/user1/go-xdp-counter-userspace:test
      KIND_CLUSTER_NAME  Name of the deployed cluster to load example images to, defaults to `bpfd-deployment`
      ignore-not-found  For any undeploy command, set to true to ignore resource not found errors during deletion. Example: make undeploy ignore-not-found=true

    Deployment
      deploy-tc        Deploy go-tc-counter to the cluster specified in ~/.kube/config.
      undeploy-tc      Undeploy go-tc-counter from the cluster specified in ~/.kube/config.
      deploy-tracepoint  Deploy go-tracepoint-counter to the cluster specified in ~/.kube/config.
      undeploy-tracepoint  Undeploy go-tracepoint-counter from the cluster specified in ~/.kube/config.
      deploy-xdp       Deploy go-xdp-counter to the cluster specified in ~/.kube/config.
      undeploy-xdp     Undeploy go-xdp-counter from the cluster specified in ~/.kube/config.
      deploy           Deploy all examples to the cluster specified in ~/.kube/config.
      undeploy         Undeploy all examples to the cluster specified in ~/.kube/config.
```

#### Building A Userspace Container Image

To build the userspace examples in a container instead of using the pre-built ones,
from the bpfd code source directory (`quay.io/bpfd-userspace/`), run the following build commands:

```console
    cd bpfd/examples
    make IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest \
    IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest \
    IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest \
    build-us-images
```

Then **EITHER** push images to a remote repository:

```console
    docker login quay.io
    cd bpfd/examples
    make IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest \
    IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest \
    IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest \

    push-us-images
```

**OR** load the images directly to a specified kind cluster:

```console
    cd bpfd/examples
    make IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest \
    IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest \
    IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest \
    KIND_CLUSTER_NAME=bpfd-deployment \
    load-us-images-kind
```

Lastly, update the yaml to use the private images or override the yaml files using the Makefile:

```console
    cd bpfd/examples/
    make deploy-xdp IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest
    make undeploy-xdp

    make deploy-tc IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest
    make undeploy-tc

    make deploy-tracepoint IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest
    make undeploy-tracepoint
```
