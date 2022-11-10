# How to Manually Deploy bpfd on Kubernetes

## Pre-requsites

- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/)
  - [kind](https://kind.sigs.k8s.io/docs/user/quick-start/) can be used for a local development cluster.
    This is optional and `bpfd` can be deployed on any kubernetes cluster.
  - If using a `kind` cluster, the following command will create the cluster:
    ```bash
    kind create cluster
    ```

- [cert-manager](https://cert-manager.io/)
  - [cert-manager](https://cert-manager.io/) is used for certificate management.
  - Full install instructions for cert-manager can be found [here](https://cert-manager.io/docs/installation/).
  - To use the default static install instructions simply run:
    ```bash
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.9.1/cert-manager.yaml
    ```
    This will create three pods in the `cert-manager` namespace:
    ```bash
    kubectl get pods -n cert-manager
    NAME                                         READY   STATUS    RESTARTS   AGE
    cert-manager-5dd59d9d9b-bbqbx                1/1     Running   0          22h
    cert-manager-cainjector-8696fc9f89-rxtm7     1/1     Running   0          22h
    cert-manager-webhook-7d4b5b8c56-gt6xb        1/1     Running   0          22h
    ```

## Bpfd Component install

1. Install `EbpfProgram` CRD.
   This `EbpfProgram` CRD can then be used to add, delete and list eBPF programs.
   See [Load a sample program](#load-a-sample-program) below.

```bash
cargo run --bin crdgen | kubectl apply -f -
```

2. Install CA and Necessary certs.

```bash
kubectl apply -f ./packaging/kubernetes-deployment/bpfd-core/bpfd-cert-issuer.yaml
kubectl apply -f ./packaging/kubernetes-deployment/bpfd-core/bpfd-certs.yaml
```

3. Install `bpfd` configmap.
   This configmap contains the `bpfd.toml` file content (see [configuration.md](configuration.md)).
   When the bpfd daemonset is created in step 5, this configmap will be propagated to each node via a volume mount in the bpfd container.

```bash
kubectl apply -f ./packaging/kubernetes-deployment/bpfd-core/bpfd-config.yaml
```

4. Install bpfd `ServiceAccount`, `ClusterRole`, and `ClusterRoleBinding`.

```bash
kubectl apply -f ./packaging/kubernetes-deployment/bpfd-core/bpfd-agent-rbac.yaml
```

5. Install bpfd daemonset which contains the `bpfd` and `bpfd-agent` processes.
   This runs a `bpfd-xxxxx` pod on each node, where each pod contains a `bpfd` and `bpfd-agent` container.

```bash
kubectl apply -f ./packaging/kubernetes-deployment/bpfd-core/bpfd-ds.yaml
```


If everything worked correctly the `bpfd-xxxxx` daemonset pods will be up and running in the
`kube-system` namespace:

```bash
kubectl get pods -A
NAMESPACE            NAME                                         READY   STATUS    RESTARTS   AGE
cert-manager         cert-manager-5dd59d9d9b-bbqbx                1/1     Running   0          22h
cert-manager         cert-manager-cainjector-8696fc9f89-rxtm7     1/1     Running   0          22h
cert-manager         cert-manager-webhook-7d4b5b8c56-gt6xb        1/1     Running   0          22h
kube-system          bpfd-qd9h4                                   2/2     Running   0          3h19m
kube-system          coredns-6d4b75cb6d-87jnh                     1/1     Running   0          22h
kube-system          coredns-6d4b75cb6d-pvh5l                     1/1     Running   0          22h
kube-system          etcd-kind-control-plane                      1/1     Running   0          22h
kube-system          kindnet-2dwpx                                1/1     Running   0          22h
kube-system          kube-apiserver-kind-control-plane            1/1     Running   0          22h
kube-system          kube-controller-manager-kind-control-plane   1/1     Running   0          22h
kube-system          kube-proxy-8ld2v                             1/1     Running   0          22h
kube-system          kube-scheduler-kind-control-plane            1/1     Running   0          22h
local-path-storage   local-path-provisioner-9cd9bd544-s4nvk       1/1     Running   0          22h
```

## Load a sample program

A sample xdp pass program is provided at [packaging/kubernetes-deployment/sample-ebpfprograms/xdp-pass.yaml](../../packaging/kubernetes-deployment/sample-ebpfprograms/xdp-pass.yaml)
which resembles the following:

```bash
apiVersion: bpfd.io/v1alpha1
kind: EbpfProgram
metadata:
  name: ebpfprogram-sample
spec:
  type: xdp
  name: xdp-pass
  interface: eth0
  image: quay.io/astoycos/xdp_pass
  sectionname: pass
  priority: 50
```

To deploy the Ebpf program to all nodes in the cluster simply run:

```bash
kubectl apply -f ./packaging/kubernetes-deployment/sample-ebpfprograms/xdp-pass.yaml
```

If the program was loaded successfully the `bpfd-agent` will write the
attach_point (either interface or cgroup name/path) and the UUID of the
program on the `ebpfprogram-sample` object's annotations and also update
the program's `sync-status` to `Loaded`.

```bash
kubectl get ebpfprograms
NAME                 AGE
ebpfprogram-sample   99m

kubectl get ebpfprogram ebpfprogram-sample -o yaml
apiVersion: bpfd.io/v1alpha1
kind: EbpfProgram
metadata:
  annotations:
    bpfd.ebpfprogram.io/attach_point: eth0
    bpfd.ebpfprogram.io/uuid: d939a05d-d81f-46ee-9066-8c779893e254
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfd.io/v1alpha1","kind":"EbpfProgram","metadata":{"annotations":{},"name":"ebpfprogram-sample","namespace":"kube-system"},"spec":{"image":"quay.io/astoycos/xdp_pass","interface":"eth0","name":"xdp-pass","priority":50,"sectionname":"pass","type":"xdp"}}
  creationTimestamp: "2022-08-24T18:26:44Z"
  finalizers:
  - ebpfprogram.bpfd.io/finalizer
  generation: 1
  name: ebpfprogram-sample
  namespace: kube-system
  resourceVersion: "113427"
  uid: 895aaf52-b891-4bac-8252-7c60430dbf7f
spec:
  image: quay.io/astoycos/xdp_pass
  interface: eth0
  name: xdp-pass
  priority: 50
  sectionname: pass
  type: xdp
status:
  syncStatus: Loaded
```

To remove the program simply delete the `ebpfProgram` object:

```bash
kubectl delete -f bundle/manifests/ebpf-program-sample.yaml
```

Look at the bpfd logs to ensure the program was successfully deleted:

```bash
kubectl logs bpfd-qd9h4 -c bpfd 
[2022-08-24T14:40:39Z INFO  bpfd] Log using env_logger
[2022-08-24T14:40:39Z INFO  bpfd::server] Listening on [::1]:50051
[2022-08-24T15:31:53Z INFO  bpfd::server::bpf] Map eth0 to 356
[2022-08-24T15:31:53Z INFO  bpfd::server::bpf] Program added: 1 programs attached to eth0
[2022-08-24T15:33:58Z INFO  bpfd::server::bpf] Map eth0 to 356
[2022-08-24T15:33:58Z INFO  bpfd::server::bpf] Program removed: 0 programs attached to eth0
```
