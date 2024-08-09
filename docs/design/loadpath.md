# bpfman outside of the load-path

## Introduction

One of the drawbacks of bpfman in it's current incarnation is that it requires
users to "opt in" to our opinionated loading pattern for eBPF programs.

From a "linux only" standpoint the benefits of our scheme are as follows:

1. Offering containers as a lightweight packaging alternative to RPMs/DEBs
   for eBPF programs.
1. Provides signing of eBPF Container Images through cosign.
1. Provides a way to load eBPF programs from a container image without
   requiring the container runtime to be installed on the host.
1. Allows for long-running user-space processes (that work with eBPF maps) to
   be run with no privileges, since most map read interactions are permissable
   without CAP_BPF. This is a security benefit, since it reduces the attack
   surface of the user-space process. There are a few exceptions to this
   rule (e.g perf_event_array maps), but we're planning on finding a way to
   work around this.
1. Simplified (and consistent) lifecycle management of eBPF programs regardless
   of the program type or attachment point.

Given that a lot of the software using eBPF is also Cloud-Native, we have the
following additional benefits:

1. A Cloud-Native, declarative way to load eBPF programs and have them
   orchestrated by Kubernetes. This is backed by packaging eBPF programs as
   container images.
1. For daemonsets/deployments that use these eBPF programs to not require
   privileged containers, host mounts or any capabilities.

These benefits however are proving to be insufficient to sway prospective users
away from loading their own eBPF programs for 2 reasons:

1. They are an established project and has code that works and don't see a
   reason to change it
1. They believe that separating the user-space code and kernel-space code is
   a bad idea, citing the following reasons:
   - The versions of the kernel and user-space must be tightly coupled.
   - They are wedded to BPF Skeletons (bpftool generated C code) since they
     require complex .bss/.rodata section manipulation before loading, and they
     argue that this is too complex to model via an API.

This design document explores a way to address these concerns, while still
providing the benefits of the current bpfman design.

## Design

### Requirements

We would like for bpfman to still be valuable to users who don't want to use
our opinionated loading pattern. To address the concerns raised above, we
must allow users to package and load their eBPF bytecode as they see fit.
However we are left with the following gaps:

1. Signing of eBPF programs - previously enabled via cosign.
1. Removing CAP_BPF from long running processes or daemonsets/deployments
   that use eBPF programs.
1. Simplified lifecycle management of eBPF programs through pinning.

We can immediately discard "simplified lifecycle management" as a requirement
since that becomes the responsibility of the user. However, we will evaluate
potential solutions to the other 2 requirements.

### Signing

Therefore, in order for bpfman to NOT be in the load path, we need to find a way
to preserve the security benefits of our current design.

#### fsverity and LSM Gatekeeper

[BPF Signing using fsverity and LSM Gatekeepers] is a proposal that was put
forward by Lorenz Bauer. It proposes a way to sign programs that contain
embedded eBPF bytecode (or eBPF bytecode itself), the kernel will then verify
the signature when the program is loaded. LSM hooks can the be used to further
apply policy to the program that's being loaded.

This prevents a bad actor from modifying the eBPF bytecode in a program, since
the signature will no longer be valid.

This proposal is still a work in progress, but it's a promising development.
In particular, the integration with the IMA subsystem is very interesting as it
opens up the possibility of remote attestations using Keylime. Not only that,
the more I learn about IMAs capabilities, the more I think it's a good fit for
eBPF.

There are however some drawbacks to this approach:

- Fsverity is limited to ext4, btrfs and f2fs filesystems.
- Support for fsverity on overlayfs is enabled via mount option - additional work
  may be required to support this in container runtimes.
- This approach is less effective on static binaries, since if they were to,
  for example, link to libbpf, then you would also need libbpf to be signed
  with fsverity to avoid any potential tampering. This could be mitigated by
  having relocations performed by the kernel instead.
- Some investigation is needed to see whether the fsverity xattrs are preserved
  when a file is copied into a container image, or signed there as part of the
  build process.

[BPF Signing using fsverity and LSM Gatekeepers]: http://vger.kernel.org/bpfconf2023_material/Lorenz_Bauer_-_BPF_signing_using_fsverity_and_LSM_gatekeeper.pdf

#### BPF Signatures using eBPF Skeletons

[BPF Signatures] proposal is being championed by KP Singh.
This approach involves generating an eBPF "light skeleton" that contains:

- The eBPF bytecode
- A recording of the syscalls that would be used to load the program

This light skeleton would then be signed, and the signature would be verified
by the kernel when the program is loaded. eBPF LSM or IMA could be used to
further apply policy to the program that's being loaded.

This uses the eBPF program type "BPF_PROG_TYPE_SYSCALL", which effectively uses
eBPF to replay the syscalls and load the program.

There are some drawbacks to this approach also:

- CO:RE relocations occur when you generate the skeleton. As such, you would
  need to generate a skeleton for each kernel version that you want to support
  which partially defeats the point of CO:RE in the first place.
- Light Skeletons are not supported for all program types
- I don't believe that this approach would work with BPF Tokens since the
  token cannot be embedded in the skeleton.

[BPF Signatures]: https://lpc.events/event/16/contributions/1357/attachments/1045/1999/BPF%20Signatures.pdf

#### Signing Conclusion

Both of these approaches have merits and some drawbacks.
In an ideal world, we would have both of these approaches available to us in the
following way:

- All CO:RE relocations occur in the kernel - making libbpf and other libraries
  that use CO:RE much simpler.
- As such, the instruction buffer generated by the compiler is "stable" enough
  to be signed.
- The proposed verification and policy application mechanisms in the proposal
  are implemented.
- The requirement to use "light skeletons" is removed to allow for all program
  types to be supported.
- fs-verity can be used to ensure the integrity of "loader programs" and or
  bytecode on disk (replacing the requirement to sign the syscall recording).
- Implement fs-verity support on more filesystems and ensure that container
  runtimes extend support to overlayfs by default.

In doing so we'd have a solution that covers all of the use-cases that we
support.

From a bpfman standpoint, we'd still require container images to be signed but
only for supply chain security. The inner eBPF bytecode would also be signed.

Loader programs are outside the scope of bpfman, but we could provide a
recommendation that the program itself is signed with fs-verity and the container
it ships in is signed with cosign.

### Removing CAP_BPF

Removing CAP_BPF is desirable since:

1. It's effectively granting root privileges to the process.
1. It's not a fine-grained capability, so it's not possible to grant only the
   permissions that are required.

While it may not be possible to remove it entirely, we could either:

1. Support its use in a user namespace - which somewhat confines the
   capabilities that are granted.
1. Police the use of CAP_BPF using some other mechanism to ensure only certain
   operations are permitted.

#### BPF Token

The [BPF Token patchset] is a set of patches that were merged into the Linux
kernel recently. It allows for the loading of eBPF programs from a
user namespace by passing a BPF token file descriptor during bpf() syscalls
which effectively skips the security checks that would normally be performed
when loading eBPF programs - checking instead that the token is permitted to
perform this operation.

While this is still a work in progress, it is a promising development and
something we can leverage to achieve our goals.

The integration would look something like this:

1. Using the bpfman CSI plugin in Kubernetes, it would be possible to express -
   through the volume attributes - that a pod should be given a token with
   whichever permissions are required and that it should be placed to a given
   location on the bpf filesystem.
1. At this point, there is nothing more for bpfman to do, since the token is
   already in place and the pod can load eBPF programs as it sees fit.

This would also be exposed through a `bpfman token create` command, which would
create a token with the given permissions and place it at the given location
on the bpf filesystem. This could then be mounted into a container and used
to load eBPF programs.

To use the token, you must still have CAP_BPF permissions granted in the
user namespace.

There are a lot of great things about this approach. The only potential
downside that we can see is that there are some use-cases that won't be
supported. For example, attaching a uprobe to a binary in another pid/mount
namespace. Therefore, we may still need a "rootful" eBPF program loader for
these use-cases.

[BPF Token patchset]: https://lore.kernel.org/bpf/CAEiveUeDLr00SjyU=SMSc4XbHSA6LTn4U2DHr12760rbo5WqSw@mail.gmail.com/T/

#### Syscall Proxying (seccomp-notify)

seccomp-notify is another possible way to solve the problem.
As described in a blog post titled [The Seccomp Notifier: New Frontiers in Unprivileged Container Development].

The premise here is that syscalls are proxied into user-space, where they can
be handled by a user-space process that has the required capabilities, and then
the result of the syscall is returned to the caller.

When presented as an alternative to the BPF Token patchset, it was dismissed
since Christian Brauner (the author of the blog post) described it as
"impractical". However, there is a project called [Seitan] which seeks to
address some of the practicality issues, and also provide container runtime
integration.

The integration would look something like this:

1. bpfman might be able to provide information to Seitan that can be used to
   generate the seccomp filters that are required to proxy syscalls.
1. bpfman might do nothing, except for preparing a bpffs filesystem for the
   container runtime to use.

[The Seccomp Notifier: New Frontiers in Unprivileged Container Development]: https://people.kernel.org/brauner/the-seccomp-notifier-new-frontiers-in-unprivileged-container-development
[Seitan](https://seitan.rocks/seitan/about/)

#### Policing CAP_BPF (via LSM or otherwise)

It's possible to police how the bpf syscall is used by a process using LSM.

This would likely require some "profiling" of an eBPF application in order
to derive a policy that we could apply - using eBPF LSM or something else.
This would be a one-time cost at development time.
You could choose to issue warnings or block operations that are not permitted by
the policy.

Such an approach was [demonstrated at CloudNativeSecurityCon] by the Tetragon
team.

The downside here is that the security guarantee is only ever as strong as your
policy. The policies required may end up being pretty verbose and complex.

[demonstrated at CloudNativeSecurityCon](https://youtu.be/UBVTJ0LeXxc?si=lGjcHK_CAuYd1d5v)

#### CAP_BPF Conclusion

The use of eBPF in rootless containers (user namespaces) is still
a work in progress. In the short term, the following work items are required:

1. Teach bpfman to generate BPF tokens.
1. Permit BPF token generation into the bpfman CSI plugin.
1. Try loading eBPF programs from a user namespace using BPF tokens and confirm
   which use-cases are supported, and which are not.
1. Present the results of that investigation to the kernel community and
   determine whether it's possible to support the remaining use-cases with
   BPF tokens, or whether they will always be required to be run as root.

In the medium term, we should consider the following:

1. Work with the Seitan authors on supporting the bpf syscall.
1. Scope out what controls Seitan offers over which syscalls can be proxied,
   and how much room we have to define policy there.
1. See what value, if any, bpfman could add on top of this.

Longer term we might consider working with the Tetragon team on policy
profiling and enforcement for eBPF programs.

## In Summary

In summary, we have a path forward to remove bpfman from the load path,
while still providing some of the benefits of our current design.
However, this is highly dependent on whether or not we can successfully convince
the proposal authors to adapt their proposals to our needs.

We'll continue to monitor the progress of these proposals and see if we can
help in any way, while also incubating these ideas in bpfman.
