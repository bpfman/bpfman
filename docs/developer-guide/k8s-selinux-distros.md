# Running the Examples as Non-Root on SELinux Distributions

Developer instances of Kubernetes such as kind often set SELinux to permissive
mode, ensuring the security subsystem does not interfere with the local
cluster operations.  However, in production distributions such as
Openshift, EKS, GKE and AWS where security is paramount, SELinux and other
security subsystems are often enabled by default.  This among other things
presents unique challenges when determining how to deploy unprivileged applications
with bpfman.

In order to deploy the provided examples on SELinux distributions, users must
first install the [security-profiles-operator](https://github.com/kubernetes-sigs/security-profiles-operator).
This will allow bpfman to deploy custom SELinux policies which will allow container users
access to bpf maps (i.e `map_read` and `map_write` actions).

It can easily be installed via operatorhub.io from [here](https://operatorhub.io/operator/security-profiles-operator).

Once the security-profiles-operator and bpfman are installed simply deploy desired
examples:

```bash
cd examples/
make deploy-tc-selinux
make deploy-xdp-selinux
:
make undeploy-tc-selinux
make undeploy-xdp-selinux
```
