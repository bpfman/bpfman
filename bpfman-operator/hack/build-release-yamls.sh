#!/usr/bin/env bash

# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

thisyear=`date +"%Y"`

mkdir -p release-v${VERSION}/

## Location to install dependencies to
LOCALBIN=$(pwd)/bin

## Tool Binaries
KUSTOMIZE=${LOCALBIN}/kustomize

# Generate all install yaml's

## 1. bpfman CRD install

# Make clean files with boilerplate
cat hack/boilerplate.sh.txt > release-v${VERSION}/bpfman-crds-install.yaml
sed -i "s/YEAR/$thisyear/g" release-v${VERSION}/bpfman-crds-install.yaml
cat << EOF >> release-v${VERSION}/bpfman-crds-install.yaml
#
# bpfman Kubernetes API install
#
EOF

for file in `ls config/crd/bases/bpfman*.yaml`
do
    echo "---" >> release-v${VERSION}/bpfman-crds-install.yaml
    echo "#" >> release-v${VERSION}/bpfman-crds-install.yaml
    echo "# $file" >> release-v${VERSION}/bpfman-crds-install.yaml
    echo "#" >> release-v${VERSION}/bpfman-crds-install.yaml
    cat $file >> release-v${VERSION}/bpfman-crds-install.yaml
done

echo "Generated:" release-v${VERSION}/bpfman-crds-install.yaml

## 2. bpfman-operator install yaml

$(cd ./config/bpfman-operator-deployment && ${KUSTOMIZE} edit set image quay.io/bpfman/bpfman-operator=quay.io/bpfman/bpfman-operator:v${VERSION})
${KUSTOMIZE} build ./config/default > release-v${VERSION}/bpfman-operator-install.yaml
### replace configmap :latest images with :v${VERSION}
sed -i "s/quay.io\/bpfman\/bpfman-agent:latest/quay.io\/bpfman\/bpfman-agent:v${VERSION}/g" release-v${VERSION}/bpfman-operator-install.yaml
sed -i "s/quay.io\/bpfman\/bpfman:latest/quay.io\/bpfman\/bpfman:v${VERSION}/g" release-v${VERSION}/bpfman-operator-install.yaml

echo "Generated:" release-v${VERSION}/bpfman-operator-install.yaml

## 3. examples install yamls

### XDP
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-xdp-counter > release-v${VERSION}/go-xdp-counter-install.yaml
echo "Generated:" release-v${VERSION}/go-xdp-counter-install.yaml
### TC
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-tc-counter > release-v${VERSION}/go-tc-counter-install.yaml
echo "Generated:" release-v${VERSION}/go-tc-counter-install.yaml
### TRACEPOINT
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-tracepoint-counter > release-v${VERSION}/go-tracepoint-counter-install.yaml
echo "Generated:" release-v${VERSION}/go-tracepoint-counter-install.yaml
### UPROBE
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-uprobe-counter > release-v${VERSION}/go-uprobe-counter-install.yaml
echo "Generated:" release-v${VERSION}/go-uprobe-counter-install.yaml
### URETPROBE
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-uretprobe-counter > release-v${VERSION}/go-uretprobe-counter-install.yaml
echo "Generated:" release-v${VERSION}/go-uretprobe-counter-install.yaml
### KPROBE
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-kprobe-counter > release-v${VERSION}/go-kprobe-counter-install.yaml
echo "Generated:" release-v${VERSION}/go-kprobe-counter-install.yaml

## 4. examples install yamls for SELINUX distros
### XDP
${KUSTOMIZE} build ../examples/config/v${VERSION}-selinux/go-xdp-counter > release-v${VERSION}/go-xdp-counter-install-selinux.yaml
echo "Generated:" release-v${VERSION}/go-xdp-counter-install-selinux.yaml
### TC
${KUSTOMIZE} build ../examples/config/v${VERSION}-selinux/go-tc-counter > release-v${VERSION}/go-tc-counter-install-selinux.yaml
echo "Generated:" release-v${VERSION}/go-tc-counter-install-selinux.yaml
### TRACEPOINT
${KUSTOMIZE} build ../examples/config/v${VERSION}-selinux/go-tracepoint-counter > release-v${VERSION}/go-tracepoint-counter-install-selinux.yaml
echo "Generated:" release-v${VERSION}/go-tracepoint-counter-install-selinux.yaml
### UPROBE
${KUSTOMIZE} build ../examples/config/v${VERSION}-selinux/go-uprobe-counter > release-v${VERSION}/go-uprobe-counter-install-selinux.yaml
echo "Generated:" release-v${VERSION}/go-uprobe-counter-install-selinux.yaml
### URETPROBE
${KUSTOMIZE} build ../examples/config/v${VERSION}-selinux/go-uretprobe-counter > release-v${VERSION}/go-uretprobe-counter-install-selinux.yaml
echo "Generated:" release-v${VERSION}/go-uretprobe-counter-install-selinux.yaml
### KPROBE
${KUSTOMIZE} build ../examples/config/v${VERSION}-selinux/go-kprobe-counter > release-v${VERSION}/go-kprobe-counter-install-selinux.yaml
echo "Generated:" release-v${VERSION}/go-kprobe-counter-install-selinux.yaml
