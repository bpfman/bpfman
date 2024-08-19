The v0.5.1 release is a patch release that introduced a bytecode image spec update to support multiple images
and adds build tooling for bytecode images.
It also makes some changes so that the main branch uses the "latest" versions of the XDP and TC dispatchers,
and released versions use the version supported by the release.

## What's Changed
* build(deps): bump the production-dependencies group with 2 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1189
* Update image spec to support multiple images, add build tooling for bytecode images by @astoycos in https://github.com/bpfman/bpfman/pull/1141
* bpfman: XDP Mode Updates by @Billy99 in https://github.com/bpfman/bpfman/pull/1191
* Red Hat Konflux update bpfman by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1194
* fixup bytecode image building by @astoycos in https://github.com/bpfman/bpfman/pull/1192
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1195
* chore(deps): update fedora docker tag to v41 by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1196
* Don't use S390 object code for generating build args by @anfredette in https://github.com/bpfman/bpfman/pull/1201
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1198
* Use aya::Endianness in image.rs instead of object::Endianness by @anfredette in https://github.com/bpfman/bpfman/pull/1202
* build(deps): bump the production-dependencies group across 1 directory with 13 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1203
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1204
* Update aya dependency to get PerfEventArray fix by @anfredette in https://github.com/bpfman/bpfman/pull/1205
* chore(deps): update konflux references to f93024e by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1207
* update Aya to pick fix PR #1004 by @msherif1234 in https://github.com/bpfman/bpfman/pull/1208
* Fix FromAsCasing warnings in container files by 
@frobware
 in https://github.com/bpfman/bpfman/pull/1209
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman/pull/1210
* CNCF required website updates by @anfredette in https://github.com/bpfman/bpfman/pull/1211
* bpfman: dispatcher should point to release tag by @Billy99 in https://github.com/bpfman/bpfman/pull/1217
* build(deps): bump the production-dependencies group across 1 directory with 11 updates by @dependabot in https://github.com/bpfman/bpfman/pull/1218
* build(deps): bump sigstore/cosign-installer from 3.5.0 to 3.6.0 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman/pull/1219
* bpfman: Failed dispatcher load corrupts DB by @Billy99 in https://github.com/bpfman/bpfman/pull/1223
* bpfman-cli: bpfman get of non-bpfman loaded prog fails by @Billy99 in https://github.com/bpfman/bpfman/pull/1225
* bpfman: restore caps for bpfman-rpc as a service by @Billy99 in https://github.com/bpfman/bpfman/pull/1226
## New Contributors
* @frobware
 made their first contribution in https://github.com/bpfman/bpfman/pull/1209
**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.5.0...v0.5.1