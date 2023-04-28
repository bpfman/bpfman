#!/bin/bash

# Try to determine current directory, then cd to the scripts directory
# so the example yaml is always in the same relative directory.
CALL_POPD=false
if [[ "$PWD" == */bpfd-operator ]]; then
    pushd ../scripts &>/dev/null
    CALL_POPD=true
elif [[ "$PWD" == */go-tc-counter ]]; then
    pushd ../../scripts &>/dev/null
    CALL_POPD=true
elif [[ "$PWD" == */go-tracepoint-counter ]]; then
    pushd ../../scripts &>/dev/null
    CALL_POPD=true
elif [[ "$PWD" == */go-xdp-counter ]]; then
    pushd ../../scripts &>/dev/null
    CALL_POPD=true
elif [[ "$PWD" == */examples ]]; then
    pushd ../scripts &>/dev/null
    CALL_POPD=true
elif [[ "$PWD" == */bpfd ]]; then
    pushd scripts &>/dev/null
    CALL_POPD=true
elif [[ "$PWD" == */scripts ]]; then
    echo "Do nothing"
else
    echo "Not in a known directory. Must be in one of the following to run this script:"
    echo "  bpfd/"
    echo "  bpfd/bpfd-operator"
    echo "  bpfd/examples"
    echo "  bpfd/examples/go-tc-counter"
    echo "  bpfd/examples/go-tracepoint-counter"
    echo "  bpfd/examples/go-xdp-counter"
    echo "  bpfd/scripts"
    exit 1
fi

# Delete the associated Userspace DaemonSet (go-xxx-counter.yaml) with supporting objects (Namespace,
# ServiceAccount, ClusterRoleBinding) and then delete the BPF Program (go-xxx-counter-bytecode.yaml)

# XDP
kubectl delete -f ../examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter.yaml
kubectl delete -f ../examples/go-xdp-counter/kubernetes-deployment/go-xdp-counter-bytecode.yaml
# TC
kubectl delete -f ../examples/go-tc-counter/kubernetes-deployment/go-tc-counter.yaml
kubectl delete -f ../examples/go-tc-counter/kubernetes-deployment/go-tc-counter-bytecode.yaml
# Ttacepoint
kubectl delete -f ../examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter.yaml
kubectl delete -f ../examples/go-tracepoint-counter/kubernetes-deployment/go-tracepoint-counter-bytecode.yaml

if [[ "$CALL_POPD" == true ]]; then
    popd &>/dev/null
fi
