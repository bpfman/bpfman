# Deploying Example eBPF Programs On Kubernetes

This section will describe launching eBPF enabled applications on a Kubernetes cluster.
The approach is slightly different when running on a Kubernetes cluster.

This section assumes there is already a Kubernetes cluster running and `bpfman` is running in the cluster.
See [Deploying the bpfman-operator](./operator-quick-start.md) for details on
deploying bpfman on a Kubernetes cluster, but the quickest solution is to run a Kubernetes KIND Cluster:

```console
cd bpfman-operator/
make run-on-kind
```

## Loading eBPF Programs On Kubernetes

Instead of using the userspace program or CLI to load the eBPF bytecode as done in previous sections,
the bytecode will be loaded by creating a Kubernetes CRD object.
There is a CRD object for each eBPF program type bpfman supports.

* Kprobe program: [Kprobe Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-kprobe-counter/bytecode.yaml)
* Tc prograom: [TcProgram Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-tc-counter/bytecode.yaml)
* Tcx program: [TcxProgram Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-tcx-counter/bytecode.yaml)
* Tracepoint program: [Tracepoint Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-tracepoint-counter/bytecode.yaml)
* Uprobe program: [Uprobe Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-uprobe-counter/bytecode.yaml)
* URetProbe program: [URetProbe Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-uretprobe-counter/bytecode.yaml)
* Xdp program: [XdpProgram Examples yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-xdp-counter/bytecode.yaml)

Sample bytecode yaml with XdpProgram CRD:
```console
$ cat examples/config/base/go-xdp-counter/bytecode.yaml
---
apiVersion: bpfman.io/v1alpha1
kind: ClusterBpfApplication
metadata:
  labels:
    app.kubernetes.io/name: xdpprogram
  name: go-xdp-counter-example
spec:
  # Select all nodes
  nodeSelector: {}
  byteCode:
    image:
      url: quay.io/bpfman-bytecode/go-xdp-counter:latest
  programs:
    - name: xdp_stats
      type: XDP
      xdp:
        links:
          - interfaceSelector:
              primaryNodeInterface: true
            priority: 55
```

Note that all the sample yaml files are configured with the bytecode running on all nodes
(`nodeselector: {}`).
This can be configured to run on specific nodes, but the DaemonSet yaml for the userspace program, which
is described below, should have an equivalent change.

Assume the following command is run:

```console
$ kubectl apply -f examples/config/base/go-xdp-counter/bytecode.yaml
clusterbpfapplication.bpfman.io/go-xdp-counter-example created
```

The diagram below shows `go-xdp-counter` example, but the other examples operate in
a similar fashion.

![go-xdp-counter On Kubernetes](../img/gocounter-on-k8s.png)

Following the diagram for XDP example (Blue numbers):

1. The user creates a `ClusterBpfApplicatin` object of program type XDP and with the parameters
associated with the eBPF bytecode, like interface, priority and BFP bytecode image.
The name of the `ClusterBpfApplication` object in this example is `go-xdp-counter-example`.
The `ClusterBpfApplication` is applied using `kubectl`, but in a more practical deployment, the `ClusterBpfApplication`
would be applied by the application or a controller.
2. `bpfman-agent`, running on each node, is watching for all changes to `ClusterBpfApplication` objects.
When it sees a `ClusterBpfApplication` object created or modified, it makes sure a apply the corresponding
BPF program to that node.
3. `bpfman-agent` then determines if it should be running on the given node, loads or unloads as needed
by making gRPC calls via `bpfman-rpc`, which calls into the `bpfman` Library.
`bpfman` behaves the same as described in the `running locally` example.
4. `bpfman-agent` finally updates the status of the `ClusterBpfApplicationState` object.
5. `bpfman-operator` watches all `ClusterBpfApplicationState` objects, and updates the status of the corresponding
`ClusterBpfApplication` object indicating if the eBPF program has been applied to all the desired nodes or not.

To retrieve information about the `ClusterBpfApplication` object:

```console
$ kubectl get ClusterBpfApplication
NAME                     NODESELECTOR   STATUS    AGE
go-xdp-counter-example                  Success   5m47s

$ kubectl get ClusterBpfApplication -o yaml
apiVersion: v1
items:
- apiVersion: bpfman.io/v1alpha1
  kind: ClusterBpfApplication
  metadata:
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: |
        {"apiVersion":"bpfman.io/v1alpha1","kind":"ClusterBpfApplication","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"xdpprogram"},"name":"go-xdp-counter-example"},"spec":{"byteCode":{"image":{"url":"quay.io/bpfman-bytecode/go-xdp-counter:latest"}},"nodeSelector":{},"programs":[{"name":"xdp_stats","type":"XDP","xdp":{"links":[{"interfaceSelector":{"primaryNodeInterface":true},"priority":55}]}}]}}
    creationTimestamp: "2025-07-21T15:47:29Z"
    finalizers:
    - bpfman.io.operator/finalizer
    generation: 1
    labels:
      app.kubernetes.io/name: xdpprogram
    name: go-xdp-counter-example
    resourceVersion: "1004"
    uid: e84f069f-1584-41fa-b4ac-e260f10e5012
  spec:
    byteCode:
      image:
        imagePullPolicy: IfNotPresent
        url: quay.io/bpfman-bytecode/go-xdp-counter:latest
    nodeSelector: {}
    programs:
    - name: xdp_stats
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
    - lastTransitionTime: "2025-07-21T15:47:39Z"
      message: BPF application configuration successfully applied on all nodes
      reason: Success
      status: "True"
      type: Success
kind: List
metadata:
  resourceVersion: ""
```

To retrieve information about the `ClusterBpfApplicationState` objects:

```console
$ kubectl get ClusterBpfApplicationState -l bpfman.io/ownedByProgram=go-xdp-counter-example
NAME                              NODE                              STATUS    AGE
go-xdp-counter-example-34a4624c   bpfman-deployment-control-plane   Success   7m57s

$ kubectl get ClusterBpfApplicationState -l bpfman.io/ownedByProgram=go-xdp-counter-example -o yaml
apiVersion: v1
items:
- apiVersion: bpfman.io/v1alpha1
  kind: ClusterBpfApplicationState
  metadata:
    creationTimestamp: "2025-07-21T15:47:29Z"
    finalizers:
    - bpfman.io.clbpfapplicationcontroller/finalizer
    generation: 1
    labels:
      bpfman.io/ownedByProgram: go-xdp-counter-example
      kubernetes.io/hostname: bpfman-deployment-control-plane
    name: go-xdp-counter-example-34a4624c
    ownerReferences:
    - apiVersion: bpfman.io/v1alpha1
      blockOwnerDeletion: true
      controller: true
      kind: ClusterBpfApplication
      name: go-xdp-counter-example
      uid: e84f069f-1584-41fa-b4ac-e260f10e5012
    resourceVersion: "1003"
    uid: b8316810-f4a5-4760-8a72-bf943625eaed
  status:
    appLoadStatus: LoadSuccess
    conditions:
    - lastTransitionTime: "2025-07-21T15:47:39Z"
      message: The BPF application has been successfully loaded and attached
      reason: Success
      status: "True"
      type: Success
    node: bpfman-deployment-control-plane
    programs:
    - name: xdp_stats
      programId: 1845
      programLinkStatus: Success
      type: XDP
      xdp:
        links:
        - interfaceName: eth0
          linkId: 2825010721
          linkStatus: Attached
          priority: 55
          proceedOn:
          - Pass
          - DispatcherReturn
          shouldAttach: true
          uuid: 86961016-12a2-467d-92b1-5ef0b7c3d74e
    updateCount: 2
kind: List
metadata:
  resourceVersion: ""
```

## Deploying an eBPF enabled application On Kubernetes

Here, a userspace container is deployed to consume the map data generated by the
eBPF counter program.
bpfman provides a [Container Storage Interface (CSI)](https://kubernetes-csi.github.io/docs/)
driver for exposing eBPF maps into a userspace container.
To avoid having to mount a host directory that contains the map pinned file into the container
and forcing the container to have permissions to access that host directory, the CSI driver
mounts the map at a specified location in the container.
All the examples use CSI, here is
[go-xdp-counter/deployment.yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-xdp-counter/deployment.yaml)
for reference:

```console
cd bpfman/examples/
cat config/base/go-xdp-counter/deployment.yaml
:
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: go-xdp-counter-ds
  namespace: go-xdp-counter
  labels:
    k8s-app: go-xdp-counter
spec:
  :
  template:
    :
    spec:
       :
      containers:
      - name: go-xdp-counter
        :
        volumeMounts:
        - name: go-xdp-counter-maps                        <==== 2) VolumeMount in container
          mountPath: /run/xdp/maps                         <==== 2a) Mount path in the container
          readOnly: true
      volumes:
      - name: go-xdp-counter-maps                          <==== 1) Volume describing the map
        csi:
          driver: csi.bpfman.io                             <==== 1a) bpfman CSI Driver
          volumeAttributes:
            csi.bpfman.io/program: go-xdp-counter-example   <==== 1b) eBPF Program owning the map
            csi.bpfman.io/maps: xdp_stats_map               <==== 1c) Map to be exposed to the container
```

### Loading A Userspace Container Image

The userspace programs have been pre-built and can be found here:

* [quay.io/bpfman-userspace/go-kprobe-counter:latest](https://quay.io/organization/bpfman-userspace)
* [quay.io/bpfman-userspace/go-tc-counter:latest](https://quay.io/organization/bpfman-userspace)
* [quay.io/bpfman-userspace/go-tracepoint-counter:latest](https://quay.io/organization/bpfman-userspace)
* [quay.io/bpfman-userspace/go-uprobe-counter:latest](https://quay.io/organization/bpfman-userspace)
* [quay.io/bpfman-userspace/go-xdp-counter:latest](https://quay.io/organization/bpfman-userspace)

The example YAML files below utilise the following images:

* [go-kprobe-counter/deployment.yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-kprobe-counter/deployment.yaml)
* [go-tc-counter/deployment.yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-tc-counter/deployment.yaml)
* [go-tracepoint-counter/deployment.yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-tracepoint-counter/deployment.yaml)
* [go-uprobe-counter/deployment.yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-uprobe-counter/deployment.yaml)
* [go-xdp-counter/deployment.yaml](https://github.com/bpfman/bpfman/tree/main/examples/config/base/go-xdp-counter/deployment.yaml)

The userspace program in a Kubernetes Deployment doesn't interact directly with `bpfman` like it
did in the local host deployment.
Instead, the userspace program running on each node, if needed, reads the `ClusterBpfApplicationState` object
from the KubeApiServer to gather additional information about the loaded eBPF program.
To interact with the KubeApiServer, RBAC must be setup properly to access the `ClusterBpfApplicationState`
object.
The `bpfman-operator` defined the yaml for several ClusterRoles that can be used to access the
different `bpfman` related CRD objects with different access rights.
The example userspace containers will use the `bpfman-bpfapplication-viewer-role` and
`bpfman-clusterbpfapplication-viewer-role` clusterroles, which allow Read-Only
access to the `BpfApplication` / `ClusterBpfApplication` objects.
This ClusterRole is created automatically by the `bpfman-operator`.

The remaining objects (NameSpace, ServiceAccount, ClusterRoleBinding and examples DaemonSet)
can be created for each program type as follows:

```console
$ cd bpfman/
$ kubectl create -f examples/config/base/go-xdp-counter/deployment.yaml
namespace/go-xdp-counter created
serviceaccount/bpfman-app-go-xdp-counter created
daemonset.apps/go-xdp-counter-ds created
```

This creates the `go-xdp-counter` userspace pod, but the other examples operate in
a similar fashion.

![go-xdp-counter On Kubernetes](../img/gocounter-on-k8s.png)

Following the diagram for the XDP example (Green numbers):

1. The userspace program queries the KubeApiServer for a specific `ClusterBpfApplication` or `BpfApplication` object,
if required.
2. The userspace program verifies the BPF program has been loaded and uses the map to periodically read the counter
values.

To see if the userspace programs are working, view the logs:

```console
$ kubectl get pods -n go-xdp-counter
NAME                      READY   STATUS    RESTARTS   AGE
go-xdp-counter-ds-hnnj7   1/1     Running   0          49m

$ kubectl logs -n go-xdp-counter --tail=6 go-xdp-counter-ds-hnnj7
2025/07/21 17:22:40 44015 packets received
2025/07/21 17:22:40 63352772 bytes received

2025/07/21 17:22:43 44031 packets received
2025/07/21 17:22:43 63357010 bytes received
```

To cleanup:

```console
kubectl delete -f examples/config/base/go-xdp-counter/deployment.yaml
kubectl delete -f examples/config/base/go-xdp-counter/bytecode.yaml
```

### Automated Deployment

The steps above are automated in the `Makefile` in the examples directory.
Run `make deploy` to load each of the example bytecode and userspace yaml files, then
`make undeploy` to unload them.

```console
$ cd bpfman/examples/
$ make deploy
  for target in deploy-tc deploy-tracepoint deploy-xdp deploy-xdp-ms deploy-kprobe deploy-target deploy-uprobe ; do \
	  make $target  || true; \
  done
  make[1]: Entering directory '/home/<$USER>/go/src/github.com/bpfman/bpfman/examples'
  sed 's@URL_BC@quay.io/bpfman-bytecode/go-tc-counter:latest@' config/default/go-tc-counter/patch.yaml.env > config/default/go-tc-counter/patch.yaml
  cd config/default/go-tc-counter && /home/<$USER>/go/src/github.com/bpfman/bpfman/examples/bin/kustomize edit set image quay.io/bpfman-userspace/go-tc-counter=quay.io/bpfman-userspace/go-tc-counter:latest
  namespace/go-tc-counter created
  serviceaccount/bpfman-app-go-tc-counter created
  daemonset.apps/go-tc-counter-ds created
  tcprogram.bpfman.io/go-tc-counter-example created
  :
  sed 's@URL_BC@quay.io/bpfman-bytecode/go-uprobe-counter:latest@' config/default/go-uprobe-counter/patch.yaml.env > config/default/go-uprobe-counter/patch.yaml
  cd config/default/go-uprobe-counter && /home/<$USER>/go/src/github.com/bpfman/bpfman/examples/bin/kustomize edit set image quay.io/bpfman-userspace/go-uprobe-counter=quay.io/bpfman-userspace/go-uprobe-counter:latest
  namespace/go-uprobe-counter created
  serviceaccount/bpfman-app-go-uprobe-counter created
  daemonset.apps/go-uprobe-counter-ds created
  uprobeprogram.bpfman.io/go-uprobe-counter-example created
  make[1]: Leaving directory '/home/<$USER>/go/src/github.com/bpfman/bpfman/examples'

# Test Away ...

$ kubectl get pods -A | grep -E '^go-'
go-app-counter          go-app-counter-ds-xt6h8                                   1/1     Running   0          3m8s
go-kprobe-counter       go-kprobe-counter-ds-2wrsz                                1/1     Running   0          3m10s
go-target               go-target-ds-72x86                                        1/1     Running   0          3m10s
go-tc-counter           go-tc-counter-ds-zv8f5                                    1/1     Running   0          3m13s
go-tcx-counter          go-tcx-counter-ds-cf6sz                                   1/1     Running   0          3m12s
go-tracepoint-counter   go-tracepoint-counter-ds-zbnkl                            1/1     Running   0          3m12s
go-uprobe-counter       go-uprobe-counter-ds-r5dm7                                1/1     Running   0          3m9s
go-uretprobe-counter    go-uretprobe-counter-ds-8bvw9                             1/1     Running   0          3m8s
go-xdp-counter          go-xdp-counter-ds-b9jk4                                   1/1     Running   0          3m11s

$ kubectl get clusterbpfapplications
NAME                            NODESELECTOR   STATUS    AGE
app-counter                                    Success   3m46s
go-kprobe-counter-example                      Success   3m48s
go-tc-counter-example                          Success   3m51s
go-tcx-counter-example                         Success   3m50s
go-tracepoint-counter-example                  Success   3m50s
go-uprobe-counter-example                      Success   3m47s
go-uretprobe-counter-example                   Success   3m46s
go-xdp-counter-example                         Success   3m49s

$ make undeploy
  for target in undeploy-tc undeploy-tracepoint undeploy-xdp undeploy-xdp-ms undeploy-kprobe undeploy-uprobe undeploy-target ; do \
	  make $target  || true; \
  done
  make[1]: Entering directory '/home/<$USER>/go/src/github.com/bpfman/bpfman/examples'
  sed 's@URL_BC@quay.io/bpfman-bytecode/go-tc-counter:latest@' config/default/go-tc-counter/patch.yaml.env > config/default/go-tc-counter/patch.yaml
  cd config/default/go-tc-counter && /home/<$USER>/go/src/github.com/bpfman/bpfman/examples/bin/kustomize edit set image quay.io/bpfman-userspace/go-tc-counter=quay.io/bpfman-userspace/go-tc-counter:latest
  namespace "go-tc-counter" deleted
  serviceaccount "bpfman-app-go-tc-counter" deleted
  daemonset.apps "go-tc-counter-ds" deleted
  tcprogram.bpfman.io "go-tc-counter-example" deleted
  :
  kubectl delete -f config/base/go-target/deployment.yaml
  namespace "go-target" deleted
  serviceaccount "bpfman-app-go-target" deleted
  daemonset.apps "go-target-ds" deleted
  make[1]: Leaving directory '/home/<$USER>/go/src/github.com/bpfman/bpfman/examples'
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

Build
  build            Build all the userspace example code.
  generate         Run `go generate` to build the bytecode for each of the examples.
  build-us-images  Build all example userspace images
  build-bc-images  Build bytecode example userspace images
  push-us-images   Push all example userspace images
  push-bc-images   Push all example bytecode images
  load-us-images-kind  Build and load all example userspace images into kind

Deployment Variables (not commands)
  TAG              Used to set all images to a fixed tag. Example: make deploy TAG=v0.2.0
  IMAGE_TC_BC      TC Bytecode image. Example: make deploy-tc IMAGE_TC_BC=quay.io/user1/go-tc-counter-bytecode:test
  IMAGE_TC_US      TC Userspace image. Example: make deploy-tc IMAGE_TC_US=quay.io/user1/go-tc-counter-userspace:test
  IMAGE_TP_BC      Tracepoint Bytecode image. Example: make deploy-tracepoint IMAGE_TP_BC=quay.io/user1/go-tracepoint-counter-bytecode:test
  IMAGE_TP_US      Tracepoint Userspace image. Example: make deploy-tracepoint IMAGE_TP_US=quay.io/user1/go-tracepoint-counter-userspace:test
  IMAGE_XDP_BC     XDP Bytecode image. Example: make deploy-xdp IMAGE_XDP_BC=quay.io/user1/go-xdp-counter-bytecode:test
  IMAGE_XDP_US     XDP Userspace image. Example: make deploy-xdp IMAGE_XDP_US=quay.io/user1/go-xdp-counter-userspace:test
  IMAGE_KP_BC      Kprobe Bytecode image. Example: make deploy-kprobe IMAGE_KP_BC=quay.io/user1/go-kprobe-counter-bytecode:test
  IMAGE_KP_US      Kprobe Userspace image. Example: make deploy-kprobe IMAGE_KP_US=quay.io/user1/go-kprobe-counter-userspace:test
  IMAGE_UP_BC      Uprobe Bytecode image. Example: make deploy-uprobe IMAGE_UP_BC=quay.io/user1/go-uprobe-counter-bytecode:test
  IMAGE_UP_US      Uprobe Userspace image. Example: make deploy-uprobe IMAGE_UP_US=quay.io/user1/go-uprobe-counter-userspace:test
  IMAGE_GT_US      Uprobe Userspace target. Example: make deploy-target IMAGE_GT_US=quay.io/user1/go-target-userspace:test
  KIND_CLUSTER_NAME  Name of the deployed cluster to load example images to, defaults to `bpfman-deployment`
  ignore-not-found  For any undeploy command, set to true to ignore resource not found errors during deletion. Example: make undeploy ignore-not-found=true

Deployment
  deploy-tc        Deploy go-tc-counter to the cluster specified in ~/.kube/config.
  undeploy-tc      Undeploy go-tc-counter from the cluster specified in ~/.kube/config.
  deploy-tracepoint  Deploy go-tracepoint-counter to the cluster specified in ~/.kube/config.
  undeploy-tracepoint  Undeploy go-tracepoint-counter from the cluster specified in ~/.kube/config.
  deploy-xdp       Deploy go-xdp-counter to the cluster specified in ~/.kube/config.
  undeploy-xdp     Undeploy go-xdp-counter from the cluster specified in ~/.kube/config.
  deploy-xdp-ms    Deploy go-xdp-counter-sharing-map (shares map with go-xdp-counter) to the cluster specified in ~/.kube/config.
  undeploy-xdp-ms  Undeploy go-xdp-counter-sharing-map from the cluster specified in ~/.kube/config.
  deploy-kprobe    Deploy go-kprobe-counter to the cluster specified in ~/.kube/config.
  undeploy-kprobe  Undeploy go-kprobe-counter from the cluster specified in ~/.kube/config.
  deploy-uprobe    Deploy go-uprobe-counter to the cluster specified in ~/.kube/config.
  undeploy-uprobe  Undeploy go-uprobe-counter from the cluster specified in ~/.kube/config.
  deploy-target    Deploy go-target to the cluster specified in ~/.kube/config.
  undeploy-target  Undeploy go-target from the cluster specified in ~/.kube/config.
  deploy           Deploy all examples to the cluster specified in ~/.kube/config.
  undeploy         Undeploy all examples to the cluster specified in ~/.kube/config.
```

### Building A Userspace Container Image

To build the userspace examples in a container instead of using the pre-built ones,
from the bpfman examples code source directory, run the following build command:

```console
cd bpfman/examples
make \
  IMAGE_KP_US=quay.io/$USER/go-kprobe-counter:latest \
  IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest \
  IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest \
  IMAGE_UP_US=quay.io/$USER/go-uprobe-counter:latest \
  IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest \
  build-us-images
```

Then **EITHER** push images to a remote repository:

```console
docker login quay.io
cd bpfman/examples
make \
  IMAGE_KP_US=quay.io/$USER/go-kprobe-counter:latest \
  IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest \
  IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest \
  IMAGE_UP_US=quay.io/$USER/go-uprobe-counter:latest \
  IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest \
  push-us-images
```

**OR** load the images directly to a specified kind cluster:

```console
cd bpfman/examples
make \
  IMAGE_KP_US=quay.io/$USER/go-kprobe-counter:latest \
  IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest \
  IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest \
  IMAGE_UP_US=quay.io/$USER/go-uprobe-counter:latest \
  IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest \
  KIND_CLUSTER_NAME=bpfman-deployment \
  load-us-images-kind
```

Lastly, update the yaml to use the private images or override the yaml files using the Makefile:

```console
cd bpfman/examples/

make deploy-kprobe IMAGE_XDP_US=quay.io/$USER/go-kprobe-counter:latest
make undeploy-kprobe

make deploy-tc IMAGE_TC_US=quay.io/$USER/go-tc-counter:latest
make undeploy-tc

make deploy-tracepoint IMAGE_TP_US=quay.io/$USER/go-tracepoint-counter:latest
make undeploy-tracepoint

make deploy-uprobe IMAGE_XDP_US=quay.io/$USER/go-uprobe-counter:latest
make undeploy-uprobe

make deploy-xdp IMAGE_XDP_US=quay.io/$USER/go-xdp-counter:latest
make undeploy-xdp
```
