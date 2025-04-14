# Logging

This section describes how to enable logging in different `bpfman` deployments.

## Logs From bpfman CLI Commands

`bpfman` uses the [env_logger](https://docs.rs/env_logger) crate to log messages to the terminal.
By default, only `error` messages are logged, but that can be overwritten by setting
the `RUST_LOG` environment variable.
Valid values:

* `error`
* `warn`
* `info`
* `debug`
* `trace`

Example:

```console
$ sudo RUST_LOG=info bpfman load image ...
[INFO  bpfman] Request to load 1 programs
[INFO  bpfman::oci_utils::cosign] Fetching Sigstore TUF data
[INFO  bpfman::oci_utils::cosign] fetching fulcio_certs
[INFO  bpfman::oci_utils::cosign] Creating ManualTrustRoot
[INFO  bpfman::oci_utils::cosign] Starting Cosign Verifier, downloading data from Sigstore TUF repository
:
```

## Logs From bpfman Running as Systemd Service

If `bpfman` is running as a systemd service, then `bpfman` will log to journald.
By default, `info` and higher messages are logged, but that can be
overwritten by setting the `RUST_LOG` environment variable.

Example:

```console
sudo vi /usr/lib/systemd/system/bpfman.service
[Unit]
Description=Run bpfman as a service
DefaultDependencies=no
After=network.target

[Service]
Environment="RUST_LOG=Info"    <==== Set Log Level Here
ExecStart=/usr/sbin/bpfman system service
AmbientCapabilities=CAP_BPF CAP_DAC_READ_SEARCH CAP_NET_ADMIN CAP_PERFMON CAP_SYS_ADMIN CAP_SYS_RESOURCE
CapabilityBoundingSet=CAP_BPF CAP_DAC_READ_SEARCH CAP_NET_ADMIN CAP_PERFMON CAP_SYS_ADMIN CAP_SYS_RESOURCE
```

Start the service:

```console
sudo systemctl daemon-reload
sudo systemctl enable --now bpfman.socket
```

Check the logs:

```console
$ sudo journalctl -u bpfman.service -u bpfman.socket -f
Feb 23 22:02:02 ebpf03 systemd[1]: bpfman.service: Deactivated successfully.
Feb 25 13:21:49 ebpf03 systemd[1]: bpfman.socket: Deactivated successfully.
Feb 25 13:21:49 ebpf03 systemd[1]: Closed bpfman.socket - bpfman API Socket.
Feb 25 13:21:56 ebpf03 systemd[1]: Listening on bpfman.socket - bpfman API Socket.
Feb 26 09:32:38 ebpf03 systemd[1]: bpfman.socket: Deactivated successfully.
Feb 26 09:32:38 ebpf03 systemd[1]: Closed bpfman.socket - bpfman API Socket.
:
```

Stop the service:

```console
sudo systemctl stop bpfman.socket
```

## Kubernetes Deployment

When `bpfman` is run in a Kubernetes deployment, there is the bpfman Daemonset that runs on every node
and the bpfman Operator that runs on the control plane:

```console
kubectl get pods -A
NAMESPACE            NAME                                                    READY   STATUS    RESTARTS   AGE
bpfman               bpfman-daemon-dgqzw                                     3/3     Running   0          3d22h
bpfman               bpfman-daemon-gqsgd                                     3/3     Running   0          3d22h
bpfman               bpfman-daemon-zx9xr                                     3/3     Running   0          3d22h
bpfman               bpfman-operator-7fbf4888c4-z8w76                        2/2     Running   0          3d22h
:
```

### bpfman Daemonset

`bpfman` and `bpfman-agent` are running in the bpfman daemonset.

#### View Logs

To view the `bpfman` logs:

```console
kubectl logs -n bpfman bpfman-daemon-dgqzw -c bpfman
[INFO  bpfman_rpc::serve] Using no inactivity timer
[INFO  bpfman_rpc::serve] Using default Unix socket
[INFO  bpfman_rpc::serve] Listening on /run/bpfman-sock/bpfman.sock
[INFO  bpfman_rpc::storage] CSI Plugin Listening on /run/bpfman/csi/csi.sock
:
```

To view the `bpfman-agent` logs:

```console
kubectl logs -n bpfman bpfman-daemon-dgqzw -c bpfman-agent
{"level":"info","ts":"2025-03-06T13:37:08Z","logger":"setup","msg":"Waiting for active connection to bpfman"}
{"level":"info","ts":"2025-03-06T13:37:08Z","logger":"setup","msg":"starting Bpfman-Agent"}
{"level":"info","ts":"2025-03-06T13:37:08Z","logger":"controller-runtime.metrics","msg":"Starting metrics server"}
{"level":"info","ts":"2025-03-06T13:37:08Z","msg":"starting server","name":"health probe","addr":"[::]:8175"}
{"level":"info","ts":"2025-03-06T13:37:08Z","msg":"starting server","name":"pprof","addr":"[::]:6060"}
:
```

#### Change Log Level

To change the log level of the agent or daemon, edit the `bpfman-config` ConfigMap.
The `bpfman-operator` will detect the change and restart the bpfman daemonset with the updated values.

```console
kubectl edit configmaps -n bpfman bpfman-config
apiVersion: v1
data:
  bpfman.agent.image: quay.io/bpfman/bpfman-agent:latest
  bpfman.image: quay.io/bpfman/bpfman:latest
  bpfman.log.level: info                     <==== Set bpfman Log Level Here
  bpfman.agent.log.level: info               <==== Set bpfman agent Log Level Here
kind: ConfigMap
metadata:
  creationTimestamp: "2023-05-05T14:41:19Z"
  name: bpfman-config
  namespace: bpfman
  resourceVersion: "700803"
  uid: 0cc04af4-032c-4712-b824-748b321d319b
```

Valid values for the **daemon** (`bpfman.log.level`) are:

* `error`
* `warn`
* `info`
* `debug`
* `trace`

`trace` can be very verbose. More information can be found regarding Rust's
env_logger [here](https://docs.rs/env_logger/latest/env_logger/).

Valid values for the **agent** (`bpfman.agent.log.level`) are:

* `info`
* `debug`
* `trace`

### bpfman Operator

The bpfman Operator is running as a Deployment with a ReplicaSet of one.
It runs with the containers `bpfman-operator` and `kube-rbac-proxy`.

#### View Logs

To view the `bpfman-operator` logs:

```console
kubectl logs -n bpfman bpfman-operator-7fbf4888c4-z8w76 -c bpfman-operator
{"level":"info","ts":"2025-03-06T13:36:57Z","logger":"setup","msg":"Discovering APIs"}
{"level":"info","ts":"2025-03-06T13:36:57Z","logger":"setup","msg":"detected platform version","PlatformVersion":"v1.32.2"}
{"level":"info","ts":"2025-03-06T13:36:57Z","logger":"setup","msg":"starting manager"}
{"level":"info","ts":"2025-03-06T13:36:57Z","logger":"controller-runtime.metrics","msg":"Starting metrics server"}
{"level":"info","ts":"2025-03-06T13:36:57Z","msg":"starting server","name":"health probe","addr":"[::]:8175"}
{"level":"info","ts":"2025-03-06T13:36:57Z","logger":"controller-runtime.metrics","msg":"Serving metrics server","bindAddress":"127.0.0.1:8174","secure":false}
:
```

To view the `kube-rbac-proxy` logs:

```console
kubectl logs -n bpfman bpfman-operator-7fbf4888c4-z8w76 -c kube-rbac-proxy
I0509 18:37:11.063386       1 main.go:186] Valid token audiences: 
I0509 18:37:11.063485       1 main.go:316] Generating self signed cert as no cert is provided
I0509 18:37:11.955256       1 main.go:366] Starting TCP socket on 0.0.0.0:8443
I0509 18:37:11.955849       1 main.go:373] Listening securely on 0.0.0.0:8443
```

#### Change Log Level

To change the log level, edit the `bpfman-operator` Deployment.
The change will get detected and the bpfman operator pod will get restarted with the updated log level.

```console
kubectl edit deployment -n bpfman bpfman-operator
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: "1"
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"apps/v1","kind":"Deployment","metadata":{"annotations":{},"labels":{"app.kubernetes.io/component":"manager","app.kubernetes.io/create>
  creationTimestamp: "2023-05-09T18:37:08Z"
  generation: 1
:
spec:
:
  template:
    metadata:
:
    spec:
      containers:
      - args:
:
      - args:
        - --health-probe-bind-address=:8175
        - --metrics-bind-address=127.0.0.1:8174
        - --leader-elect
        command:
        - /bpfman-operator
        env:
        - name: GO_LOG
          value: info                   <==== Set Log Level Here
        image: quay.io/bpfman/bpfman-operator:latest
        imagePullPolicy: IfNotPresent
:
```

Valid values are:

* `error`
* `info`
* `debug`
* `trace`
