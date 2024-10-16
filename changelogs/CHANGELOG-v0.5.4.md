The v0.5.4 release is a patch release that fixes a glibc incompatibility between
the bpfman binaries and the base image in the bpfman container image, and a
Fedora RPM build issue.

## What's Changed
* image: bpfman image build needs same build and base image by @Billy99 in https://github.com/bpfman/bpfman/pull/1313
* Remove executable bit from vendored lib.rs by @danielmellado in https://github.com/bpfman/bpfman/pull/1310
* docs: add OpenShift note to QuickStart guide by @Billy99 in https://github.com/bpfman/bpfman/pull/1315

**Full Changelog**: https://github.com/bpfman/bpfman/compare/v0.5.3...v0.5.4