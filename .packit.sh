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
if [ -z "${PRE_RELEASE}" ]; then
    echo "Pre-Release unset, removing from Spec"
    sed -i "s/^%global prerelease.*//" bpfman.spec
else
    echo "Overriding Spec Pre-Release to $PRE_RELEASE"
    sed -i "s/^%global prerelease.*/%global prerelease $PRE_RELEASE/" bpfman.spec
fi

if [ "$OVERWRITE_RELEASE" == "true" ]; then
    # Use Packit's supplied variable in the Release field in rpm spec.
    sed -i "s/^Release:.*/Release: $PACKIT_RPMSPEC_RELEASE%{?dist}/" bpfman.spec

    # Ensure last part of the release string is the git shortcommit without a
    # prepended "g"
    sed -i "/^Release: $PACKIT_RPMSPEC_RELEASE%{?dist}/ s/\(.*\)g/\1/" bpfman.spec
fi
