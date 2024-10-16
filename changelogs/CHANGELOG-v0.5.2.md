The v0.5.2 release is a patch release that updates bpfman to aya version 0.13.0
and aya-obj version 0.2.0.  This update allows bpfman to make a proper bpfman
release that doesn't have Aya pinned to a git SHA.

Other Notable Changes:
* Add support for ppc64le and s390x
* Allow Cosign to be disabled and enabled at runtime.
* Added an OpenSSF Scorecard action to scan bpfman for security best practices
  on a regular basis and started making recommended improvements.

## What's Changed
* Add bpfman OpenShift container to build bpfman by @msherif1234 in
  https://github.com/bpfman/bpfman/pull/1231
* bpfman/lib: add function documentation by @frobware in
  https://github.com/bpfman/bpfman/pull/1186
* Move Fedora back to release 40 in Containerfile.bpfman.local by @anfredette in
  https://github.com/bpfman/bpfman/pull/1235
* bpfman: Make whether to use cosign configurable by @anfredette in
  https://github.com/bpfman/bpfman/pull/1240
* Fix BUILDPLATFORM redefinition issue in bpfman Containerfile by @msherif1234
  in https://github.com/bpfman/bpfman/pull/1244
* Add an integration test for adding a program with cosign disabled by
  @anfredette in https://github.com/bpfman/bpfman/pull/1242
* bpfman: add ppc64 and s390x support by @Billy99 in
  https://github.com/bpfman/bpfman/pull/1234
* ci: run image-build.yaml when Containerfiles are updated by @Billy99 in
  https://github.com/bpfman/bpfman/pull/1263
* Update to sigstore v0.10.0 to pickup cosign fix by @anfredette in
  https://github.com/bpfman/bpfman/pull/1262
* chore: Update Artwork by @dave-tucker in
  https://github.com/bpfman/bpfman/pull/1252
* ci: pin to rust nightly-2024-09-24 by @Billy99 in
  https://github.com/bpfman/bpfman/pull/1283
* ci: add --toolchain to public-api to make pinning easier by @Billy99 in
  https://github.com/bpfman/bpfman/pull/1284
* docs: Update docs by @Billy99 in https://github.com/bpfman/bpfman/pull/1236
* ci: Pin actions to SHAs by @dave-tucker in
  https://github.com/bpfman/bpfman/pull/1294
* cI: Create scorecard.yml by @dave-tucker in
  https://github.com/bpfman/bpfman/pull/1292
* Restore the build badge by @anfredette in
  https://github.com/bpfman/bpfman/pull/1295
* Allow the scorecard workflow to be manually triggered by @anfredette in
  https://github.com/bpfman/bpfman/pull/1296
* Update the release documentation by @anfredette in
  https://github.com/bpfman/bpfman/pull/1288
* Upgrade to aya version 0.13.0 and aya-obj version 0.2.0 by @anfredette in
  https://github.com/bpfman/bpfman/pull/1298

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.5.1...v0.5.2

## Known Issues
* The the OpenShift console should not be used to install the v0.5.2 bpfman
  community operator because it will use the latest version of the bpfman
  container image which may or may not work. 
