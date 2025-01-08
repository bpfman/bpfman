#!/bin/bash
#
# This script adds a network namespace and a veth pair to the default namespace.
# It can be used to test bpfman for the network interface-based eBPF programs
# (XDP, TC, and TCX) on a virtual interface. It can also be used to test the
# netns option for intsalling the programs inside a network namespace.
#
# non-ns load tests:
# bpfman load image --image-url quay.io/bpfman-bytecode/tcx_test:latest -n tcx_pass tcx -d ingress -i bpfman-host -p 20
# bpfman load image --image-url quay.io/bpfman-bytecode/tc_pass:latest -n pass tc -d ingress -i bpfman-host -p 50
# bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest -n pass xdp -i bpfman-host -p 100
#
# netns load tests:
# bpfman load image --image-url quay.io/bpfman-bytecode/tcx_test:latest -n tcx_pass tcx -d ingress -i bpfman-ns -n bpfman-test -p 20
# bpfman load image --image-url quay.io/bpfman-bytecode/tc_pass:latest -n pass tc -d ingress -i bpfman-ns -n bpfman-test -p 50
# bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest -n pass xdp -i bpfman-ns -n bpfman-test -p 100

NS_NAME="bpfman-test"
HOST_VETH="bpfman-host"
NS_VETH="bpfman-ns"
DEFAULT_IP_PREFIX="172.37.39"
IP_MASK="24"
HOST_IP_ID="1"
NS_IP_ID="2"

sudo ip netns add $NS_NAME
sudo ip link add $HOST_VETH type veth peer name $NS_VETH
sudo ip link set $NS_VETH netns $NS_NAME
sudo ip netns exec $NS_NAME ip addr add $DEFAULT_IP_PREFIX.$NS_IP_ID/$IP_MASK dev $NS_VETH
sudo ip addr add $DEFAULT_IP_PREFIX.$HOST_IP_ID/$IP_MASK dev $HOST_VETH
sudo ip netns exec $NS_NAME ip link set dev $NS_VETH up
sudo ip link set dev $HOST_VETH up

