The v0.5.5 release is a patch release that introduced the TCX program type, support for attaching TCX, TC and XDP programs inside containers, and improved logs. Namespace scoped CRDs were added in the bpfman-operator repository., which are a subset of the existing Cluster scoped CRDs.

## What's Changed
* Free up space on the build workflow runner by @anfredette in https://github.com/bpfman/bpfman/pull/1317
* Restore the dispatcher versions to "latest" by @anfredette in https://github.com/bpfman/bpfman/pull/1320
* Add cosign known issue to v0.5.1 release notes by @anfredette in https://github.com/bpfman/bpfman/pull/1321
* correct file extension for CI workflow configuration by @emmanuel-ferdman in https://github.com/bpfman/bpfman/pull/1322
* Use Ubuntu 24.04 everywhere by @anfredette in https://github.com/bpfman/bpfman/pull/1324
* docs: document log and metric exporters by @Billy99 in https://github.com/bpfman/bpfman/pull/1287
* Add Support for TCX Programs by @anfredette in https://github.com/bpfman/bpfman/pull/1222
* Move bpfman container back to redhat/ubi9-minimal by @anfredette in https://github.com/bpfman/bpfman/pull/1325
* image: Move bpfman container back to Ubuntu 24.04 by @Billy99 in https://github.com/bpfman/bpfman/pull/1326
* Modify filter() to just get TC programs by @anfredette in https://github.com/bpfman/bpfman/pull/1327
* update aya and  aya-obj by @msherif1234 in https://github.com/bpfman/bpfman/pull/1332
* bpfman: every load and unload request is logged by @Billy99 in https://github.com/bpfman/bpfman/pull/1331
* Post release updates to RELEASE.md by @anfredette in https://github.com/bpfman/bpfman/pull/1301
* Add an eBPF program with 9 global variables by @anfredette in https://github.com/bpfman/bpfman/pull/1338
* bpfman: Make TC and XDP dispatcher configurable by @ShockleyJE in https://github.com/bpfman/bpfman/pull/1335
* Update configuration documentation req. PR#1335 by @ShockleyJE in https://github.com/bpfman/bpfman/pull/1342
* config: rework how default values are managed by @Billy99 in https://github.com/bpfman/bpfman/pull/1345
* Support attaching XDP, TC, and TCX programs inside network namespaces by @anfredette in https://github.com/bpfman/bpfman/pull/1340
* Remove legacy dispatcher directories by @anfredette in https://github.com/bpfman/bpfman/pull/1351
* doc: update example building to use bpfman image build by @Billy99 in https://github.com/bpfman/bpfman/pull/1333
* bpfman: fixed the panic issue of 'list' command when the prog name is… by @spring-cxz in https://github.com/bpfman/bpfman/pull/1357
* Fix Error deleting tc and xdp programs after netns deleted by @anfredette in https://github.com/bpfman/bpfman/pull/1363
* doc: add design document for Cluster vs Namespace by @Billy99 in https://github.com/bpfman/bpfman/pull/1359

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.5.4...v0.5.5

## New Contributors
* @emmanuel-ferdman made their first contribution in https://github.com/bpfman/bpfman/pull/1322
* @ShockleyJE made their first contribution in https://github.com/bpfman/bpfman/pull/1335
* @spring-cxz made their first contribution in https://github.com/bpfman/bpfman/pull/1357
