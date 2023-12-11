![bpfman logo](./docs/img/bpfman_logo_256.png) <!-- markdownlint-disable-line first-line-heading -->

# bpfman: An eBPF Manager

[![License][apache2-badge]][apache2-url]
[![License][bsd2-badge]][bsd2-url]
[![License][gpl-badge]][gpl-url]
![Build status][build-badge]
[![Book][book-badge]][book-url]
[![Netlify Status][netlify-badge]][netlify-url]
[![Copr build status][copr-badge]][copr-url]

[apache2-badge]: https://img.shields.io/badge/License-Apache%202.0-blue.svg
[apache2-url]: https://opensource.org/licenses/Apache-2.0
[bsd2-badge]: https://img.shields.io/badge/License-BSD%202--Clause-orange.svg
[bsd2-url]: https://opensource.org/licenses/BSD-2-Clause
[gpl-badge]: https://img.shields.io/badge/License-GPL%20v2-blue.svg
[gpl-url]: https://opensource.org/licenses/GPL-2.0
[build-badge]: https://img.shields.io/github/actions/workflow/status/bpfman/bpfman/build.yml?branch=main
[book-badge]: https://img.shields.io/badge/read%20the-book-9cf.svg
[book-url]: https://bpfman.io/
[copr-badge]: https://copr.fedorainfracloud.org/coprs/g/ebpf-sig/bpfman-next/package/bpfman/status_image/last_build.png
[copr-url]: https://copr.fedorainfracloud.org/coprs/g/ebpf-sig/bpfman-next/package/bpfman/
[netlify-badge]: https://api.netlify.com/api/v1/badges/557ca612-4b7f-480d-a1cc-43b453502992/deploy-status
[netlify-url]: https://app.netlify.com/sites/bpfman/deploys

_Formerly know as `bpfd`_

## Welcome to bpfman

bpfman operates as an eBPF manager, focusing on simplifying the deployment and administration of eBPF programs. Its notable features encompass:

- **System Overview**: Provides insights into how eBPF is utilized in your system.
- **eBPF Program Loader**: Includes a built-in program loader that supports program cooperation for XDP and TC programs, as well as deployment of eBPF programs from OCI images.
- **eBPF Filesystem Management**: Manages the eBPF filesystem, facilitating the deployment of eBPF applications without requiring additional privileges.

Our program loader and eBPF filesystem manager ensure the secure deployment of eBPF applications.
Furthermore, bpfman includes a Kubernetes operator, extending these capabilities to Kubernetes.
This allows users to confidently deploy eBPF through custom resource definitions across nodes in a cluster.

Here are some links to help in your bpfman journey (all links are from the bpfman website <https://bpfman.io/>):

- [Welcome to bpfman](https://bpfman.io/) for overview of bpfman.
- [Setup and Building bpfman](https://bpfman.io/getting-started/building-bpfman/) for
  instructions on setting up your development environment and building bpfman.
- [Tutorial](https://bpfman.io/getting-started/tutorial/) for some examples of starting
  `bpfman`, managing logs, and using the CLI.
- [Example eBPF Programs](https://bpfman.io/getting-started/example-bpf/) for some
  examples of eBPF programs written in Go, interacting with `bpfman`.
- [How to Deploy bpfman on Kubernetes](https://bpfman.io/developer-guide/develop-operator/) for details on launching
  bpfman in a Kubernetes cluster.
- [Meet the Community](https://bpfman.io/governance/meetings/) for details on community meeting details.

## License

With the exception of eBPF code, everything is distributed under the terms of
the [Apache License] (version 2.0).

### eBPF

All eBPF code is distributed under either:

- The terms of the [GNU General Public License, Version 2] or the
  [BSD 2 Clause] license, at your option.
- The terms of the [GNU General Public License, Version 2].

The exact license text varies by file. Please see the SPDX-License-Identifier
header in each file for details.

Files that originate from the authors of bpfman use
`(GPL-2.0-only OR BSD-2-Clause)` - for example the [TC dispatcher] or our
own example programs.

Files that were originally created in [libxdp] use `GPL-2.0-only`.

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in this project by you, as defined in the GPL-2 license, shall be
dual licensed as above, without any additional terms or conditions.

[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL2
[BSD 2 Clause]: LICENSE-BSD2
[libxdp]: https://github.com/xdp-project/xdp-tools
