#!/bin/bash

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
    golangci-lint run ../bpfman-operator/... ../examples/...
    echo "### Linting bpf c code"
    git ls-files -- '*.c' '*.h' | xargs clang-format --dry-run --Werror
}

lint_all

