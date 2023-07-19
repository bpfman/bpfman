#!/bin/bash

docker login quay.io

docker build \
 --build-arg PROGRAM_NAME=xdp_pass \
 --build-arg SECTION_NAME=pass \
 --build-arg PROGRAM_TYPE=xdp \
 --build-arg BYTECODE_FILENAME=xdp_pass.bpf.o \
 -f ../../../packaging/container-deployment/Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/xdp_pass:latest

docker push quay.io/bpfd-bytecode/xdp_pass

docker build \
 --build-arg PROGRAM_NAME=tc_pass \
 --build-arg SECTION_NAME=pass \
 --build-arg PROGRAM_TYPE=tc \
 --build-arg BYTECODE_FILENAME=tc_pass.bpf.o \
 -f ../../../packaging/container-deployment/Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/tc_pass:latest

docker push quay.io/bpfd-bytecode/tc_pass

docker build \
 --build-arg PROGRAM_NAME=tracepoint \
 --build-arg SECTION_NAME=sys_enter_openat \
 --build-arg PROGRAM_TYPE=tracepoint \
 --build-arg BYTECODE_FILENAME=tp_openat.bpf.o \
 -f ../../../packaging/container-deployment//Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/tracepoint:latest

docker push quay.io/bpfd-bytecode/tracepoint

docker build \
 --build-arg PROGRAM_NAME=uprobe \
 --build-arg SECTION_NAME=my_uprobe \
 --build-arg PROGRAM_TYPE=kprobe \
 --build-arg BYTECODE_FILENAME=uprobe.bpf.o \
 -f ../../../packaging/container-deployment//Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/uprobe:latest

docker push quay.io/bpfd-bytecode/uprobe

docker build \
 --build-arg PROGRAM_NAME=uretprobe \
 --build-arg SECTION_NAME=my_uretprobe \
 --build-arg PROGRAM_TYPE=kprobe \
 --build-arg BYTECODE_FILENAME=uprobe.bpf.o \
 -f ../../../packaging/container-deployment//Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/uretprobe:latest

docker push quay.io/bpfd-bytecode/uretprobe

docker build \
 --build-arg PROGRAM_NAME=kprobe \
 --build-arg SECTION_NAME=my_kprobe \
 --build-arg PROGRAM_TYPE=kprobe \
 --build-arg BYTECODE_FILENAME=kprobe.bpf.o \
 -f ../../../packaging/container-deployment//Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/kprobe:latest

docker push quay.io/bpfd-bytecode/kprobe

docker build \
 --build-arg PROGRAM_NAME=kretprobe \
 --build-arg SECTION_NAME=my_kretprobe \
 --build-arg PROGRAM_TYPE=kprobe \
 --build-arg BYTECODE_FILENAME=kprobe.bpf.o \
 -f ../../../packaging/container-deployment//Containerfile.bytecode \
 ./.output -t quay.io/bpfd-bytecode/kretprobe:latest

docker push quay.io/bpfd-bytecode/kretprobe


