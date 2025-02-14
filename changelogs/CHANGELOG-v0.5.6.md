The v0.5.6 release is a patch release. The primary reason for the release is to move the version of sigstore-rs because of a breaking change which caused all previous bpfman versions to no longer be able to verify images. The CSI driver container used by bpfman-operator was also updated to support multiple architectures. 

## What's Changed
* chore: Fix README images by @dave-tucker in https://github.com/bpfman/bpfman/pull/1373
* unpin cargo nightly by @Billy99 in https://github.com/bpfman/bpfman/pull/1376
* post v0.5.5 release updates by @Billy99 in https://github.com/bpfman/bpfman/pull/1372
* cli: fix arm64 arch recongnization with --cilium-ebpf-project flag by @junotx in https://github.com/bpfman/bpfman/pull/1377
* image pull failure by @Billy99 in https://github.com/bpfman/bpfman/pull/1380
* update public-api to 0.43.0 by @Billy99 in https://github.com/bpfman/bpfman/pull/1389
* docs: github doc build fails in package lxml by @Billy99 in https://github.com/bpfman/bpfman/pull/1393
* docs: building docs after a PR merges fails by @Billy99 in https://github.com/bpfman/bpfman/pull/1394
* ci: update scorecard workflow to clarify upload-artifact version by @frobware in https://github.com/bpfman/bpfman/pull/1395
* disable image verification until sigstore fixed by @Billy99 in https://github.com/bpfman/bpfman/pull/1400
* Create a single bytecode image with all supported program types by @anfredette in https://github.com/bpfman/bpfman/pull/1390
* reenable image verification by @Billy99 in https://github.com/bpfman/bpfman/pull/1406
* docs: move operator details from README to docs by @Billy99 in https://github.com/bpfman/bpfman/pull/1388
* Fix errors deleting tc and xdp programs in network namespaces by @anfredette in https://github.com/bpfman/bpfman/pull/1403
* update Cargo.lock by @Billy99 in https://github.com/bpfman/bpfman/pull/1409

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.5.5...v0.5.6

## New Contributors
* @junotx made their first contribution in https://github.com/bpfman/bpfman/pull/1377