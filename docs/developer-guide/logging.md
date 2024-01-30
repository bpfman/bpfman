# Logging

This section describes how to enable logging in different `bpfman` deployments.

## Local Privileged Bpfman Process

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
$ sudo RUST_LOG=info /usr/local/bin/bpfman
[2022-08-08T20:29:31Z INFO  bpfman] Log using env_logger
[2022-08-08T20:29:31Z INFO  bpfman::server] Loading static programs from /etc/bpfman/programs.d
[2022-08-08T20:29:31Z INFO  bpfman::server::bpf] Map veth12fa8e3 to 13
[2022-08-08T20:29:31Z INFO  bpfman::server] Listening on [::1]:50051
[2022-08-08T20:29:31Z INFO  bpfman::server::bpf] Program added: 1 programs attached to veth12fa8e3
[2022-08-08T20:29:31Z INFO  bpfman::server] Loaded static program pass with UUID d9fd88df-d039-4e64-9f63-19f3e08915ce
```

## Systemd Service

If `bpfman` is running as a systemd service, then `bpfman` will log to journald.
As with env_logger, by default, `info` and higher messages are logged, but that can be
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
sudo systemctl start bpfman.service
```

Check the logs:

```console
$ sudo journalctl -f -u bpfman
Aug 08 16:25:04 ebpf03 systemd[1]: Started bpfman.service - Run bpfman as a service.
Aug 08 16:25:04 ebpf03 bpfman[180118]: Log using journald
Aug 08 16:25:04 ebpf03 bpfman[180118]: Loading static programs from /etc/bpfman/programs.d
Aug 08 16:25:04 ebpf03 bpfman[180118]: Map veth12fa8e3 to 13
Aug 08 16:25:04 ebpf03 bpfman[180118]: Listening on [::1]:50051
Aug 08 16:25:04 ebpf03 bpfman[180118]: Program added: 1 programs attached to veth12fa8e3
Aug 08 16:25:04 ebpf03 bpfman[180118]: Loaded static program pass with UUID a3ffa14a-786d-48ad-b0cd-a4802f0f10b6
```

Stop the service:

```console
sudo systemctl stop bpfman.service
```

## Kubernetes Deployment

When `bpfman` is run in a Kubernetes deployment, there is the bpfman Daemonset that runs on every node
and the bpd Operator that runs on the control plane:

```console
kubectl get pods -A
NAMESPACE            NAME                                                    READY   STATUS    RESTARTS   AGE
bpfman                 bpfman-daemon-dgqzw                                       2/2     Running   0          3d22h
bpfman                 bpfman-daemon-gqsgd                                       2/2     Running   0          3d22h
bpfman                 bpfman-daemon-zx9xr                                       2/2     Running   0          3d22h
bpfman                 bpfman-operator-7fbf4888c4-z8w76                          2/2     Running   0          3d22h
:
```

### bpfman Daemonset

`bpfman` and `bpfman-agent` are running in the bpfman daemonset.

#### View Logs

To view the `bpfman` logs:

```console
kubectl logs -n bpfman bpfman-daemon-dgqzw -c bpfman
[2023-05-05T14:41:26Z INFO  bpfman] Log using env_logger
[2023-05-05T14:41:26Z INFO  bpfman] Has CAP_BPF: false
[2023-05-05T14:41:26Z INFO  bpfman] Has CAP_SYS_ADMIN: true
:
```

To view the `bpfman-agent` logs:

```console
kubectl logs -n bpfman bpfman-daemon-dgqzw -c bpfman-agent
{"level":"info","ts":"2023-12-20T20:15:34Z","logger":"controller-runtime.metrics","msg":"Metrics server is starting to listen","addr":":8174"}
{"level":"info","ts":"2023-12-20T20:15:34Z","logger":"setup","msg":"Waiting for active connection to bpfman"}
{"level":"info","ts":"2023-12-20T20:15:34Z","logger":"setup","msg":"starting Bpfman-Agent"}
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
{"level":"info","ts":"2023-05-09T18:37:11Z","logger":"controller-runtime.metrics","msg":"Metrics server is starting to listen","addr":"127.0.0.1:8174"}
{"level":"info","ts":"2023-05-09T18:37:11Z","logger":"setup","msg":"starting manager"}
{"level":"info","ts":"2023-05-09T18:37:11Z","msg":"Starting server","kind":"health probe","addr":"[::]:8175"}
{"level":"info","ts":"2023-05-09T18:37:11Z","msg":"Starting server","path":"/metrics","kind":"metrics","addr":"127.0.0.1:8174"}
I0509 18:37:11.262885       1 leaderelection.go:248] attempting to acquire leader lease bpfman/8730d955.bpfman.io...
I0509 18:37:11.268918       1 leaderelection.go:258] successfully acquired lease bpfman/8730d955.bpfman.io
{"level":"info","ts":"2023-05-09T18:37:11Z","msg":"Starting EventSource","controller":"configmap","controllerGroup":"","controllerKind":"ConfigMap","source":"kind source: *v1.ConfigMap"}
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
