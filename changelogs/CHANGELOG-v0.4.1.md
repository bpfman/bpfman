The v0.4.1 release is a patch release which includes some bug fixes found
in the original v0.4.0 release.  Additionally support has been added for deploying
the example applications as unprivileged containers on productions systems which
enable SELinux by default.

Users can now access released RPMs from the [bpfman COPR repo](https://copr.fedorainfracloud.org/coprs/g/ebpf-sig/bpfman) and nightly RPMs from the [bpfman-next COPR repo](https://copr.fedorainfracloud.org/coprs/g/ebpf-sig/bpfman-next/).

For even more details Please see the individual commit
messages shown below.

## What's Changed
* bpfman: dir bpfman-sock missing for bpfman-rpc by @Billy99 in https://github.com/bpfman/bpfman/pull/1082
* docs: Reduce release changes by @Billy99 in https://github.com/bpfman/bpfman/pull/1071
* build(deps): bump async-trait from 0.1.79 to 0.1.80 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1085
* build(deps): bump sigstore/cosign-installer from 3.4.0 to 3.5.0 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1086
* add csi fsgroup support by @astoycos in https://github.com/bpfman/bpfman/pull/1089
* bpfman: include license in crate workspace by @danielmellado in https://github.com/bpfman/bpfman/pull/1087
* rpm: Add bpfman-rpc and bpfman-ns to rpm package by @Billy99 in https://github.com/bpfman/bpfman/pull/1096
* Add target aware uretprobe example by @msherif1234 in https://github.com/bpfman/bpfman/pull/1064
* Examples/Docs: idiomatic Go usage and update installation instructions for apt-based OS by @thediveo in https://github.com/bpfman/bpfman/pull/1084
* build(deps): bump the production-dependencies group with 3 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1099
* image: Add missing images to push to quay by @Billy99 in https://github.com/bpfman/bpfman/pull/1098
* update packit config by @astoycos in https://github.com/bpfman/bpfman/pull/1083
* fix mounter image to fedora 39 by @astoycos in https://github.com/bpfman/bpfman/pull/1102
* test: add negative test cases to integration tests by @Billy99 in https://github.com/bpfman/bpfman/pull/1100
* Start providing manifests for running our eBPF example applications as truly non-root by @astoycos in https://github.com/bpfman/bpfman/pull/1097
* build(deps): bump golangci/golangci-lint-action from 4 to 5 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1105
* build(deps): bump the production-dependencies group with 2 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1106
* De-duplicate code in the bpfman-agent by @anfredette in https://github.com/bpfman/bpfman/pull/1068
* test: add image name test by @Billy99 in https://github.com/bpfman/bpfman/pull/1107
* Add bpfman 0.4.1-rc1 to builds and cargo metadata by @danielmellado in https://github.com/bpfman/bpfman/pull/1112
* Free up disk space for running k8s int tests by @anfredette in https://github.com/bpfman/bpfman/pull/1110
* Remove propose_downstream packit job by @danielmellado in https://github.com/bpfman/bpfman/pull/1111
* cut an rc2 to ensure our rpm builds are working by @astoycos in https://github.com/bpfman/bpfman/pull/1114
* cut an rc3 release by @astoycos in https://github.com/bpfman/bpfman/pull/1115
* cut a rc4 release by @astoycos in https://github.com/bpfman/bpfman/pull/1116
* cut a 0.4.1-rc5 by @astoycos in https://github.com/bpfman/bpfman/pull/1117
* fix a bug in the release automation, test with rc6 by @astoycos in https://github.com/bpfman/bpfman/pull/1120

## New Contributors
* @thediveo made their first contribution in https://github.com/bpfman/bpfman/pull/1084
* @Silvanoc opened their first issue in https://github.com/bpfman/bpfman/issues/1077

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.4.0...v0.4.1
