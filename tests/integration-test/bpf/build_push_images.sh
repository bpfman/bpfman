#!/bin/bash

docker login quay.io

docker build \
 --build-arg PROGRAM_NAME=xdp_pass \
 --build-arg BPF_FUNCTION_NAME=pass \
 --build-arg PROGRAM_TYPE=xdp \
 --build-arg BYTECODE_FILENAME=xdp_pass.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/xdp_pass:latest

docker push quay.io/bpfman-bytecode/xdp_pass

docker build \
 --build-arg PROGRAM_NAME=xdp_pass_private \
 --build-arg BPF_FUNCTION_NAME=pass \
 --build-arg PROGRAM_TYPE=xdp \
 --build-arg BYTECODE_FILENAME=xdp_pass.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/xdp_pass_private:latest

docker push quay.io/bpfman-bytecode/xdp_pass_private

docker build \
 --build-arg PROGRAM_NAME=tc_pass \
 --build-arg BPF_FUNCTION_NAME=pass \
 --build-arg PROGRAM_TYPE=tc \
 --build-arg BYTECODE_FILENAME=tc_pass.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/tc_pass:latest

docker push quay.io/bpfman-bytecode/tc_pass

docker build \
 --build-arg PROGRAM_NAME=tracepoint \
 --build-arg BPF_FUNCTION_NAME=enter_openat \
 --build-arg PROGRAM_TYPE=tracepoint \
 --build-arg BYTECODE_FILENAME=tp_openat.bpf.o \
 -f ../../..Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/tracepoint:latest

docker push quay.io/bpfman-bytecode/tracepoint

docker build \
 --build-arg PROGRAM_NAME=uprobe \
 --build-arg BPF_FUNCTION_NAME=my_uprobe \
 --build-arg PROGRAM_TYPE=uprobe \
 --build-arg BYTECODE_FILENAME=uprobe.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/uprobe:latest

docker push quay.io/bpfman-bytecode/uprobe

docker build \
 --build-arg PROGRAM_NAME=uretprobe \
 --build-arg BPF_FUNCTION_NAME=my_uretprobe \
 --build-arg PROGRAM_TYPE=uretprobe \
 --build-arg BYTECODE_FILENAME=uprobe.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/uretprobe:latest

docker push quay.io/bpfman-bytecode/uretprobe

docker build \
 --build-arg PROGRAM_NAME=kprobe \
 --build-arg BPF_FUNCTION_NAME=my_kprobe \
 --build-arg PROGRAM_TYPE=kprobe \
 --build-arg BYTECODE_FILENAME=kprobe.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/kprobe:latest

docker push quay.io/bpfman-bytecode/kprobe

docker build \
 --build-arg PROGRAM_NAME=kretprobe \
 --build-arg BPF_FUNCTION_NAME=my_kretprobe \
 --build-arg PROGRAM_TYPE=kretprobe \
 --build-arg BYTECODE_FILENAME=kprobe.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/kretprobe:latest

docker push quay.io/bpfman-bytecode/kretprobe

docker build \
 --build-arg PROGRAM_NAME=do_unlinkat \
 --build-arg BPF_FUNCTION_NAME=test_fentry \
 --build-arg PROGRAM_TYPE=fentry \
 --build-arg BYTECODE_FILENAME=fentry.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/fentry:latest

docker push quay.io/bpfman-bytecode/fentry

docker build \
 --build-arg PROGRAM_NAME=do_unlinkat \
 --build-arg BPF_FUNCTION_NAME=test_fexit \
 --build-arg PROGRAM_TYPE=fexit \
 --build-arg BYTECODE_FILENAME=fentry.bpf.o \
 -f ../../../Containerfile.bytecode \
 ./.output -t quay.io/bpfman-bytecode/fexit:latest

docker push quay.io/bpfman-bytecode/fexit

