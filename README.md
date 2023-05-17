# [![bpfd](./docs/img/bpfd.svg)](https://bpfd.netlify.app/)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![License:
MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![License](https://img.shields.io/badge/License-BSD_2--Clause-orange.svg)](https://opensource.org/licenses/BSD-2-Clause)
[![License: GPL
v2](https://img.shields.io/badge/License-GPL_v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html)
![Build status][build-badge] [![Book][book-badge]][book-url]
[![Netlify Status](https://api.netlify.com/api/v1/badges/557ca612-4b7f-480d-a1cc-43b453502992/deploy-status)](https://app.netlify.com/sites/bpfd/deploys)

[build-badge]:
    https://img.shields.io/github/actions/workflow/status/bpfd-dev/bpfd/build.yml?branch=main
[book-badge]: https://img.shields.io/badge/read%20the-book-9cf.svg
[book-url]: https://bpfd.netlify.app/

## Welcome to bpfd

bpfd is a system daemon aimed at simplifying the deployment and management of eBPF programs.
It's goal is to enhance the developer-experience as well as provide features to improve security,
visibility and program-cooperation.
bpfd includes a Kubernetes operator to bring those same features to Kubernetes, allowing users to
safely deploy eBPF via custom resources across nodes in a cluster.

Here are some links to help in your bpfd journey (all links are from the bpfd website https://bpfd.netlify.app/):

- [Welcome to bpfd](https://bpfd.netlify.app/) for overview of bpfd.
- [Setup and Building bpfd](https://bpfd.netlify.app/getting-started/building-bpfd/) for
  instructions on setting up your development environment and building bpfd.
- [Tutorial](https://bpfd.netlify.app/getting-started/tutorial/) for some examples of starting
  `bpfd`, managing logs, and using `bpfctl`.
- [Example eBPF Programs](https://bpfd.netlify.app/getting-started/example-bpf/) for some
  examples of eBPF programs written in Go, interacting with `bpfd`.
- [How to Deploy bpfd on Kubernetes](https://bpfd.netlify.app/developer-guide/develop-operator/) for details on launching
  bpfd in a Kubernetes cluster.
- [Meet the Community](https://bpfd.netlify.app/governance/meetings/) for details on community meeting details.
