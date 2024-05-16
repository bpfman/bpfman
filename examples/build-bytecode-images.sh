#!/bin/bash

export RUST_LOG=debug

../target/debug/bpfman image build \
    -b ./go-xdp-counter/bpf_bpfel.o \
    -f ../Containerfile.bytecode \
    --tag ${IMAGE_XDP_BC}

../target/debug/bpfman image build \
    -b ./go-tc-counter/bpf_bpfel.o \
    -f ../Containerfile.bytecode \
    --tag ${IMAGE_TC_BC}

../target/debug/bpfman image build \
-b ./go-tracepoint-counter/bpf_bpfel.o \
-f ../Containerfile.bytecode \
--tag ${IMAGE_TP_BC}

../target/debug/bpfman image build \
    -b ./go-kprobe-counter/bpf_bpfel.o \
    -f ../Containerfile.bytecode \
    --tag ${IMAGE_KP_BC}

../target/debug/bpfman image build \
    -b ./go-uprobe-counter/bpf_bpfel.o \
    -f ../Containerfile.bytecode \
    --tag ${IMAGE_UP_BC}

../target/debug/bpfman image build \
    -b ./go-uretprobe-counter/bpf_x86_bpfel.o \
    -f ../Containerfile.bytecode \
    --tag ${IMAGE_URP_BC}

../target/debug/bpfman image build \
    -b ./go-app-counter/bpf_bpfel.o \
    -f ../Containerfile.bytecode \
    --tag ${IMAGE_APP_BC}