---
date: 2024-02-26
authors:
  - anfredette
---

# Technical Challenges for Attaching eBPF Programs in Containers

We recently added support for attaching uprobes inside containers. The purpose
of this blog is to give a brief overview of the feature, to document the
technical challenges encountered, and describe our solutions for those
challenges. In particular, how to attach an eBPF program inside of a container,
and how to find the host Process ID (PID) on the node for the container?

The solutions seem relatively straightforward now that they are done, but we
found limited information elsewhere, so we thought it would be helpful to
document them here.

The uprobe implementation will be used as the example in this blog, but the
concepts can (and will eventually) be applied to other program types.

<!-- more -->

## Introduction

A "uprobe" (user probe) is a type of eBPF program that can be attached to a
specific location in a user-space application. This allows developers and system
administrators to dynamically instrument a user-space binary to inspect its
behavior, measure performance, or debug issues without modifying the
application's source code or binary. When the program execution reaches the
location to which the uprobe is attached, the eBPF program associated with the
uprobe is executed.

bpfman support for uprobes has existed for some time.  We recently extended this
support to allow users to attach uprobes inside of containers both in the
general case of a container running on a Linux server and also for containers
running in a Kubernetes cluster.

The following is a bpfman command line example for loading a uprobe inside a
container:

```bash
bpfman load image --image-url quay.io/bpfman-bytecode/uprobe:latest uprobe --fn-name "malloc" --target "libc" --container-pid 102745
```

The above command instructs bpfman to attach a uprobe to the `malloc` function
in the `libc` library for the container with PID 102745. The main addition here
is the ability to specify a `container-pid`, which is the PID of the container
as it is known to the host server.

The term "target" as used in the above bpfman command (and the CRD below)
describes the library or executable that we want to attach the uprobe to.  The
fn-name (the name of the function within that target) and/or an explicit
"offset" can be used to identify a specific offset from the beginning of the
target.  We also use the term "target" more generally to describe the intended
location of the uprobe.

For Kubernetes, the CRD has been extended to include a "container selector" to
describe one or more containers as shown in the following example.

```yaml
apiVersion: bpfman.io/v1alpha1
kind: UprobeProgram
metadata:
  labels:
    app.kubernetes.io/name: uprobeprogram
  name: uprobe-example-containers
spec:
  # Select all nodes
  nodeselector: {}
  bpffunctionname: my_uprobe
  func_name: malloc
  # offset: 0 # optional offset w/in function
  target: libc
  retprobe: false
  # pid: 0 # optional pid to execute uprobe for
  bytecode:
    image:
      url: quay.io/bpfman-bytecode/uprobe:latest
  containers:      <=== New section for specifying containers to attach uprobe to
    namespace: bpfman
    pods:
      matchLabels:
        name: bpfman-daemon
    containernames:
      - bpfman
      - bpfman-agent
```

In the Kubernetes case, the container selector (`containers`) is used to
identify one or more containers in which to attach the uprobe. If `containers`
identifies any containers on a given node, the bpfman agent on that node will
determine their host PIDs and make the calls to bpfman to attach the uprobes.

## Attaching uprobes in containers

A Linux "mount namespace" is a feature that isolates the mount points seen by a
group of processes. This means that processes in different mount namespaces can
have different views of the filesystem.  A container typically has its own mount
namespace that is isolated both from those of other containers and its parent.
Because of this, files that are visible in one container are likely not visible
to other containers or even to the parent host (at least not directly). To
attach a uprobe to a file in a container, we need to have access to that
container's mount namespace so we can see the file to which the uprobe needs to
be attached.

From a high level, attaching a uprobe to an executable or library in a container
is relatively straight forward. `bpfman` needs to change to the mount namespace
of the container, attach the uprobe to the target in that container, and then
return to our own mount namespace so that we can save the needed state and
continue processing other requests.

The main challenges are:

1. Changing to the mount namespace of the target container.
1. Returning to the bpfman mount namespace.
1. `setns` (at least for the mount namespace) can't be called from a
   multi-threaded application, and bpfman is currently multithreaded.
1. How to find the right PID for the target container.

### The Mount Namespace

To enter the container namespace, `bpfman` uses the
[sched::setns](https://docs.rs/nix/latest/nix/sched/fn.setns.html) function from
the Rust [nix](https://crates.io/crates/nix) crate. The `setns` function
requires the file descriptor for the mount namespace of the target container.

For a given container PID, the namespace file needed by the `setns` function can
be found in the `/proc/<PID>/ns/` directory. An example listing for the PID
102745 directory is shown below:

```bash
sudo ls -l /proc/102745/ns/
total 0
lrwxrwxrwx 1 root root 0 Feb 15 12:10 cgroup -> 'cgroup:[4026531835]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 ipc -> 'ipc:[4026532858]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 mnt -> 'mnt:[4026532856]'
lrwxrwxrwx 1 root root 0 Feb 15 12:07 net -> 'net:[4026532860]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 pid -> 'pid:[4026532859]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 pid_for_children -> 'pid:[4026532859]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 time -> 'time:[4026531834]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 time_for_children -> 'time:[4026531834]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 user -> 'user:[4026531837]'
lrwxrwxrwx 1 root root 0 Feb 15 12:10 uts -> 'uts:[4026532857]'
```

In this case, the mount namespace file is `/proc/102745/ns/mnt`. 

> ***NOTE:*** How to find the PID and the relationship between parent and child
> PIDs is described in the "Finding The PID" section [below](#finding-the-pid).

When running directly on a Linux server, `bpfman` has access to the host `/proc`
directory and can access the mount namespace file for any PID.  However, on
Kubernetes, `bpfman` runs in a container, so it doesn't have access to the
namespace files of other containers or the `/proc` directory of the host by
default. Therefore, in the Kubernetes implementation, `/proc` is mounted in the
`bpfman` container so it has access to the ns directories of other containers. 

### Returning to the `bpfman` Mount Namespace

After `bpfman` does a `setns` to the target container mount namespace, it
has access to the target binary in that container.  However, it only has access
to that container's view of the filesystem, and in most cases, this does not
include access to bpfman's filesystem or the host filesystem.  As a result,
bpfman loses the ability to access its own mount namespace file.

However, before calling setns, `bpfman` has access to it's own mount namespace
file.  Therefore, to avoid getting stranded in a different mount namespace,
`bpfman` also opens its own mount namespace file prior to calling `setns` so it
already has the file descriptor that will allow it to call `setns` to return to
its own mount namespace.

### Running `setns` From a Multi-threaded Process

Calling `setns` to a mount namespace doesn't work from a multi-threaded process.

To work around this issue, the logic was moved to a standalone single-threaded
executable called
[bpfman-ns](https://github.com/bpfman/bpfman/blob/main/bpfman-ns/src/main.rs)
that does the job of entering the namespace, attaching the uprobe, and then
returning to the bpfman namespace to save the needed info.

### Finding the PID

#### Finding a Host Container PID on a Linux Server

This section provides an overview of PID namespaces and shows several ways to
find the host PID for a container.

##### tl;dr

If you used Podman or Docker to run your container, **and** you gave the
container a unique name, the following commands can be used to find the host PID
of a container.

```bash
podman inspect -f '{{.State.Pid}}' <CONTAINER_NAME>
```

or, similarly,

```bash
docker inspect -f '{{.State.Pid}}'  <CONTAINER_NAME>
```

##### Overview of PID namespaces and Container Host PIDs 

Each container has a PID namespace. Each PID namespace (other than the root) is
contained within a parent PID namespace. In general, this relationship is
hierarchical and PID namespaces can be nested within other PID namespaces. In
this section, we will just cover the case of a root PID namepsace on a Linux
server that has containers with PID namespaces that are direct children of the
root. The multi-level case is described in the section on Nested Containers with
kind below.

The PID namespaces can be listed using the `lsns -t pid` command. Before we
start any containers, we just have the one root pid namespace as shown below.

```bash
sudo lsns -t pid
        NS TYPE NPROCS PID USER COMMAND
4026531836 pid     325   1 root /usr/lib/systemd/systemd rhgb --switched-root --system --deserialize 30
```

Now lets start a container with the following command in a new shell:

```bash
podman run -it --name=container_1 fedora:latest /bin/bash
```

> **NOTE:** In this section, we are using `podman` to run containers. However,
> all of the same commands can also be used with `docker`.

Now back on the host we have:

```bash
sudo lsns -t pid
        NS TYPE NPROCS    PID USER      COMMAND
4026531836 pid     337      1 root      /usr/lib/systemd/systemd rhgb --switched-root --system --deserialize 30
4026532948 pid       1 150342 user_abcd /bin/bash
```

We can see that the host PID for the container we just started is 150342.

Now let's start another container in a new shell with the same command (except
with a different name), and run the `lsns` command again on the host.

```bash
podman run -it --name=container_2 fedora:latest /bin/bash
```

On the host:

```bash
sudo lsns -t pid
        NS TYPE NPROCS    PID USER      COMMAND
4026531836 pid     339      1 root      /usr/lib/systemd/systemd rhgb --switched-root --system --deserialize 30
4026532948 pid       1 150342 user_abcd /bin/bash
4026533041 pid       1 150545 user_abcd /bin/bash
```

We now have 3 pid namespaces -- one for root and two for the containers. Since
we already know that the first container had PID 150342 we can conclude that the
second container has PID 150545. However, what would we do if we didn't already
know the PID for one of the containers?  

If the container we were interested in was running a unique command, we could
use that to disambiguate. However, in this case, both are running the same
`/bin/bash` command.

If something unique is running inside of the container, we can use the `ps -e -o
pidns,pid,args` command to get some info.

For example, run `sleep 1111` in `container_1`, then

```bash
sudo ps -e -o pidns,pid,args | grep 'sleep 1111'
4026532948  150778 sleep 1111
4026531836  151002 grep --color=auto sleep 1111
```

This tells us that the `sleep 1111` command is running in PID namespace
4026532948. And,

```bash
sudo lsns -t pid | grep 4026532948
4026532948 pid       2 150342 user_abcd /bin/bash
```

Tells us that the container's host PID is 150342.

Alternatively, we could run `lsns` inside of `container_1`.

```bash
dnf install -y util-linux
lsns -t pid
        NS TYPE NPROCS PID USER COMMAND
4026532948 pid       2   1 root /bin/bash
```

This tells us a few interesting things. 

1. Inside the container, the PID is 1,
1. We can't see any of the other PID namespaces inside the container.
1. The container PID namespace is 4026532948.

With the container PID namespace, we can run the `lsns -t pid | grep 4026532948`
command as we did above to find the container's host PID

Finally, the container runtime knows the pid mapping. As mentioned at the
beginning of this section, if the unique name of the container is known, the
following command can be used to get the host PID.

```bash
podman inspect -f '{{.State.Pid}}' container_1
150342
```

### How bpfman Agent Finds the PID on Kubernetes

When running on Kubernetes, the "containers" field in the UprobeProgram CRD can
be used to identify one or more containers using the following information:

- Namespace
- Pod Label
- Container Name

If the container selector matches any containers on a given node, the
`bpfman-agent` determines the host PID for those containers and then calls
`bpfman` to attach the uprobe in the container with the given PID.

From what we can tell, there is no way to find the host PID for a container
running in a Kubernetes pod from the Kubernetes interface. However, the
[container runtime](https://kubernetes.io/docs/concepts/architecture/cri/) does
know this mapping. 

The `bpfman-agent` implementation uses multiple steps to find the set of PIDs on
a given node (if any) for the containers that are identified by the container
selector.

1. It uses the Kubernetes interface to get a list of pods on the local node that
   match the container selector.
1. It uses use [crictl] with the names of the pods found to get the pod IDs
1. It uses `crictl` with the pod ID to find the containers in those pods and
   then checks whether any match the container selector.
1. Finally, it uses `crictl` with the pod IDs found to get the host PIDs for the
   containers.

[crictl]:https://kubernetes.io/docs/tasks/debug/debug-cluster/crictl/

As an example, the [bpfman.io_v1alpha1_uprobe_uprobeprogram_containers.yaml]
file can be used with the `kubectl apply -f` command to install uprobes on two
of the containers in the `bpfman-agent` pod. The bpfman code does this
programmatically, but we will step through the process of finding the host PIDs
for the two containers here using cli commands to demonstrate how it works.

We will use a [kind](https://kind.sigs.k8s.io/) deployment with bpfman for this
demo. See [Deploy Locally via KIND] for instructions on how to get this running.

[bpfman.io_v1alpha1_uprobe_uprobeprogram_containers.yaml]:https://github.com/bpfman/bpfman/blob/main/bpfman-operator/config/samples/bpfman.io_v1alpha1_uprobe_uprobeprogram_containers.yaml
[Deploy Locally via KIND]:../../getting-started/operator-quick-start.md#deploy-locally-via-kind

The container selector in the above yaml file is the following.

```yaml
  containers:
    namespace: bpfman
    pods:
      matchLabels:
        name: bpfman-daemon
    containernames:
      - bpfman
      - bpfman-agent
```

`bpfman` accesses the Kubernetes API and uses `crictl` from the `bpfman-agent`
container. However, the `bpfman-agent` container doesn't have a shell by
default, so we will run the examples from the `bpfman-deployment-control-plane`
node, which will yield the same results. `bpfman-deployment-control-plane` is a
docker container in our kind cluster, so enter the container.

```bash
docker exec -it c84cae77f800 /bin/bash
```
Install `crictl`.

```bash
apt update
apt install wget
VERSION="v1.28.0"
wget https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-$VERSION-linux-amd64.tar.gz
tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
rm -f crictl-$VERSION-linux-amd64.tar.gz
```

First use `kubectl` to get the list of pods that match our container selector.

```bash
kubectl get pods -n bpfman -l name=bpfman-daemon
NAME                  READY   STATUS    RESTARTS   AGE
bpfman-daemon-cv9fm   3/3     Running   0          6m54s
```

> ***NOTE:*** The bpfman code also filters on the local node, but we only have
> one node in this deployment, so we'll ignore that here.

Now, use `crictl` with the name of the pod found to get the pod ID.

```bash
crictl pods --name bpfman-daemon-cv9fm
POD ID              CREATED             STATE               NAME                  NAMESPACE           ATTEMPT             RUNTIME
e359900d3eca5       46 minutes ago      Ready               bpfman-daemon-cv9fm   bpfman              0                   (default)
```

Now, use the pod ID to get the list of containers in the pod.

```bash
crictl ps --pod e359900d3eca5
CONTAINER           IMAGE               CREATED             STATE               NAME                    ATTEMPT             POD ID              POD
5eb3b4e5b45f8       50013f94a28d1       48 minutes ago      Running             node-driver-registrar   0                   e359900d3eca5       bpfman-daemon-cv9fm
629172270a384       e507ecf33b1f8       48 minutes ago      Running             bpfman-agent            0                   e359900d3eca5       bpfman-daemon-cv9fm
6d2420b80ddf0       86a517196f329       48 minutes ago      Running             bpfman                  0                   e359900d3eca5       bpfman-daemon-cv9fm
```

Now use the container IDs for the containers identified in the container
selector to get the PIDs of the containers.

```bash
# Get PIDs for bpfman-agent container
crictl inspect 629172270a384 | grep pid
    "pid": 2158,
            "pid": 1
            "type": "pid"

# Get PIDs for bpfman container
crictl inspect 6d2420b80ddf0 | grep pid
    "pid": 2108,
            "pid": 1
            "type": "pid"
```

From the above output, we can tell that the host PID for the `bpfman-agent`
container is 2158, and the host PID for the `bpfman` container is 2108. So, now
`bpfman-agent` would have the information needed to call `bpfman` with a request
to install a uprobe in the containers.

### Nested Containers with kind

kind is a tool for running local Kubernetes clusters using Docker container
“nodes”. The kind cluster we used for the previous section had a single node.

```bash
$ kubectl get nodes
NAME                              STATUS   ROLES           AGE   VERSION
bpfman-deployment-control-plane   Ready    control-plane   24h   v1.27.3
```

We can see the container for that node on the base server from Docker as
follows.

```bash
docker ps
CONTAINER ID   IMAGE                  COMMAND                  CREATED        STATUS        PORTS                       NAMES
c84cae77f800   kindest/node:v1.27.3   "/usr/local/bin/entr…"   25 hours ago   Up 25 hours   127.0.0.1:36795->6443/tcp   bpfman-deployment-control-plane
```

Our cluster has a number of pods as shown below.

```bash
kubectl get pods -A
NAMESPACE            NAME                                                      READY   STATUS    RESTARTS   AGE
bpfman               bpfman-daemon-cv9fm                                       3/3     Running   0          24h
bpfman               bpfman-operator-7f67bc7c57-bpw9v                          2/2     Running   0          24h
kube-system          coredns-5d78c9869d-7tw9b                                  1/1     Running   0          24h
kube-system          coredns-5d78c9869d-wxwfn                                  1/1     Running   0          24h
kube-system          etcd-bpfman-deployment-control-plane                      1/1     Running   0          24h
kube-system          kindnet-lbzw4                                             1/1     Running   0          24h
kube-system          kube-apiserver-bpfman-deployment-control-plane            1/1     Running   0          24h
kube-system          kube-controller-manager-bpfman-deployment-control-plane   1/1     Running   0          24h
kube-system          kube-proxy-sz8v9                                          1/1     Running   0          24h
kube-system          kube-scheduler-bpfman-deployment-control-plane            1/1     Running   0          24h
local-path-storage   local-path-provisioner-6bc4bddd6b-22glj                   1/1     Running   0          24h
```

Using the `lsns` command in the node's docker container, we can see that it has
a number of PID namespaces (1 for each container that is running in the pods in
the cluster), and all of these containers are nested inside of the docker "node"
container shown above.

```bash
lsns -t pid
        NS TYPE NPROCS   PID USER  COMMAND
# Note: 12 rows have been deleted below to save space
4026532861 pid      17     1 root  /sbin/init
4026532963 pid       1   509 root  kube-scheduler --authentication-kubeconfig=/etc/kubernetes/scheduler.conf --authorization-kubeconfig=/etc/kubernetes/scheduler.conf --bind-addre
4026532965 pid       1   535 root  kube-controller-manager --allocate-node-cidrs=true --authentication-kubeconfig=/etc/kubernetes/controller-manager.conf --authorization-kubeconfi
4026532967 pid       1   606 root  kube-apiserver --advertise-address=172.18.0.2 --allow-privileged=true --authorization-mode=Node,RBAC --client-ca-file=/etc/kubernetes/pki/ca.crt
4026532969 pid       1   670 root  etcd --advertise-client-urls=https://172.18.0.2:2379 --cert-file=/etc/kubernetes/pki/etcd/server.crt --client-cert-auth=true --data-dir=/var/lib
4026532972 pid       1  1558 root  local-path-provisioner --debug start --helper-image docker.io/kindest/local-path-helper:v20230510-486859a6 --config /etc/config/config.json
4026533071 pid       1   957 root  /usr/local/bin/kube-proxy --config=/var/lib/kube-proxy/config.conf --hostname-override=bpfman-deployment-control-plane
4026533073 pid       1  1047 root  /bin/kindnetd
4026533229 pid       1  1382 root  /coredns -conf /etc/coredns/Corefile
4026533312 pid       1  1896 65532 /usr/local/bin/kube-rbac-proxy --secure-listen-address=0.0.0.0:8443 --upstream=http://127.0.0.1:8174/ --logtostderr=true --v=0
4026533314 pid       1  1943 65532 /bpfman-operator --health-probe-bind-address=:8175 --metrics-bind-address=127.0.0.1:8174 --leader-elect
4026533319 pid       1  2108 root  ./bpfman system service --timeout=0 --csi-support
4026533321 pid       1  2158 root  /bpfman-agent --health-probe-bind-address=:8175 --metrics-bind-address=127.0.0.1:8174
4026533323 pid       1  2243 root  /csi-node-driver-registrar --v=5 --csi-address=/csi/csi.sock --kubelet-registration-path=/var/lib/kubelet/plugins/csi-bpfman/csi.sock
```
We can see the bpfman containers we were looking at earlier in the output above.
Let's take a deeper look at the `bpfman-agent` container that has a PID of 2158
on the Kubernetes node container and a PID namespace of 4026533321. If we go
back to the base server, we can find the container's PID there.

```bash
sudo lsns -t pid | grep 4026533321
4026533321 pid       1 222225 root  /bpfman-agent --health-probe-bind-address=:8175 --metrics-bind-address=127.0.0.1:8174
```

This command tells us that the PID of our `bpfman-agent` is 222225 on the base
server. The information for this PID is contained in `/proc/222225`.  The
following command will show the PID mappings for that one container at each
level.

```bash
sudo grep NSpid /proc/222225/status
NSpid:	222225	2158	1
```

The output above tells us that the PIDs for the `bpfman-agent` container are
222225 on the base server, 2158 in the Docker "node" container, and 1 inside the
container itself.

## Moving Forward

As always, there is more work to do. The highest priority goals are to support
additional eBPF program types and to use the Container Runtime Interface
directly.

We chose uprobes first because we had a user with a specific need. However,
there are use cases for other eBPF program types.

We used `crictl` in this first implementation because it already exists,
supports multiple container runtimes, handles the corner cases, and is
maintained. This allowed us to focus on the bpfman implementation and get the
feature done more quickly. However, it would be better to access the container
runtime interface directly rather than using an external executable.
