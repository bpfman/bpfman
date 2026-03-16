The v0.6.0 release is a minor release with significant architectural changes. The most notable change is splitting the `bpfman load` command into separate `load` and `attach` operations, giving users finer-grained control over BPF programme lifecycle management. Dispatcher bytecode is now embedded directly into the bpfman binary for hermetic builds, removing the runtime dependency on registry-hosted dispatcher images. The bpfman database has been relocated to `/run/bpfman/db` to fix state corruption after server reboots. RPC performance issues have been addressed, CLI tools now report version information, and the codebase has been updated to the Rust 2024 Edition.

## What's Changed
* split load and attach by @dave-tucker in https://github.com/bpfman/bpfman/pull/1354
* Add pre-commit hook by @danielmellado in https://github.com/bpfman/bpfman/pull/1309
* chore: Move completion/manpages to hidden cmds by @dave-tucker in https://github.com/bpfman/bpfman/pull/1422
* ci: Remove public-api checks by @dave-tucker in https://github.com/bpfman/bpfman/pull/1424
* chore: Unasync all the things by @dave-tucker in https://github.com/bpfman/bpfman/pull/1432
* chore(integration-test): Use rust test framework by @dave-tucker in https://github.com/bpfman/bpfman/pull/1434
* fix(bpfman-rpc): bpfman-rpc performance issues by @dave-tucker in https://github.com/bpfman/bpfman/pull/1437
* fix: correct links to install the crds and operator by @hanshal101 in https://github.com/bpfman/bpfman/pull/1440
* Update examples to use new ClusterBpfApplication by @Billy99 in https://github.com/bpfman/bpfman/pull/1447
* migrate tc to clusterbpfapplication by @msherif1234 in https://github.com/bpfman/bpfman/pull/1448
* rework kustomize for examples for new load/attach split api by @Billy99 in https://github.com/bpfman/bpfman/pull/1453
* chore: Remove public-api from precommit by @dave-tucker in https://github.com/bpfman/bpfman/pull/1465
* chore: Get cargo-deny working again by @dave-tucker in https://github.com/bpfman/bpfman/pull/1484
* chore: Group tonic|prost dependency updates by @dave-tucker in https://github.com/bpfman/bpfman/pull/1486
* update examples based on recent APIs review changes by @msherif1234 in https://github.com/bpfman/bpfman/pull/1494
* chore: Remove aws-lc-rs dependency by @dave-tucker in https://github.com/bpfman/bpfman/pull/1496
* chore: Update to Rust 2024 Edition by @dave-tucker in https://github.com/bpfman/bpfman/pull/1497
* fix: add binaries to container PATH by @monrax in https://github.com/bpfman/bpfman/pull/1501
* chore(docs): update copyright year by @monrax in https://github.com/bpfman/bpfman/pull/1502
* Chore/update meetings by @monrax in https://github.com/bpfman/bpfman/pull/1503
* chore: add PR template by @monrax in https://github.com/bpfman/bpfman/pull/1505
* chore: add issue reporting section to readme by @monrax in https://github.com/bpfman/bpfman/pull/1506
* cli: add get and list link by @Billy99 in https://github.com/bpfman/bpfman/pull/1507
* docs: update docs to match the load/attach changes by @Billy99 in https://github.com/bpfman/bpfman/pull/1511
* bpfman: invalid link fixes by @Billy99 in https://github.com/bpfman/bpfman/pull/1512
* Add markdown-lint to pre-commit by @bn222 in https://github.com/bpfman/bpfman/pull/1513
* Added golangci-lint to the pre-commit hooks by @aditya-shrivastavv in https://github.com/bpfman/bpfman/pull/1514
* chore: update to golangci-lint-v2 by @dave-tucker in https://github.com/bpfman/bpfman/pull/1516
* Add cargo-deny to precommit checks by @aditya-shrivastavv in https://github.com/bpfman/bpfman/pull/1517
* Update YAML schema path in VSCode settings by @aditya-shrivastavv in https://github.com/bpfman/bpfman/pull/1518
* docs: add OpenSSF Best Practices badge to README by @aditya-shrivastavv in https://github.com/bpfman/bpfman/pull/1521
* Update contributing guide to enhance structure and add testing policies by @aditya-shrivastavv in https://github.com/bpfman/bpfman/pull/1522
* chore: Fix cargo-deny pre-commit hook by @dave-tucker in https://github.com/bpfman/bpfman/pull/1525
* Clean up xdp and tc dispatcher state when attach fails by @anfredette in https://github.com/bpfman/bpfman/pull/1529
* ci: Integrate uv as package manager for documentation build by @mdaffad in https://github.com/bpfman/bpfman/pull/1530
* chore: Fix docs build by @dave-tucker in https://github.com/bpfman/bpfman/pull/1536
* Fix get_dispatcher() function by @anfredette in https://github.com/bpfman/bpfman/pull/1539
* Don't call init_image_manager until you need it by @anfredette in https://github.com/bpfman/bpfman/pull/1540
* docs: update docs with csi driver registrar notes by @Billy99 in https://github.com/bpfman/bpfman/pull/1543
* feat: add new workspace lints by @hanshal101 in https://github.com/bpfman/bpfman/pull/1546
* feat: Add version information to CLI tools by @frobware in https://github.com/bpfman/bpfman/pull/1551
* Update MAINTAINERS.md by @dave-tucker in https://github.com/bpfman/bpfman/pull/1558
* docs: Add note on Security Profiles Operator by @dave-tucker in https://github.com/bpfman/bpfman/pull/1560
* Fix go-xdp-counter-sharing-map example deployment error by @frobware in https://github.com/bpfman/bpfman/pull/1571
* Documentation fixes by @andreaskaris in https://github.com/bpfman/bpfman/pull/1573
* Fix: After a server reboot the state is corrupted by @andreaskaris in https://github.com/bpfman/bpfman/pull/1577
* v.0.5.6: build-release-yamls - fix typo in kustomize by @andreaskaris in https://github.com/bpfman/bpfman/pull/1591
* Improve error display for loading pinned BPF maps by @andreaskaris in https://github.com/bpfman/bpfman/pull/1595
* Fix clippy this pattern reimplements `Option::unwrap_or` by @andreaskaris in https://github.com/bpfman/bpfman/pull/1596
* Sigstore rs to 0.13.0 by @andreaskaris in https://github.com/bpfman/bpfman/pull/1614
* Embed dispatcher bytecode for hermetic builds by @frobware in https://github.com/bpfman/bpfman/pull/1631

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.5.6...v0.6.0

## New Contributors
* @hanshal101 made their first contribution in https://github.com/bpfman/bpfman/pull/1440
* @monrax made their first contribution in https://github.com/bpfman/bpfman/pull/1501
* @aditya-shrivastavv made their first contribution in https://github.com/bpfman/bpfman/pull/1514
* @bn222 made their first contribution in https://github.com/bpfman/bpfman/pull/1513
* @mdaffad made their first contribution in https://github.com/bpfman/bpfman/pull/1530
* @andreaskaris made their first contribution in https://github.com/bpfman/bpfman/pull/1573
