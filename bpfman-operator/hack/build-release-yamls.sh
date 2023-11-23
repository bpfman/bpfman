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

mkdir -p release/

## Location to install dependencies to
LOCALBIN=$(pwd)/bin

## Tool Binaries
KUSTOMIZE=${LOCALBIN}/kustomize

# Generate all install yaml's

## 1. bpfman CRD install

# Make clean files with boilerplate
cat hack/boilerplate.sh.txt > release/bpfman-crds-install-v${VERSION}.yaml
sed -i "s/YEAR/$thisyear/g" release/bpfman-crds-install-v${VERSION}.yaml
cat << EOF >> release/bpfman-crds-install-v${VERSION}.yaml
#
# bpfman Kubernetes API install
#
EOF

for file in `ls config/crd/bases/bpfman*.yaml`
do
    echo "---" >> release/bpfman-crds-install-v${VERSION}.yaml
    echo "#" >> release/bpfman-crds-install-v${VERSION}.yaml
    echo "# $file" >> release/bpfman-crds-install-v${VERSION}.yaml
    echo "#" >> release/bpfman-crds-install-v${VERSION}.yaml
    cat $file >> release/bpfman-crds-install-v${VERSION}.yaml
done

echo "Generated:" release/bpfman-crds-install-v${VERSION}.yaml

## 2. bpfman-operator install yaml

$(cd ./config/bpfman-operator-deployment && ${KUSTOMIZE} edit set image quay.io/bpfman/bpfman-operator=quay.io/bpfman/bpfman-operator:v${VERSION})
${KUSTOMIZE} build ./config/default > release/bpfman-operator-install-v${VERSION}.yaml
### replace configmap :latest images with :v${VERSION}
sed -i "s/quay.io\/bpfman\/bpfman-agent:latest/quay.io\/bpfman\/bpfman-agent:v${VERSION}/g" release/bpfman-operator-install-v${VERSION}.yaml
sed -i "s/quay.io\/bpfman\/bpfman:latest/quay.io\/bpfman\/bpfman:v${VERSION}/g" release/bpfman-operator-install-v${VERSION}.yaml

echo "Generated:" release/bpfman-operator-install-v${VERSION}.yaml

## 3. examples install yamls

### XDP
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-xdp-counter > release/go-xdp-counter-install-v${VERSION}.yaml
echo "Generated:" go-xdp-counter-install-v${VERSION}.yaml
### TC
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-tc-counter > release/go-tc-counter-install-v${VERSION}.yaml
echo "Generated:" go-tc-counter-install-v${VERSION}.yaml
### TRACEPOINT
${KUSTOMIZE} build ../examples/config/v${VERSION}/go-tracepoint-counter > release/go-tracepoint-counter-install-v${VERSION}.yaml
echo "Generated:" go-tracepoint-counter-install-v${VERSION}.yaml

## 4. examples install yamls for OCP
### XDP
${KUSTOMIZE} build ../examples/config/v${VERSION}-ocp/go-xdp-counter > release/go-xdp-counter-install-ocp-v${VERSION}.yaml
echo "Generated:" go-xdp-counter-install-ocp-v${VERSION}.yaml
### TC
${KUSTOMIZE} build ../examples/config/v${VERSION}-ocp/go-tc-counter > release/go-tc-counter-install-ocp-v${VERSION}.yaml
echo "Generated:" go-tc-counter-install-ocp-v${VERSION}.yaml
### TRACEPOINT
${KUSTOMIZE} build ../examples/config/v${VERSION}-ocp/go-tracepoint-counter > release/go-tracepoint-counter-install-ocp-v${VERSION}.yaml
echo "Generated:" go-tracepoint-counter-install-ocp-v${VERSION}.yaml
