# Welcome to bpfd

bpfd is a system daemon for managing eBPF programs.

It is currently a work in progress.

## Goals

- To allow multiple XDP programs to share the same interface
- To allow administrator defined ordering of network eBPF programs
- To allow programs to be loaded at launch time
- To simplify the packaging and loading of eBPF-based infrastructure software
