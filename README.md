# [![bpfman](./docs/img/bpfman.svg)](https://bpfman.io/)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![License](https://img.shields.io/badge/License-BSD_2--Clause-orange.svg)](https://opensource.org/licenses/BSD-2-Clause)
[![License: GPL
v2](https://img.shields.io/badge/License-GPL_v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html)
![Build status][build-badge] [![Book][book-badge]][book-url]
[![Netlify Status](https://api.netlify.com/api/v1/badges/557ca612-4b7f-480d-a1cc-43b453502992/deploy-status)](https://app.netlify.com/sites/bpfman/deploys)

[build-badge]:
    https://img.shields.io/github/actions/workflow/status/bpfman/bpfman/build.yml?branch=main
[book-badge]: https://img.shields.io/badge/read%20the-book-9cf.svg
[book-url]: https://bpfman.io/

_Formerly know as `bpfd`_

## Welcome to bpfman

bpfman is a system daemon aimed at simplifying the deployment and management of eBPF programs.
It's goal is to enhance the developer-experience as well as provide features to improve security,
visibility and program-cooperation.
bpfman includes a Kubernetes operator to bring those same features to Kubernetes, allowing users to
safely deploy eBPF via custom resources across nodes in a cluster.

Here are some links to help in your bpfman journey (all links are from the bpfman website <https://bpfman.io/>):

- [Welcome to bpfman](https://bpfman.io/) for overview of bpfman.
- [Setup and Building bpfman](https://bpfman.io/getting-started/building-bpfman/) for
  instructions on setting up your development environment and building bpfman.
- [Tutorial](https://bpfman.io/getting-started/tutorial/) for some examples of starting
  `bpfman`, managing logs, and using `bpfctl`.
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
