#!/bin/bash

readonly GOLANGCI_LINT_VERSION="v2.0.2"

lint_all() {
    echo "### Linting yaml"
    if command -v prettier &>/dev/null; then
        find ../ -type f -name '*.yaml' -print0 | xargs -0 prettier -l
    else
        echo "### prettier could not be found, skipping Yaml lint"
    fi
    echo "### Linting toml"
    if command -v taplo &>/dev/null; then
        taplo fmt --check
    else
        echo "### taplo could not be found, skipping Toml lint"
    fi
    echo "### Linting bash scripts"
    if command -v shellcheck &>/dev/null; then
        shellcheck -e SC2046 -e SC2086 -e SC2034 -e SC2181 -e SC2207 -e SC2002 -e  SC2155 -e SC2128 ./*.sh
    else
        echo "### shellcheck could not be found, skipping shell lint"
    fi
    echo "### Linting rust code"
    cargo +nightly fmt --all -- --check
    cargo +nightly clippy --all -- --deny warnings
    echo "### Linting golang code"
    # See configuration file in .golangci.yml.
    docker run --rm -v $(pwd)/examples:/bpfman/examples -v $(pwd)/clients:/bpfman/clients \
        -v $(pwd)/go.mod:/bpfman/go.mod -v $(pwd)/go.sum:/bpfman/go.sum  \
        -v $(pwd)/.golangci.yaml:/bpfman/.golangci.yaml --security-opt="label=disable" -e GOLANGCI_LINT_CACHE=/cache \
        -w /bpfman "golangci/golangci-lint:$GOLANGCI_LINT_VERSION" golangci-lint run -v --enable=gofmt,typecheck --timeout 5m
    echo "### Linting bpf c code"
    git ls-files -- '*.c' '*.h' | xargs clang-format --dry-run --Werror
}

lint_all

