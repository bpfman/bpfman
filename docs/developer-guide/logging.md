# Logging

This section describes how to enable logging in different `bpfd` deployments.

## Local Privileged Process

`bpfd` and `bpfctl` use the [env_logger](https://docs.rs/env_logger) crate to log messages to the terminal.
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
$ sudo RUST_LOG=info /usr/local/bin/bpfd
[2022-08-08T20:29:31Z INFO  bpfd] Log using env_logger
[2022-08-08T20:29:31Z INFO  bpfd::server] Loading static programs from /etc/bpfd/programs.d
[2022-08-08T20:29:31Z INFO  bpfd::server::bpf] Map veth12fa8e3 to 13
[2022-08-08T20:29:31Z INFO  bpfd::server] Listening on [::1]:50051
[2022-08-08T20:29:31Z INFO  bpfd::server::bpf] Program added: 1 programs attached to veth12fa8e3
[2022-08-08T20:29:31Z INFO  bpfd::server] Loaded static program pass with UUID d9fd88df-d039-4e64-9f63-19f3e08915ce
```

`bpfctl` has a minimal set of logs, but the infrastructure is in place if needed for future debugging.

```console
sudo RUST_LOG=info bpfctl list
[2023-05-09T12:46:59Z WARN  bpfctl] Unable to read config file, using defaults
[2023-05-09T12:46:59Z INFO  bpfctl] Using UNIX socket as transport
 Program ID  Name  Type  Load Time
```

## Systemd Service

If `bpfd` is running as a systemd service, then `bpfd` will log to journald.
As with env_logger, by default, `info` and higher messages are logged, but that can be
overwritten by setting the `RUST_LOG` environment variable.
`bpfctl` won't be run as a service, so it always uses env_logger.

Example:

```console
sudo vi /usr/lib/systemd/system/bpfd.service
[Unit]
Description=Run bpfd as a service
DefaultDependencies=no
After=network.target

[Service]
Environment="RUST_LOG=Info"    <==== Set Log Level Here
ExecStart=/usr/sbin/bpfd
MemoryAccounting=true
MemoryLow=infinity
MemoryMax=infinity
User=bpfd
Group=bpfd
AmbientCapabilities=CAP_BPF CAP_DAC_READ_SEARCH CAP_NET_ADMIN CAP_PERFMON CAP_SYS_ADMIN CAP_SYS_RESOURCE
CapabilityBoundingSet=CAP_BPF CAP_DAC_READ_SEARCH CAP_NET_ADMIN CAP_PERFMON CAP_SYS_ADMIN CAP_SYS_RESOURCE
```

Start the service:

```console
sudo systemctl start bpfd.service
```

Check the logs:

```console
$ sudo journalctl -f -u bpfd
Aug 08 16:25:04 ebpf03 systemd[1]: Started bpfd.service - Run bpfd as a service.
Aug 08 16:25:04 ebpf03 bpfd[180118]: Log using journald
Aug 08 16:25:04 ebpf03 bpfd[180118]: Loading static programs from /etc/bpfd/programs.d
Aug 08 16:25:04 ebpf03 bpfd[180118]: Map veth12fa8e3 to 13
Aug 08 16:25:04 ebpf03 bpfd[180118]: Listening on [::1]:50051
Aug 08 16:25:04 ebpf03 bpfd[180118]: Program added: 1 programs attached to veth12fa8e3
Aug 08 16:25:04 ebpf03 bpfd[180118]: Loaded static program pass with UUID a3ffa14a-786d-48ad-b0cd-a4802f0f10b6
```

Stop the service:

```console
sudo systemctl stop bpfd.service
```

## Kubernetes Deployment

When `bpfd` is run in a Kubernetes deployment, there is the bpfd Daemonset that runs on every node
and the bpd Operator that runs on the control plane:

```console
kubectl get pods -A
NAMESPACE            NAME                                                    READY   STATUS    RESTARTS   AGE
bpfd                 bpfd-daemon-dgqzw                                       2/2     Running   0          3d22h
bpfd                 bpfd-daemon-gqsgd                                       2/2     Running   0          3d22h
bpfd                 bpfd-daemon-zx9xr                                       2/2     Running   0          3d22h
bpfd                 bpfd-operator-7fbf4888c4-z8w76                          2/2     Running   0          3d22h
:
```

### bpfd Daemonset

`bpfd` and `bpfd-agent` are running in the bpfd daemonset.

#### View Logs

To view the `bpfd` logs:

```console
kubectl logs -n bpfd bpfd-daemon-dgqzw -c bpfd
[2023-05-05T14:41:26Z INFO  bpfd] Log using env_logger
[2023-05-05T14:41:26Z INFO  bpfd] Has CAP_BPF: false
[2023-05-05T14:41:26Z INFO  bpfd] Has CAP_SYS_ADMIN: true
:
```

To view the `bpfd-agent` logs:

```console
kubectl logs -n bpfd bpfd-daemon-dgqzw -c bpfd-agent
2023-05-05T14:41:27Z	INFO	controller-runtime.metrics	Metrics server is starting to listen	{"addr": ":8080"}
2023-05-05T14:41:27Z	INFO	tls-internal	Reading...
	{"Default config path": "/etc/bpfd/bpfd.toml"}
2023-05-05T14:41:27Z	INFO	setup	Waiting for active connection to bpfd at %s	{"addr": "localhost:50051", "creds": {}}
:
```

#### Change Log Level

To change the log level, edit the `bpfd-config` ConfigMap.
The `bpfd-operator` will detect the change and restart the bpfd daemonset with the updated values.

```console
kubectl edit configmaps -n bpfd bpfd-config
apiVersion: v1
data:
  bpfd.agent.image: quay.io/bpfd/bpfd-agent:latest
  bpfd.image: quay.io/bpfd/bpfd:latest
  bpfd.log.level: debug                 <==== Set bpfd-agent Log Level Here
  bpfd.toml: |
    [tls] # REQUIRED
    ca_cert = "/etc/bpfd/certs/ca/ca.crt"
    cert = "/etc/bpfd/certs/bpfd/tls.crt"
    key = "/etc/bpfd/certs/bpfd/tls.key"
    client_cert = "/etc/bpfd/certs/bpfd-client/tls.crt"
    client_key = "/etc/bpfd/certs/bpfd-client/tls.key"
kind: ConfigMap
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"v1","data":{"bpfd.agent.image":"quay.io/bpfd/bpfd-agent:latest","bpfd.image":"quay.io/bpfd/bpfd:latest","bpfd.log.level":"debug","bpfd.na>
                                                                              Set bpfd Log Level Here =========================================^^^^^
  creationTimestamp: "2023-05-05T14:41:19Z"
  name: bpfd-config
  namespace: bpfd
  resourceVersion: "700803"
  uid: 0cc04af4-032c-4712-b824-748b321d319b
```

Valid values are:

* `error`
* `info`
* `debug`
* `trace`

`trace` can be very verbose.

### bpfd Operator

The bpfd Operator is running as a Deployment with a ReplicaSet of one.
It runs with the containers `bpfd-operator` and `kube-rbac-proxy`.

#### View Logs

To view the `bpfd-operator` logs:

```console
kubectl logs -n bpfd bpfd-operator-7fbf4888c4-z8w76 -c bpfd-operator
{"level":"info","ts":"2023-05-09T18:37:11Z","logger":"controller-runtime.metrics","msg":"Metrics server is starting to listen","addr":"127.0.0.1:8080"}
{"level":"info","ts":"2023-05-09T18:37:11Z","logger":"setup","msg":"starting manager"}
{"level":"info","ts":"2023-05-09T18:37:11Z","msg":"Starting server","kind":"health probe","addr":"[::]:8081"}
{"level":"info","ts":"2023-05-09T18:37:11Z","msg":"Starting server","path":"/metrics","kind":"metrics","addr":"127.0.0.1:8080"}
I0509 18:37:11.262885       1 leaderelection.go:248] attempting to acquire leader lease bpfd/8730d955.bpfd.dev...
I0509 18:37:11.268918       1 leaderelection.go:258] successfully acquired lease bpfd/8730d955.bpfd.dev
{"level":"info","ts":"2023-05-09T18:37:11Z","msg":"Starting EventSource","controller":"configmap","controllerGroup":"","controllerKind":"ConfigMap","source":"kind source: *v1.ConfigMap"}
:
```

To view the `kube-rbac-proxy` logs:

```console
kubectl logs -n bpfd bpfd-operator-7fbf4888c4-z8w76 -c kube-rbac-proxy
I0509 18:37:11.063386       1 main.go:186] Valid token audiences: 
I0509 18:37:11.063485       1 main.go:316] Generating self signed cert as no cert is provided
I0509 18:37:11.955256       1 main.go:366] Starting TCP socket on 0.0.0.0:8443
I0509 18:37:11.955849       1 main.go:373] Listening securely on 0.0.0.0:8443
```

#### Change Log Level

To change the log level, edit the `bpfd-operator` Deployment.
The change will get detected and the bpfd operator pod will get restarted with the updated log level.

```console
kubectl edit deployment -n bpfd bpfd-operator
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
        - --health-probe-bind-address=:8081
        - --metrics-bind-address=127.0.0.1:8080
        - --leader-elect
        command:
        - /bpfd-operator
        env:
        - name: GO_LOG
          value: info                   <==== Set Log Level Here
        image: quay.io/bpfd/bpfd-operator:latest
        imagePullPolicy: IfNotPresent
:
```

Valid values are:

* `error`
* `info`
* `debug`
* `trace`
