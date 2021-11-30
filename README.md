bpfd
====

A work in progress bpf loading daemon

- bpfd is the daemon
- bpfctl is the client program

There is a gRPC API that connects the two

## Requirements

- Rust Stable
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

## TODO

- [ ] add support for BPF_PROG_TYPE_EXT in Aya
- [ ] add pidfile so we only have one daemon running
- [ ] recreate internal state on daemon restart
- [ ] make sure we pin programs the same way xdp multi_prog does

## License

Code in the bpf subdirectory is licensed under the terms of the [GNU General Public License, Version 2]

All other code in this project is distributed under the terms of either the [MIT license] or the [Apache License] (version 2.0), at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this crate by you, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

[MIT license]: LICENSE-MIT
[Apache license]: LICENSE-APACHE
[GNU General Public License, Version 2]: LICENSE-GPL