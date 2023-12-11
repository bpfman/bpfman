#!/usr/bin/env bash

GIT_SHA=$(git rev-parse HEAD)
GIT_SHORT_SHA=$(git rev-parse --short HEAD)

sed -i "s/GITSHA/${GIT_SHA}/g" bpfman.spec
sed -i "s/GITSHORTSHA/${GIT_SHORT_SHA}/g" bpfman.spec

sed -i -r "s/Release:(\s*)\S+/Release:\1${PACKIT_RPMSPEC_RELEASE}%{?dist}/" bpfman.spec
