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
* KprobeProgram CRD: [bpfd-operator/config/samples/bpfd.io_v1alpha1_kprobe_kprobeprogram.yaml](https://github.com/bpfd-dev/bpfd/blob/main/bpfd-operator/config/samples/bpfd.io_v1alpha1_kprobe_kprobeprogram.yaml)
* UprobeProgram CRD: [bpfd-operator/config/samples/bpfd.io_v1alpha1_uprobe_uprobeprogram.yaml](https://github.com/bpfd-dev/bpfd/blob/main/bpfd-operator/config/samples/bpfd.io_v1alpha1_uprobe_uprobeprogram.yaml)

Sample bytecode yaml with XdpProgram CRD:
```console
cat examples/config/base/go-xdp-counter/bytecode.yaml
apiVersion: bpfd.dev/v1alpha1
kind: XdpProgram
metadata:
  labels:
    app.kubernetes.io/name: xdpprogram
  name: go-xdp-counter-example
spec:
  name: xdp_stats
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
The `XdpProgram` is applied using `kubectl`, but in a more practical deployment, the `XdpProgram`
would be applied by the application or a controller.
2. `bpfd-agent`, running on each node, is watching for all changes to `XdpProgram` objects.
When it sees a `XdpProgram` object created or modified, it makes sure a `BpfProgram` object for that
node exists.
The name of the `BpfProgram` object is the `XdpProgram` object name with the node name and interface or
attach point appended.
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
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfd.dev/v1alpha1","kind":"XdpProgram","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"xdpprogram"},"name":"go-xdp-counter-example"},"spec":{"bpffunctionname":"xdp_stats","bytecode":{"image":{"url":"quay.io/bpfd-bytecode/go-xdp-counter:latest"}},"interfaceselector":{"primarynodeinterface":true},"nodeselector":{},"priority":55}}
  creationTimestamp: "2023-11-06T21:05:15Z"
  finalizers:
  - bpfd.dev.operator/finalizer
  generation: 2
  labels:
    app.kubernetes.io/name: xdpprogram
  name: go-xdp-counter-example
  resourceVersion: "3103"
  uid: edd45e2e-a40b-4668-ac76-c1f1eb63a23b
spec:
  bpffunctionname: xdp_stats
  bytecode:
    image:
      imagepullpolicy: IfNotPresent
      url: quay.io/bpfd-bytecode/go-xdp-counter:latest
  interfaceselector:
    primarynodeinterface: true
  mapownerselector: {}
  nodeselector: {}
  priority: 55
  proceedon:
  - pass
  - dispatcher_return
status:
  conditions:
  - lastTransitionTime: "2023-11-06T21:05:21Z"
    message: bpfProgramReconciliation Succeeded on all nodes
    reason: ReconcileSuccess
    status: "True"
    type: ReconcileSuccess
```

To retrieve information on the `BpfProgram` objects:

```console
kubectl get bpfprograms
NAME                                                                                  AGE
:
4822-bpfd-deployment-control-plane                                                    60m
4825-bpfd-deployment-control-plane                                                    60m
go-tc-counter-example-bpfd-deployment-control-plane-eth0                              61m
go-tracepoint-counter-example-bpfd-deployment-control-plane-syscalls-sys-enter-kill   61m
go-xdp-counter-example-bpfd-deployment-control-plane-eth0                             61m
go-xdp-counter-sharing-map-example-bpfd-deployment-control-plane-eth0                 60m
tc-dispatcher-4805-bpfd-deployment-control-plane                                      60m
xdp-dispatcher-4816-bpfd-deployment-control-plane                                     60m


kubectl get go-xdp-counter-example-bpfd-deployment-control-plane-eth0 -o yaml
apiVersion: bpfd.dev/v1alpha1
kind: BpfProgram
metadata:
  annotations:
    bpfd.dev.xdpprogramcontroller/interface: eth0
    bpfd.dev/ProgramId: "4801"
  creationTimestamp: "2023-11-06T21:05:15Z"
  finalizers:
  - bpfd.dev.xdpprogramcontroller/finalizer
  generation: 1
  labels:
    bpfd.dev/ownedByProgram: go-xdp-counter-example
    kubernetes.io/hostname: bpfd-deployment-control-plane
  name: go-xdp-counter-example-bpfd-deployment-control-plane-eth0
  ownerReferences:
  - apiVersion: bpfd.dev/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: XdpProgram
    name: go-xdp-counter-example
    uid: edd45e2e-a40b-4668-ac76-c1f1eb63a23b
  resourceVersion: "3102"
  uid: f7ffd156-168b-4dc8-be38-18c42626a631
spec:
  type: xdp
status:
  conditions:
  - lastTransitionTime: "2023-11-06T21:05:21Z"
    message: Successfully loaded bpfProgram
    reason: bpfdLoaded
    status: "True"
    type: Loaded
```

### Loading Userspace Container On Kubernetes

Here, a userspace container is deployed to consume the map data generated by the
eBPF counter program.
bpfd provides a [Container Storage Interface (CSI)](https://kubernetes-csi.github.io/docs/)
driver for exposing eBPF maps into a userspace container.
To avoid having to mount a host directory that contains the map pinned file into the container
and forcing the container to have permissions to access that host directory, the CSI driver
mounts the map at a specified location in the container.
All the examples use CSI, here is
[go-xdp-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-xdp-counter/deployment.yaml)
for reference:

```console
cd bpfd/examples/
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
          driver: csi.bpfd.dev                             <==== 1a) bpfd CSI Driver
          volumeAttributes:
            csi.bpfd.dev/program: go-xdp-counter-example   <==== 1b) eBPF Program owning the map
            csi.bpfd.dev/maps: xdp_stats_map               <==== 1c) Map to be exposed to the container
```

#### Loading A Userspace Container Image

The userspace programs have been pre-built and can be found here:

* `quay.io/bpfd-userspace/go-tc-counter:latest`
* `quay.io/bpfd-userspace/go-tracepoint-counter:latest`
* `quay.io/bpfd-userspace/go-xdp-counter:latest`

The example yaml files below are loading from these image.

* [go-tc-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-tc-counter/deployment.yaml)
* [go-tracepoint-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-tracepoint-counter/deployment.yaml)
* [go-xdp-counter/deployment.yaml](https://github.com/bpfd-dev/bpfd/tree/main/examples/config/base/go-xdp-counter/deployment.yaml)

The userspace program in a Kubernetes Deployment doesn't interacts directly with `bpfd` like it
did in the local host deployment.
Instead, the userspace program running on each node, if needed, reads the `BpfProgram` object
from the KubeApiServer to gather additional information about the loaded eBPF program.
To interact with the KubeApiServer, RBAC must be setup properly to access the `BpfProgram`
object.
The `bpfd-operator` defined the yaml for several ClusterRoles that can be used to access the
different `bpfd` related CRD objects with different access rights.
The example userspace containers will use the `bpfprogram-viewer-role`, which allows Read-Only
access to the `BpfProgram` object.
This ClusterRole is created automatically by the `bpfd-operator`.

The remaining objects (NameSpace, ServiceAccount, ClusterRoleBinding and examples DaemonSet)
can be created for each program type as follows:

```console
cd bpfd/
kubectl create -f examples/config/base/go-xdp-counter/deployment.yaml
kubectl create -f examples/config/base/go-tc-counter/deployment.yaml
kubectl create -f examples/config/base/go-tracepoint-counter/deployment.yaml
```

Following the diagram for the XDP example (Green numbers):

1. The userspace program queries the KubeApiServer for a specific `BpfProgram` object.
2. The userspace program verifies the `BpfProgram` has been loaded and uses the map to
periodically read the counter values.

To see if the userspace programs are working, view the logs:

```console
NAMESPACE               NAME                              READY   STATUS    RESTARTS   AGE
bpfd                    bpfd-daemon-jsgdh                 3/3     Running   0          11m
bpfd                    bpfd-operator-6c5c8887f7-qk28x    2/2     Running   0          12m
go-tc-counter           go-tc-counter-ds-9jv4g            1/1     Running   0          5m37s
go-tracepoint-counter   go-tracepoint-counter-ds-2gzbt    1/1     Running   0          5m35s
go-xdp-counter          go-xdp-counter-ds-2hs6g           1/1     Running   0          6m12s
:

kubectl logs -n go-xdp-counter go-xdp-counter-ds-2hs6g
2023/11/06 20:27:16 2429 packets received
2023/11/06 20:27:16 1328474 bytes received

2023/11/06 20:27:19 2429 packets received
2023/11/06 20:27:19 1328474 bytes received

2023/11/06 20:27:22 2430 packets received
2023/11/06 20:27:22 1328552 bytes received
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
  cd config/default/go-tc-counter && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tc-counter=quay.io/bpfd-userspace/go-tc-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-tc-counter | kubectl apply -f -
  namespace/go-tc-counter created
  serviceaccount/bpfd-app-go-tc-counter created
  clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-tc-counter created
  clusterrolebinding.rbac.authorization.k8s.io/privileged-scc-tc created
  daemonset.apps/go-tc-counter-ds created
  tcprogram.bpfd.dev/go-tc-counter-example created
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-tracepoint-counter:latest@' config/default/go-tracepoint-counter/patch.yaml.env > config/default/go-tracepoint-counter/patch.yaml
  cd config/default/go-tracepoint-counter && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tracepoint-counter=quay.io/bpfd-userspace/go-tracepoint-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-tracepoint-counter | kubectl apply -f -
  namespace/go-tracepoint-counter created
  serviceaccount/bpfd-app-go-tracepoint-counter created
  clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-tracepoint-counter created
  clusterrolebinding.rbac.authorization.k8s.io/privileged-scc-tracepoint created
  daemonset.apps/go-tracepoint-counter-ds created
  tracepointprogram.bpfd.dev/go-tracepoint-counter-example created
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter:latest@' config/default/go-xdp-counter/patch.yaml.env > config/default/go-xdp-counter/patch.yaml
  cd config/default/go-xdp-counter && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-xdp-counter | kubectl apply -f -
  namespace/go-xdp-counter unchanged
  serviceaccount/bpfd-app-go-xdp-counter unchanged
  clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-xdp-counter unchanged
  clusterrolebinding.rbac.authorization.k8s.io/privileged-scc-xdp unchanged
  daemonset.apps/go-xdp-counter-ds configured
  xdpprogram.bpfd.dev/go-xdp-counter-example unchanged
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter:latest@' config/default/go-xdp-counter-sharing-map/patch.yaml.env > config/default/go-xdp-counter-sharing-map/patch.yaml
  cd config/default/go-xdp-counter-sharing-map && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-xdp-counter-sharing-map | kubectl apply -f -
  xdpprogram.bpfd.dev/go-xdp-counter-sharing-map-example created

# Test Away ...

make undeploy
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-tc-counter:latest@' config/default/go-tc-counter/patch.yaml.env > config/default/go-tc-counter/patch.yaml
  cd config/default/go-tc-counter && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tc-counter=quay.io/bpfd-userspace/go-tc-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-tc-counter | kubectl delete --ignore-not-found=false -f -
  namespace "go-tc-counter" deleted
  serviceaccount "bpfd-app-go-tc-counter" deleted
  clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-tc-counter" deleted
  clusterrolebinding.rbac.authorization.k8s.io "privileged-scc-tc" deleted
  daemonset.apps "go-tc-counter-ds" deleted
  tcprogram.bpfd.dev "go-tc-counter-example" deleted
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-tracepoint-counter:latest@' config/default/go-tracepoint-counter/patch.yaml.env > config/default/go-tracepoint-counter/patch.yaml
  cd config/default/go-tracepoint-counter && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-tracepoint-counter=quay.io/bpfd-userspace/go-tracepoint-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-tracepoint-counter | kubectl delete --ignore-not-found=false -f -
  namespace "go-tracepoint-counter" deleted
  serviceaccount "bpfd-app-go-tracepoint-counter" deleted
  clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-tracepoint-counter" deleted
  clusterrolebinding.rbac.authorization.k8s.io "privileged-scc-tracepoint" deleted
  daemonset.apps "go-tracepoint-counter-ds" deleted
  tracepointprogram.bpfd.dev "go-tracepoint-counter-example" deleted
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter:latest@' config/default/go-xdp-counter/patch.yaml.env > config/default/go-xdp-counter/patch.yaml
  cd config/default/go-xdp-counter && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-xdp-counter | kubectl delete --ignore-not-found=false -f -
  namespace "go-xdp-counter" deleted
  serviceaccount "bpfd-app-go-xdp-counter" deleted
  clusterrolebinding.rbac.authorization.k8s.io "bpfd-app-rolebinding-go-xdp-counter" deleted
  clusterrolebinding.rbac.authorization.k8s.io "privileged-scc-xdp" deleted
  daemonset.apps "go-xdp-counter-ds" deleted
  xdpprogram.bpfd.dev "go-xdp-counter-example" deleted
  sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter:latest@' config/default/go-xdp-counter-sharing-map/patch.yaml.env > config/default/go-xdp-counter-sharing-map/patch.yaml
  cd config/default/go-xdp-counter-sharing-map && /home/bmcfall/src/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
  /home/bmcfall/src/bpfd/examples/bin/kustomize build config/default/go-xdp-counter-sharing-map | kubectl delete --ignore-not-found=false -f -
  xdpprogram.bpfd.dev "go-xdp-counter-sharing-map-example" deleted
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
  push-bc-images   Push all example userspace images
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
  deploy-xdp-ms    Deploy go-xdp-counter-sharing-map (shares map with go-xdp-counter) to the cluster specified in ~/.kube/config.
  undeploy-xdp-ms  Undeploy go-xdp-counter-sharing-map from the cluster specified in ~/.kube/config.
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
