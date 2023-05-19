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

# Make clean files with boilerplate
cat hack/boilerplate.sh.txt > release/bpfd-crds-${VERSION}.yaml
sed -i "s/YEAR/$thisyear/g" release/bpfd-crds-${VERSION}.yaml
cat << EOF >> release/bpfd-crds-${VERSION}.yaml
#
# bpfd Kubernetes API install
#
EOF

for file in `ls config/crd/bases/bpfd*.yaml`
do
    echo "---" >> release/bpfd-crds-${VERSION}.yaml
    echo "#" >> release/bpfd-crds-${VERSION}.yaml
    echo "# $file" >> release/bpfd-crds-${VERSION}.yaml
    echo "#" >> release/bpfd-crds-${VERSION}.yaml
    cat $file >> release/bpfd-crds-${VERSION}.yaml
done

echo "Generated:" release/install.yaml
