The v0.3.0 release is our second official minor release of the the bpfd project.

> **WARNING**
This release contains breaking changes for both the core bpfd GRPC API
as well as the Kubernetes CRDs.  There is no backwards compatibility or guarantees
with any of our previous releases at this point.

The following describes some of the major new features/updates:

- The ability to list and get kernel information for ALL programs regardless
of whether they were loaded by bpfd or another process

- Deprecation of bpfd specific UUIDs for each loaded program in favor of the
standard generated kernel ID for all programs regardless of what process loaded them.

- Support for some new bpf program types: 
    * `Uprobe`
    * `Uretprobe`
    * `Kprobe`
    * `KretProbe`
    Along with their corresponding K8s API CRD Resources (`Uprobe` and `Kprobe` CRDs)

- `bpfctl` got some exciting new features + functionality:
    * The ability to pre-pull a bytecode image from a remote repository for later use
    * The ability to get a program based on Kernel ID
    * The ability to load a program with user determined metadata labels which the user
    can later use to filter via a `bpfctl` list
    * Much better formatting and both kernel + bpfd information feedback on `load`, `list`
    and `get` calls

- For maps, multiple programs can now share the same maps via the `map_owner_id` field
allowing for data sharing across various programs which are loaded via bpfd

- Removal of the cert-manager dependency in the bpfd kubernetes deployment

- Preliminary CSI(Container Storage Interface) support which allows applications
to receive their maps in kubernetes applications with a simple custom volume type

## New Contributors
* @maryamtahhan made their first contribution in https://github.com/bpfd-dev/bpfd/pull/540
* @navarrothiago made their first contribution in https://github.com/bpfd-dev/bpfd/pull/720

**Full Changelog**: https://github.com/bpfd-dev/bpfd/compare/v0.2.1...v0.3.0