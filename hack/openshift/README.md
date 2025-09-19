# OpenShift Build Tools

Tools for maintaining OpenShift-specific build artefacts.

## Version Synchronisation

Synchronise version labels in OpenShift Containerfiles with the upstream Cargo.toml:

```bash
make -C hack/openshift set-version
```

Run this after:
- Merging from upstream when version changes
- Updating version in Cargo.toml

The script reads `version` from `[workspace.package]` in Cargo.toml and updates all `version=` labels in OpenShift-specific Containerfiles.
