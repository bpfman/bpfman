#!/usr/bin/env bash

GIT_SHA=$(git rev-parse HEAD)
GIT_SHORT_SHA=$(git rev-parse --short HEAD)
VERSION=$(awk -F' = ' '/^version/ { gsub(/"/, "", $2); print $2; exit; }' Cargo.toml)
BASE_VERSION=$(echo ${VERSION} | awk -F '[-]' '{print ""$1""}')
PRE_RELEASE=$(echo ${VERSION} | awk -F '[-]' '{print ""$2""}')

# Use the git sha from HEAD in the rpm spec
sed -i "s/^%global commit.*/%global commit $GIT_SHA/" bpfman.spec

# Use the short git sha from HEAD in the rpm spec
sed -i "s/^%global shortcommit.*/%global shortcommit $GIT_SHORT_SHA/" bpfman.spec

# Use the correct base version in the rpm spec
sed -i "s/^%global base_version.*/%global base_version $BASE_VERSION/" bpfman.spec

# Use the correct pre-release version in the rpm spec
sed -i "s/^%global prerelease.*/%global prerelease $PRE_RELEASE/" bpfman.spec
