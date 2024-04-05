
The v0.4.0 release is a minor release and the first following the project
[rename from bpfd to bpfman](https://bpfman.io/main/blog/2023/11/23/bpfd-becomes-bpfman/).
From a design perspective, this release fully transitions bpfman to be a
library rather than a daemon.  From a user's perspective the main
difference is that the `bpfctl` CLI can now simply be called directly with the
`bpfman` binary, otherwise all of the major commands remain the same.

On top of transitioning to a library the community has also implemented some
exciting new features:

- Support for Uprobe Programs
- The ability to attach Uprobes inside containers (locally and in Kubernetes), see [the blog](https://bpfman.io/main/blog/2024/02/26/technical-challenges-for-attaching-ebpf-programs-in-containers/) for more.
- Support for Kprobe Programs
- Support for Fentry Programs
- Support for Fexit Programs

Additionally this release provides some new binary crates. The `bpfman-rpc` binary
allows other languages to call into the bpfman library using the existing
grpc api defined in the  `bpfman-api` crate bindings. The `bpf-metrics-exporter`,
and the `bpf-log-exporter`binaries allow users to gather useful information regarding
the bpf subsystem.

> [!WARNING]
FEATURE DEPRECATION: The ability to view all programs on a given node via the
`BpfProgram` CRD has been deprecated.  Instead the community now provides
the `bpf-metrics-exporter` crate.

> [!WARNING]
The CSI feature still requires a privileged application on distributions which
enable SELinux by default (i.e Red Hat Openshift).  Therefore we've shipped a set of
deployment configs specifically for openshift in this release, see the additional
`go-<example>-counter-install-ocp.yaml` artifacts included in the release
payload. Stay tuned to [#829](https://github.com/bpfman/bpfman/issues/596) for
updates.

## What's Changed (excluding dependency bumps)

* Correct cargo path by @danielmellado in https://github.com/bpfman/bpfman/pull/835
* Rename all of the things! by @dave-tucker in https://github.com/bpfman/bpfman/pull/834
* Merge bpfctl into bpfman by @dave-tucker in https://github.com/bpfman/bpfman/pull/826
* .github: Reinstate the image-build workflow by @dave-tucker in https://github.com/bpfman/bpfman/pull/839
* bpf-metrics-exporter: Add metrics exporter by @dave-tucker in https://github.com/bpfman/bpfman/pull/821
* docs/design: Add daemonless design doc by @dave-tucker in https://github.com/bpfman/bpfman/pull/778
* blog: Add blog about logo design by @dave-tucker in https://github.com/bpfman/bpfman/pull/840
* README: Update Title by @dave-tucker in https://github.com/bpfman/bpfman/pull/841
* .github: Bring back the verify check by @dave-tucker in https://github.com/bpfman/bpfman/pull/836
* blog: some small fixups by @astoycos in https://github.com/bpfman/bpfman/pull/847
* bpfman: Fix panic if XDP prog already loaded by @Billy99 in https://github.com/bpfman/bpfman/pull/849
* scripts: Update scripts to still cleanup bpfd by @Billy99 in https://github.com/bpfman/bpfman/pull/850
* Update Project Logo by @dave-tucker in https://github.com/bpfman/bpfman/pull/851
* docs: Simplify api-docs generation by @dave-tucker in https://github.com/bpfman/bpfman/pull/852
* Running `cargo xtask build-proto` resulted in some changes by @anfredette in https://github.com/bpfman/bpfman/pull/846
* docs: Update README by @dave-tucker in https://github.com/bpfman/bpfman/pull/854
* Metrics deployment updates by @astoycos in https://github.com/bpfman/bpfman/pull/853
* Support attaching uprobes to targets inside containers by @anfredette in https://github.com/bpfman/bpfman/pull/784
* packaging: Add RPMs by @dave-tucker in https://github.com/bpfman/bpfman/pull/848
* Packaging and Release Fixes by @dave-tucker in https://github.com/bpfman/bpfman/pull/868
* bpfman: in memory db setup + oci_utils conversion by @astoycos in https://github.com/bpfman/bpfman/pull/861
* Packaging tweaks by @dave-tucker in https://github.com/bpfman/bpfman/pull/869
* bpfman: cli: Refactor command handling by @dave-tucker in https://github.com/bpfman/bpfman/pull/870
* bpfman-actions: update -artifact by @astoycos in https://github.com/bpfman/bpfman/pull/884
* bpfman actions: remove put-issue-in-project by @astoycos in https://github.com/bpfman/bpfman/pull/883
* bpfman: Remove grpc section from config by @Billy99 in https://github.com/bpfman/bpfman/pull/885
* xtask: add man and completion script generation by @weiyuhang2011 in https://github.com/bpfman/bpfman/pull/873
* bpfman: add support for socket activation by @Billy99 in https://github.com/bpfman/bpfman/pull/872
* Sled integration for the core program type by @astoycos in https://github.com/bpfman/bpfman/pull/874
* docs: Community Meeting Minutes - Jan 4, 2024 by @Billy99 in https://github.com/bpfman/bpfman/pull/902
* Tidy up async code and shutdown handling by @dave-tucker in https://github.com/bpfman/bpfman/pull/903
* Sled fixes, convert xdpdispatcher to use sled, fix loaded_programs race by @astoycos in https://github.com/bpfman/bpfman/pull/901
* fixup xdp_pass_private test by @astoycos in https://github.com/bpfman/bpfman/pull/916
* Sled tc dispatcher by @astoycos in https://github.com/bpfman/bpfman/pull/910
* article: sled-db conversion by @astoycos in https://github.com/bpfman/bpfman/pull/911
* add daemonless doc to website fixup sled article  by @astoycos in https://github.com/bpfman/bpfman/pull/934
* packaging: Move RPM to use socket activation by @Billy99 in https://github.com/bpfman/bpfman/pull/922
* deps: Bump netlink dependencies by @dave-tucker in https://github.com/bpfman/bpfman/pull/953
* cli: fix nits in manpage and tab-completion by @Billy99 in https://github.com/bpfman/bpfman/pull/935
* refactor: make all sled-db keys into const by @shawnh2 in https://github.com/bpfman/bpfman/pull/923
* Kubernetes Support for attaching uprobes in containers by @anfredette in https://github.com/bpfman/bpfman/pull/875
* docs: Community Meeting Minutes - Jan 11 and 19, 2024 by @Billy99 in https://github.com/bpfman/bpfman/pull/954
* bpfman: move bpfman.sock to standalone directory by @Billy99 in https://github.com/bpfman/bpfman/pull/962
* docs: Minor nits by @Billy99 in https://github.com/bpfman/bpfman/pull/963
* bpfman: Make the listening socket configurable by @astoycos in https://github.com/bpfman/bpfman/pull/964
* DROP: Temporarily drop  rust-cache action by @astoycos in https://github.com/bpfman/bpfman/pull/974
* build: set golang to 1.21 for github builds by @Billy99 in https://github.com/bpfman/bpfman/pull/975
* sled fixups and prefixes for final conversion bits by @astoycos in https://github.com/bpfman/bpfman/pull/956
* operator: make health and metrics ports configurable by @Billy99 in https://github.com/bpfman/bpfman/pull/965
* build: make bundle failing on upstream main by @Billy99 in https://github.com/bpfman/bpfman/pull/976
* build: revert back to nightly by @Billy99 in https://github.com/bpfman/bpfman/pull/979
* ci: Don't fail the build on codecov by @dave-tucker in https://github.com/bpfman/bpfman/pull/981
* ci: Group dependabot updates by @dave-tucker in https://github.com/bpfman/bpfman/pull/982
* Enable loggercheck lint by @dave-tucker in https://github.com/bpfman/bpfman/pull/983
* chore: Fix yamllint/vscode-yaml formatting discrepencies by @dave-tucker in https://github.com/bpfman/bpfman/pull/984
* speed up bpfman image build by @astoycos in https://github.com/bpfman/bpfman/pull/986
* remove to-string clippy failures by @astoycos in https://github.com/bpfman/bpfman/pull/989
* fixups for pr #875 by @anfredette in https://github.com/bpfman/bpfman/pull/978
* fix startup race by @astoycos in https://github.com/bpfman/bpfman/pull/990
* Make bpfman get work without gRPC by @dave-tucker in https://github.com/bpfman/bpfman/pull/994
* Fix clippy errors flagged by new version of clippy by @anfredette in https://github.com/bpfman/bpfman/pull/998
* bpfman: Cache TUF Metadata by @dave-tucker in https://github.com/bpfman/bpfman/pull/1000
* Convert the rest of the cli to not use GRPC by @astoycos in https://github.com/bpfman/bpfman/pull/995
* Fix broken links in docs by @anfredette in https://github.com/bpfman/bpfman/pull/996
* chore(bpfman): Update aya dependency by @dave-tucker in https://github.com/bpfman/bpfman/pull/1006
* bpfman-agent: list filters not working by @Billy99 in https://github.com/bpfman/bpfman/pull/1004
* bpfman-operator: Add status to kubectl get commands by @Billy99 in https://github.com/bpfman/bpfman/pull/1007
* docs: Update Kubernetes integration test instructions by @anfredette in https://github.com/bpfman/bpfman/pull/1005
* update dependencies by @astoycos in https://github.com/bpfman/bpfman/pull/1008
* Image manager refactor by @astoycos in https://github.com/bpfman/bpfman/pull/1011
* blog: uprobe in container work by @anfredette in https://github.com/bpfman/bpfman/pull/1001
* chore(bpfman): Use native-tls by @dave-tucker in https://github.com/bpfman/bpfman/pull/1023
* blog: bpfman integration with AF_XDP by @maryamtahhan in https://github.com/bpfman/bpfman/pull/1010
* Fixup bpfd references in AF_XDP blog by @maryamtahhan in https://github.com/bpfman/bpfman/pull/1027
* bpfman: Add support for fentry/fexit program types by @Billy99 in https://github.com/bpfman/bpfman/pull/1024
* docs: Update authors by @Billy99 in https://github.com/bpfman/bpfman/pull/1029
* feat(bpf-log-exporter): Initial Commit by @dave-tucker in https://github.com/bpfman/bpfman/pull/1028
* Turn bpfman into a library crate by @astoycos in https://github.com/bpfman/bpfman/pull/1014
* Fixup K8s operator generated code  by @astoycos in https://github.com/bpfman/bpfman/pull/1033
* Add kprobe and uprobe example programs by @anfredette in https://github.com/bpfman/bpfman/pull/1017
* build docs on pull requests by @astoycos in https://github.com/bpfman/bpfman/pull/1035
* add public-api checks by @astoycos in https://github.com/bpfman/bpfman/pull/1037
* DROP pull in cosign fixes by @astoycos in https://github.com/bpfman/bpfman/pull/1042
* Add xtask rules to run lint and ut by @msherif1234 in https://github.com/bpfman/bpfman/pull/1039
* bpfman-operator: Add K8s support for fentry and fexit by @Billy99 in https://github.com/bpfman/bpfman/pull/1047
* Add kprobe and uprobe k8s integration tests by @anfredette in https://github.com/bpfman/bpfman/pull/1041
* bump sigstore-rs to 0.9.0 by @astoycos in https://github.com/bpfman/bpfman/pull/1051
* small doc fixups by @astoycos in https://github.com/bpfman/bpfman/pull/1052
* remove a bunch of un-needed apis by @astoycos in https://github.com/bpfman/bpfman/pull/1049
* fix public-api for latest nightly by @astoycos in https://github.com/bpfman/bpfman/pull/1057
* docs: Scrub documentation for Release 0.4.0 by @Billy99 in https://github.com/bpfman/bpfman/pull/1053
* stop checking bpfman-api and bpfman-csi public_api by @astoycos in https://github.com/bpfman/bpfman/pull/1061

## New Contributors
* @danielmellado made their first contribution in https://github.com/bpfman/bpfman/pull/835
* @weiyuhang2011 made their first contribution in https://github.com/bpfman/bpfman/pull/873
* @shawnh2 made their first contribution in https://github.com/bpfman/bpfman/pull/923
* @msherif1234 made their first contribution in https://github.com/bpfman/bpfman/pull/1039

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.3.1...v0.4.0