![Logo](./img/bpfd.png)

bpfd
====

A work in progress implementation of the xdp_multiprog protocol in Rust, using Aya.
It differs from the implementation in libxdp as we have chosen to implement a daemon instead.

- bpfd is the daemon
- bpfctl is the client program

There is a gRPC API that connects the two

## Requirements

- Rust Stable & Rust Nightly
- [bpf-linker](https://github.com/aya-rs/bpf-linker)
- protoc
- LLVM 11 or later
- ... and make sure the submodules are checked out

## Building

```
$ cargo xtask build-ebpf --release
$ cargo build
```

## Usage

Load the sample XDP Program:
```
$ cargo build
$ sudo ./target/debug/bpfd&
$ ./target/debug/bpfctl load ./target/bpfel-unknown-none/release/xdp-pass -p xdp -i wlp2s0 --priority 50 -s "pass"
```
## License

## bpfd-ebpf

Code in this crate is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the GPL-2 license, shall be dual licensed as above, without any additional terms or conditions.

## bpfd, bpfd-common

Rust code in all other crates is distributed under the terms of either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

The `bpfd` crate also contains eBPF code that is distributed under the terms of the [GNU General Public License, Version 2] or the [BSD 2 Clause] license, at your option. It is packaged, in object form, inside the `bpfd` binary.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL
[BSD 2 Clause]: LICENSE-BSD2
