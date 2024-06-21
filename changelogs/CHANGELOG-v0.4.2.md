The v0.4.2 release is a patch release which is mainly cut pull in some OLM bundle
fixes for the bpfman-operator OLM manifest deployment.

## What's Changed
* build(deps): bump tokio-util from 0.7.10 to 0.7.11 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1122
* docs: cleanup documentation nits for v0.4.1 release by @Billy99 in https://github.com/bpfman/bpfman/pull/1123
* build(deps): bump golangci/golangci-lint-action from 5 to 6 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1135
* build(deps): bump the production-dependencies group with 5 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1136
* RPM fixes  by @astoycos in https://github.com/bpfman/bpfman/pull/1134
* build(deps): bump the production-dependencies group with 7 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1144
* bpfman: don't panic if DatabaseLockError occurred by @ZhangShuaiyi in https://github.com/bpfman/bpfman/pull/1140
* Make DatabaseConfig max_retries work as defined. by @anfredette in https://github.com/bpfman/bpfman/pull/1147
* Have systemd-rpm-macros use bpfman service by @danielmellado in https://github.com/bpfman/bpfman/pull/1146
* docs: update the RPM uninstall process by @Billy99 in https://github.com/bpfman/bpfman/pull/1148
* build(deps): bump serde from 1.0.202 to 1.0.203 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1149
* build(deps): bump tokio from 1.37.0 to 1.38.0 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1151
* fix public-api check by @astoycos in https://github.com/bpfman/bpfman/pull/1152
* build(deps): bump the production-dependencies group with 6 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1153
* Temporary work-around for multi-program container images by @anfredette in https://github.com/bpfman/bpfman/pull/1155
* bpfman: add multiarch builds for bpfman binaries by @Billy99 in https://github.com/bpfman/bpfman/pull/1150
* build(deps): bump the production-dependencies group with 2 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1160
* build(deps): bump the production-dependencies group with 2 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1159
* multiarch: Move multiarch building to bpfman-operator repo by @Billy99 in https://github.com/bpfman/bpfman/pull/1158
* examples: cleanup patch.yaml files by @Billy99 in https://github.com/bpfman/bpfman/pull/1161

## New Contributors
* @ZhangShuaiyi made their first contribution in https://github.com/bpfman/bpfman/pull/1140

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.4.1...v0.4.2