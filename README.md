bpfd
====

A work in progress implementation of the xdp_multiprog protocol in Rust, using Aya.
It differs from the implementation in libxdp as we have chosen to implement a daemon instead.

- bpfd is the daemon
- bpfctl is the client program

There is a gRPC API that connects the two

## Requirements

- Rust Stable & Rust Nightly
- My [fork](https://github.com/dave-tucker/bpf-linker/tree/bpf-v2) of bpf-linker
- protoc
- LLVM 11 or later

## Building

```
$ cargo build
```

This will also compile the protobuf file and the *.bpf.c programs

## Usage

Load the sample XDP Program:
```
$ cargo build
$ sudo ./target/debug/bpfd&
$ ./target/debug/bpfctl load ./bpf/.output/xdp_pass.bpf.o -p xdp -i wlp2s0 --priority 50 -s "pass"
```
## License

Code in the bpf subdirectory is licensed under the terms of the [GNU General Public License, Version 2]

All other code in this project is distributed under the terms of either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL
