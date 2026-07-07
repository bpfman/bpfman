.DEFAULT_GOAL := all

# ============================================================================
# Variables
# ============================================================================

# ---------------------------------------------------------------------------
# Make helpers for use inside $(if) expansions and tag joining.
# ---------------------------------------------------------------------------
comma := ,
empty :=
space := $(empty) $(empty)
# comma-join turns a space-separated word list into a comma-separated
# string, dropping empty words. Used to compose -tags lists without
# producing stray leading/trailing commas when a contributor (STATIC,
# EXTRA_TAGS) is empty.
comma-join = $(subst $(space),$(comma),$(strip $(1)))

# ---------------------------------------------------------------------------
# Tool versions -- single source of truth for CI and Docker builds.
# ---------------------------------------------------------------------------
FEDORA_VERSION ?= 43
GO_VERSION ?= 1.25
# GOTOOLCHAIN-format pin for the go fix modernisers (see bpfman-gofix).
# The fixers ship with the toolchain, so this version -- not GO_VERSION
# -- decides which modernisers run; bump it deliberately.
GOFIX_GO_VERSION ?= go1.26.4
GOLANGCI_LINT_VERSION ?= v2.11.2
PROTOC_GEN_GO_VERSION ?= v1.36.11
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.0
# protoc (the compiler) is pinned and downloaded into $(BIN_DIR) rather
# than taken from the host/devshell, so generated stubs are reproducible
# regardless of the environment's protobuf version. The value is the
# protocolbuffers/protobuf release tag without the leading "v"; the
# committed stubs were generated with this release (header
# "protoc v6.32.1").
PROTOC_VERSION ?= 32.1

# ---------------------------------------------------------------------------
# Paths.
# ---------------------------------------------------------------------------
BIN_DIR ?= bin
BPFMAN_PROTO_DIR := proto
BPFMAN_PB_DIR := server/pb
DOC_PORT ?= 6060
# Canonical bpfman-shell sources that should be formatter-owned. The
# e2e corpus is the broad runnable script set; outside e2e, include
# individual positive fixtures deliberately. Do not glob
# cmd/bpfman-shell/testdata: it mixes positive fixtures with
# parser/check/runtime negative cases whose exact source shape matters.
# Dockerfile.bpfman is a container build file, and
# contrib/emacs/syntax-gallery.bpfman is deliberately non-canonical.
BPFMAN_SHELL_FORMAT_SOURCES := \
	e2e/lib.bpfman \
	$(wildcard e2e/scripts/*.bpfman) \
	cmd/bpfman-shell/shell/lower/testdata/language.bpfman \
	cmd/bpfman-shell/shell/lower/testdata/language-lib.bpfman

# ---------------------------------------------------------------------------
# Image-building tool (docker / podman). Mirrors the bpfman-operator
# Makefile's convention: detect whichever of docker/podman is on PATH
# (docker wins if both are present, as CI runs on docker), fall back
# to a literal "docker" if neither is, and let the caller override
# with `make OCI_BIN=podman ...`. Exported so helper scripts the
# recipes shell out to pick the same tool up.
#
# Differences from the operator's exact form, both forced on us by
# the canonical multi-arch build flow: `make bpfman-compile` is
# re-entered inside Dockerfile.bpfman's fedora-minimal builder
# stage, which ships neither docker, podman, nor `basename`.
#
#   * stderr is redirected on both `which` legs. The operator only
#     suppresses the first; in fedora-minimal that leaks "which:
#     command not found" from the second probe on every parse.
#   * `$(notdir ...)` rather than `$(shell basename ...)`. With
#     both tools absent OCI_BIN_PATH is empty, and `basename` with
#     no operand prints "missing operand" on every reference;
#     $(notdir) is a make builtin, handles the empty case silently,
#     and never forks a shell.
#   * `$(or ...,docker)` guarantees a non-empty value. Recipes that
#     would otherwise expand to `" build -t ..."` (and confuse sh
#     with an empty argv[0]) now expand to `"docker build -t ..."`
#     and fail with the obvious "docker: command not found" if the
#     binary truly isn't there.
# ---------------------------------------------------------------------------
OCI_BIN_PATH := $(shell which docker 2>/dev/null || which podman 2>/dev/null)
OCI_BIN ?= $(or $(notdir $(OCI_BIN_PATH)),docker)
export OCI_BIN

# ---------------------------------------------------------------------------
# Image names and deployment knobs.
# ---------------------------------------------------------------------------
# BPFMAN_IMG matches the variable name used by the upstream Rust
# bpfman repository and the bpfman-operator: a single full pullspec
# (registry/repository:tag) rather than separate name + tag knobs.
# To override, pass the entire ref, e.g.
#   make build-image BPFMAN_IMG=ttl.sh/me/bpfman-test:debug
BPFMAN_IMG ?= quay.io/bpfman/bpfman:latest
CSI_SANITY_IMG ?= csi-sanity:latest

# ---------------------------------------------------------------------------
# CI build environment knobs. The `ci-*` make targets drive a
# Fedora-based docker image that mirrors what the GH workflows
# run, so a developer can reproduce CI locally with `make ci`.
# ---------------------------------------------------------------------------
CI_IMAGE       ?= bpfman-ci
CI_DOCKERFILE  ?= Dockerfile.ci
CI_E2E_BUNDLE  ?= ./ci-e2e-bundle

# Refuse to proceed if CI_E2E_BUNDLE is empty or a path that
# would `rm -rf` something catastrophic. The ci-test-e2e and
# clean recipes both $(RM) -r this directory; an unguarded
# `make clean CI_E2E_BUNDLE=.` would shred the source tree.
ifeq (,$(strip $(CI_E2E_BUNDLE)))
$(error CI_E2E_BUNDLE is empty)
endif
ifneq (,$(filter . ./ .. ../ /,$(strip $(CI_E2E_BUNDLE))))
$(error CI_E2E_BUNDLE=$(CI_E2E_BUNDLE) is unsafe (would remove source tree or filesystem root))
endif

# Caller-supplied buildx flags appended to the buildx-driven
# ci-* recipes. Empty by default for local invocations; CI sets
#   CI_BUILDX_CACHE=--cache-from type=gha,scope=ci --cache-to type=gha,mode=max,scope=ci
# (typically via `env:` in the workflow YAML) so the buildkit
# layer cache is shared across all CI jobs.
CI_BUILDX_CACHE ?=

# Shared docker-run incantation for the ci-* targets that drive
# work inside the CI container. Mounts the source tree and named
# volumes for Go's build and module caches so consecutive runs
# benefit from incremental compile.
CI_RUN := $(OCI_BIN) run --rm \
	-v $(CURDIR):/src -w /src \
	-v bpfman-ci-go-build:/root/.cache/go-build \
	-v bpfman-ci-go-mod:/root/go/pkg/mod \
	$(CI_IMAGE)

# ---------------------------------------------------------------------------
# Test knobs.
# ---------------------------------------------------------------------------
PARALLEL ?=
# Optional regex passed to `-test.run` in test-e2e / test-e2e-scripts
# to narrow which tests execute. Empty by default = run all.
TEST ?=
# Iteration count threaded into `-test.count` for `make test-e2e`.
# Default 1 keeps the local loop fast; CI pins this to 5 so every PR
# runs a small count loop on top of the deterministic gate.
STRESS_COUNT ?= 1

# Knobs the parallel gRPC e2e test honours. Declared here (empty
# by default) so checkmake's --warn-undefined-variables lane
# doesn't trip on the $(if $(VAR),VAR=$(VAR)) idiom that forwards
# them through sudo in run-e2e-grpc. The test binary itself
# carries the actual defaults; leaving them empty
# at the make layer means run-e2e-grpc forwards nothing and the
# binary uses its own defaults.
BPFMAN_GRPC_GOROUTINES ?=
BPFMAN_GRPC_ITERATIONS ?=
BPFMAN_GRPC_PROGRESS_INTERVAL ?=
# Forwarded to the e2e test binary (test-e2e) and to the daemon
# subprocess spawned by test-e2e-grpc. See the logging package's
# component-level spec format (e.g. info,lock=debug,store=debug).
BPFMAN_LOG ?=
# SQLite tuning knobs forwarded to the daemon. See
# platform/store/sqlite/doc.go for the full descriptions. Empty
# leaves the daemon on its package-level defaults; CI uses these
# to widen tolerance on the RACE=1 grpc lane where the race
# detector roughly doubles per-tx wall time and the default 5s
# busy_timeout becomes too tight.
BPFMAN_SQLITE_BUSY_TIMEOUT ?=
BPFMAN_SQLITE_TX_RETRY_BACKOFFS ?=
# Global writer-lock acquire deadline forwarded to the daemon
# via the Kong env-tag binding on the --lock-timeout flag. Empty
# leaves the daemon on its 30s default; the RACE=1 grpc lane
# widens this in CI alongside the SQLite knobs above because the
# race detector pushes worst-case flock waits past 30s.
BPFMAN_LOCK_TIMEOUT ?=
# Knobs the Go-based .bpfman script runner (test-e2e-scripts-go)
# honours. Declared empty so checkmake's
# --warn-undefined-variables doesn't trip on the
# $(if $(VAR),VAR=$(VAR)) forwarding idiom in run-e2e-scripts-go.
# BPFMAN_E2E_SCRIPT_TIMEOUT widens the per-script deadline. The test
# binary carries the actual defaults.
BPFMAN_E2E_BYTECODE_SOURCE ?=
BPFMAN_E2E_IMAGE_REGISTRY ?=
BPFMAN_E2E_POLICY_RULE_PREF ?=
BPFMAN_E2E_SCRIPT_SELECTOR ?=
BPFMAN_E2E_SCRIPT_TIMEOUT ?=
BPFMAN_E2E_SCRIPT_REPEATS ?=
BPFMAN_E2E_SCRIPT_STRESS_REPEATS ?= 16
BPFMAN_E2E_SCRIPT_STRESS_PARALLEL ?= 128

# ---------------------------------------------------------------------------
# Verbose-build switch, modelled on the Linux kernel tree's V=
# convention. Quiet by default (one short tag per recipe, e.g.
# `  CLANG-BPF dispatcher/bpf/tc_dispatcher.bpf.o`); `make V=1`
# restores the full command lines for debugging. Used in the BPF
# compile rules; other recipes (go fmt / vet / build) print their
# own progress and are left as-is.
# ---------------------------------------------------------------------------
V ?=
ifeq ($(V),1)
Q :=
quiet_cmd = @:
else
Q := @
quiet_cmd = @printf "  %-9s %s\n" "$(1)" "$(2)"
endif

# ---------------------------------------------------------------------------
# CGO is always on for this repo: internal/bpfman/ns ships a C constructor
# (nsexec.c) that has to be linked into every binary that imports the
# package, and the project policy is to assume cgo is available.
# Export so every recipe inherits it and individual go invocations no
# longer need to repeat CGO_ENABLED=1. Linker mode is deliberately not
# pinned here: with CGO_ENABLED=1, the Go toolchain auto-picks
# external linking when a binary actually links C and internal linking
# otherwise, which is the right behaviour; pinning
# GO_EXTLINK_ENABLED=1 unconditionally would emit "loadinternal:
# cannot find runtime/cgo" warnings on pure-Go test binaries.
# ---------------------------------------------------------------------------
export CGO_ENABLED := 1

# ---------------------------------------------------------------------------
# Static linking is opt-in via STATIC=1. Any other value disables it.
# The upstream container image enables it because the runtime base is
# scratch, which ships no libc; downstream consumers building with a
# FIPS Go toolchain (Red Hat go-toolset, Microsoft Go FIPS) must leave
# it off, since FIPS crypto requires dynamic linkage to a validated
# OpenSSL.
#
# Normalisation is required because Make's $(if cond,...) treats any
# non-empty string as true, so without the filter below STATIC=0 would
# enable static linking. `override` is required because command-line
# assignments (make STATIC=0) otherwise win over file-level ones. The
# `?=` gives STATIC an empty default so `make --warn-undefined-variables`
# does not flag the $(STATIC) reference on the next line when STATIC
# is not set in the environment or on the command line.
# ---------------------------------------------------------------------------
STATIC ?=
override STATIC := $(filter 1,$(STATIC))

# RACE=1 enables the race detector for unit, e2e test binaries, and
# bpfman-compile. Default off: race overhead can mask
# the kernel-timing behaviour e2e tests aim to surface, and on the
# static-glibc devshell -race forces external linkage. Empty default
# (rather than unset) keeps `make --warn-undefined-variables` quiet.
RACE ?=
override RACE := $(filter 1,$(RACE))

# BPFMAN_E2E_ISOLATED_RUNTIME=1 is forwarded into the e2e sudo
# command line, switching the suite from its production-shaped
# default (one bpffs mount, one sqlite store, one manager instance
# shared across tests) to per-test isolated runtimes (each test
# gets its own). Use the isolated lane when chasing a specific
# feature where orthogonal cross-test contention would muddy
# attribution; CI exercises both lanes, so the default just decides
# which one a developer hits first when they type `make test-e2e`.
# The Go side checks for the literal string "1", so any other value
# collapses to empty here and matches the env-unset (= shared)
# behaviour. The Make variable name is intentionally the same as
# the env var it controls so the forward-env macro can pick it up
# uniformly. Same filter-1 pattern as RACE/STATIC.
BPFMAN_E2E_ISOLATED_RUNTIME ?=
override BPFMAN_E2E_ISOLATED_RUNTIME := $(filter 1,$(BPFMAN_E2E_ISOLATED_RUNTIME))

# forward-env renders `VAR='value'` for each VAR in $(1) that has
# a non-empty value in the current Make environment. Suitable for
# prepending to a sudo command line where sudo strips arbitrary
# env by default and the recipe needs to re-inject a controlled
# set. The Make variable name must match the env variable name --
# adding a new env knob means picking a Make var name that aligns
# (BPFMAN_FOO=$(BPFMAN_FOO), not FOO=$(BPFMAN_FOO)) and appending
# it to the recipe's per-recipe forward list.
forward-env = $(foreach v,$(1),$(if $($(v)),$(v)='$($(v))'))

# ---------------------------------------------------------------------------
# Runtime image dispatch.
#
# Canonical (build-image): a static binary has no runtime libc
# dependency, so it can ship on ubi9-minimal (RHEL CVE feed,
# OpenShift ecosystem). A dynamic binary needs a runtime whose
# glibc matches the Fedora builder, so it ships on fedora-minimal
# pinned to the same FEDORA_VERSION. Override RUNTIME_IMAGE on the
# command line to pin a specific tag or digest.
#
# Dev (build-image-dev): always fedora-minimal at the same
# FEDORA_VERSION, regardless of STATIC. The dev image exists for
# live in-cluster debuggability -- microdnf install whatever you
# need at the moment -- and Fedora's package set is the broadest
# minimal-distro option for ad-hoc tooling. STATIC still controls
# the host build's linkage; only the runtime base is pinned.
# ---------------------------------------------------------------------------
RUNTIME_IMAGE     ?= $(if $(STATIC),registry.access.redhat.com/ubi9/ubi-minimal:latest,registry.fedoraproject.org/fedora-minimal:$(FEDORA_VERSION))
DEV_RUNTIME_IMAGE ?= registry.fedoraproject.org/fedora-minimal:$(FEDORA_VERSION)

# ---------------------------------------------------------------------------
# Version information injected at build time.
# ---------------------------------------------------------------------------
VERSION_PKG := github.com/bpfman/bpfman/version
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
GIT_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
GIT_STATE ?= $(shell if git diff --quiet 2>/dev/null; then echo clean; else echo dirty; fi)
# Captured once so every reference returns the same timestamp. ?=
# would have been recursively-expanded and re-run `date` per use.
ifndef BUILD_DATE
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
endif
GIT_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)

# ---------------------------------------------------------------------------
# Caller-tunable go build / go test / go ldflags.
# ---------------------------------------------------------------------------
# Extra flags appended to `go build` and `go test` recipes
# (e.g. EXTRA_GOFLAGS=-a to force a from-scratch rebuild).
EXTRA_GOFLAGS ?=

# Caller-supplied additional ldflags. Empty by default so local
# development still produces unstripped binaries with full symbol
# information for debugging; CI publish overrides this with -s -w
# to drop the symbol table and DWARF sections from shipped images.
EXTRA_GO_LDFLAGS ?=

# STAMP turns on version stamping (-X flags carrying git commit,
# branch, state, build date) on the bpfman and bpfman-shell
# binaries. Default off because the stamps invalidate Go's link
# cache on every invocation (timestamps and dirty-state change),
# which is wasted work for local development. CI sets STAMP=1 so
# released binaries report their provenance via `bpfman version`.
STAMP ?=

# ---------------------------------------------------------------------------
# Image attestation metadata, baked into the binary via -ldflags so
# `bpfman version` can print a ready-to-pipe `cosign verify` command
# for the image this binary was published from. All three default
# to empty: local `make build`, the host-build path via
# Dockerfile.bpfman.dev, and downstream Konflux/RHEL/UBI builds
# leave them unset, and the version printer omits the Attestation
# line entirely when any of them is empty. Only the CI image-build
# workflow (.github/workflows/image-build.yml) populates them.
# ---------------------------------------------------------------------------
IMAGE_REF       ?=
SIGNER_IDENTITY ?=
OIDC_ISSUER     ?=

# ---------------------------------------------------------------------------
# Derived: GO_LDFLAGS composes STATIC, version stamping, image
# attestation, and EXTRA_GO_LDFLAGS. Must be defined after all of
# those so `:=` captures their final values.
#
# TEST_LDFLAGS is the subset relevant to test linking: only the
# static-link mode. Version stamps change on every invocation
# (timestamps, git state) and would force Go's test cache to
# relink every binary. Tests do not read the stamped values, so
# dropping them keeps `make test` fast.
# ---------------------------------------------------------------------------
TEST_LDFLAGS := $(strip \
    $(if $(STATIC),-linkmode=external -extldflags '-static') \
    $(EXTRA_GO_LDFLAGS))

GO_LDFLAGS := $(strip \
    $(TEST_LDFLAGS) \
    -X $(VERSION_PKG).gitCommit=$(GIT_COMMIT) \
    -X $(VERSION_PKG).gitBranch=$(GIT_BRANCH) \
    -X $(VERSION_PKG).gitState=$(GIT_STATE) \
    -X $(VERSION_PKG).buildDate=$(BUILD_DATE) \
    -X $(VERSION_PKG).version=$(GIT_VERSION) \
    $(if $(IMAGE_REF),-X $(VERSION_PKG).imageRef=$(IMAGE_REF)) \
    $(if $(SIGNER_IDENTITY),-X $(VERSION_PKG).signerIdentity=$(SIGNER_IDENTITY)) \
    $(if $(OIDC_ISSUER),-X $(VERSION_PKG).oidcIssuer=$(OIDC_ISSUER)))

# BIN_LDFLAGS is what the bpfman and bpfman-shell build recipes
# pass to `go build`. STAMP=1 selects the full GO_LDFLAGS (with
# version stamps); otherwise it falls back to the unstamped
# TEST_LDFLAGS. CI invokes ci-build with STAMP=1 so shipped
# binaries carry their provenance; local `make` defaults to
# unstamped binaries that hit the link cache on every rebuild.
BIN_LDFLAGS := $(if $(STAMP),$(GO_LDFLAGS),$(TEST_LDFLAGS))

# ---------------------------------------------------------------------------
# Build tags.
# ---------------------------------------------------------------------------
STATIC_TAGS := osusergo,netgo
EXTRA_TAGS ?=
# mattn/go-sqlite3 is the only SQLite driver, so every build links the
# embedded SQLite amalgamation through cgo. sqlite_omit_load_extension
# compiles it with -DSQLITE_OMIT_LOAD_EXTENSION, dropping the
# sqlite3_load_extension() API and its unixDlOpen() wrapper -- the sole
# reason the static linker emits "Using 'dlopen' in statically linked
# applications requires at runtime the shared libraries from the glibc
# version used for linking". We never load runtime SQL extensions, so
# omitting them is pure subtraction; the tag is always on.
SQLITE_TAGS := sqlite_omit_load_extension
# Tag sets consumed by each go build/test recipe. EXTRA_TAGS is
# appended to every set so callers can add a tag once and have every
# build path pick it up.
BUILD_TAGS     := $(call comma-join,$(if $(STATIC),$(STATIC_TAGS)) $(SQLITE_TAGS) $(EXTRA_TAGS))
TEST_TAGS      := $(BUILD_TAGS)
E2E_TAGS       := $(call comma-join,e2e $(if $(STATIC),$(STATIC_TAGS)) $(SQLITE_TAGS) $(EXTRA_TAGS))
BPFMAN_NS_TAGS := $(call comma-join,bpfman_ns $(SQLITE_TAGS) $(EXTRA_TAGS))

# ---------------------------------------------------------------------------
# bpfman-ns transport cross-architecture tests.
# ---------------------------------------------------------------------------
BPFMAN_NS_ARCHES ?= amd64 arm64 ppc64le s390x
BPFMAN_NS_TEST_BIN ?= $(BIN_DIR)/bpfman-ns.test

# ---------------------------------------------------------------------------
# BPF build path.
#
# Build all BPF programs (dispatchers + e2e testdata) using the host
# toolchain: clang + libbpf headers + Linux UAPI headers. The Nix
# devShell provides these via clang-unwrapped + libbpf + linuxHeaders
# (see flake.nix); `hack/install-fedora-deps.sh` installs the
# equivalent Fedora RPMs (clang, llvm, libbpf-devel, kernel-headers,
# pkgconf-pkg-config). On stock Ubuntu CI runners, apt-get installs
# the equivalents (clang, llvm, libbpf-dev, linux-libc-dev,
# pkg-config).
# ---------------------------------------------------------------------------

# Shared compile setup. LIBBPF_CFLAGS comes from pkg-config so the
# include path follows the libbpf-devel package; BPF_CFLAGS is a
# caller knob (Ubuntu CI passes -I/usr/include/<DEB_HOST_MULTIARCH>
# so clang in -target bpfel mode finds asm/types.h under the
# multiarch include path).
#
# `=` (deferred) rather than `:=` (immediate) so pkg-config only
# fires when a recipe actually references LIBBPF_CFLAGS; an
# immediate evaluation would emit a spurious "Package 'libbpf'
# not found" pkg-config warning in environments that run make
# without libbpf-devel installed.
LIBBPF_CFLAGS = $(shell pkg-config --cflags libbpf)
BPF_CFLAGS ?=

# clang -target bpfel produces architecture-independent BPF
# bytecode, but kernel UAPI headers it pulls in (asm/types.h and
# friends) are arch-specific. Define __TARGET_ARCH_<arch> to match
# the host so the right asm/ headers are used.
HOST_ARCH ?= $(shell uname -m)
ifeq ($(HOST_ARCH),x86_64)
    BPF_TARGET_ARCH := -D__TARGET_ARCH_x86
else ifeq ($(HOST_ARCH),i686)
    BPF_TARGET_ARCH := -D__TARGET_ARCH_x86
else ifeq ($(HOST_ARCH),aarch64)
    BPF_TARGET_ARCH := -D__TARGET_ARCH_arm64
else ifeq ($(HOST_ARCH),ppc64le)
    BPF_TARGET_ARCH := -D__TARGET_ARCH_powerpc
else ifeq ($(HOST_ARCH),powerpc64le)
    BPF_TARGET_ARCH := -D__TARGET_ARCH_powerpc
else ifeq ($(HOST_ARCH),s390x)
    BPF_TARGET_ARCH := -D__TARGET_ARCH_s390
else
    $(error unsupported HOST_ARCH=$(HOST_ARCH))
endif

# Dispatcher BPF: sources live in dispatcher/bpf/; the dispatcher
# Go package's go:embed directives read .bpf.o files at the package
# root (dispatcher/), so the compile rule below targets that path
# directly -- no intermediate copy needed. xdp_dispatcher_v1.bpf.c
# is reference-only and excluded from the build.
DISPATCHER_BPF_SOURCES := $(filter-out dispatcher/bpf/xdp_dispatcher_v1.bpf.c,$(wildcard dispatcher/bpf/*.bpf.c))
DISPATCHER_BPF_EMBEDS  := $(addprefix dispatcher/,$(notdir $(DISPATCHER_BPF_SOURCES:.bpf.c=.bpf.o)))
DISPATCHER_BPF_DEPS    := $(DISPATCHER_BPF_EMBEDS:.bpf.o=.bpf.d)

# E2E testdata BPF: sources, headers, and compiled outputs all
# live in e2e/testdata/bpf/. Both e2e.test and e2e-grpc.test open
# the .bpf.o tree off disk at run time via e2e.BytecodeDir, so the
# build system points them at this directory (BPFMAN_E2E_BYTECODE_DIR)
# rather than baking the objects into the binaries.
E2E_BPF_SOURCES := $(wildcard e2e/testdata/bpf/*.bpf.c)
E2E_BPF_OBJECTS := $(E2E_BPF_SOURCES:.bpf.c=.bpf.o)
E2E_BPF_DEPS    := $(E2E_BPF_SOURCES:.bpf.c=.bpf.d)

# platform/ebpf BPF: the package's tests read xdp_pass.bpf.o off
# disk relative to the package dir (go test's cwd), so the compile
# rule emits the object straight into platform/ebpf/ alongside the
# test source. Source still lives under e2e/testdata/bpf/ -- the BPF
# program is shared between the unit tests and the e2e suite, and
# duplicating the .bpf.c would create a divergence risk.
PLATFORM_EBPF_BPF_OBJECTS := platform/ebpf/xdp_pass.bpf.o
PLATFORM_EBPF_BPF_DEPS    := $(PLATFORM_EBPF_BPF_OBJECTS:.bpf.o=.bpf.d)

# E2E kmod: leased kernel-function slots used as deterministic fentry/fexit
# and kprobe/kretprobe targets. The plain kbuild target defaults to the conventional
# Fedora/Ubuntu kernel build tree; override KDIR for other layouts.
# On NixOS, the helper tries to derive the matching kernel.dev output
# from /run/current-system/kernel when /lib/modules/.../build is absent.
E2E_KMOD_DIR          := e2e/kmod
E2E_KMOD              := $(E2E_KMOD_DIR)/bpfman_e2e_targets.ko
KERNEL_RELEASE        ?= $(shell uname -r)
KERNEL_MOD_DIR_VERSION ?= $(KERNEL_RELEASE)
KDIR                  ?= /lib/modules/$(KERNEL_RELEASE)/build
KERNEL_DEV            ?=
E2E_KMOD_KBUILD       ?= $(E2E_KMOD_DIR)/.kbuild
E2E_KMOD_PREPARE_KDIR := $(E2E_KMOD_DIR)/prepare-kdir.sh
# Records the kernel release the current .ko was built against. A
# kmod is only valid for the kernel it was compiled and BTF-encoded
# against; kbuild's incremental logic keys off source timestamps and
# would happily reuse a stale .ko across a reboot into a different
# kernel (the BTF [M] step only re-runs on a relink). e2e-kmod-build
# compares this stamp to KERNEL_RELEASE and cleans first when they
# differ, so a kernel change forces a full rebuild.
E2E_KMOD_STAMP        := $(E2E_KMOD_DIR)/.kmod-kernel-stamp

# ---------------------------------------------------------------------------
# Multi-arch buildx knobs.
#
# STATIC linkage is intentionally NOT a knob here. The Dockerfile
# hardcodes `make bpfman-compile STATIC=1` because the final stage is
# `scratch` and the two are coupled: a dynamically linked binary
# would crash immediately on a libc-less base. If you need a
# non-static binary (FIPS Go toolchains, dynamic-glibc bases), build
# on the host and package via Dockerfile.bpfman.dev instead.
#
# Multi-platform builds require a docker-container or remote buildx
# builder. CI workflows use docker/setup-buildx-action which
# provisions one automatically; locally, run `docker buildx create
# --driver docker-container --use` once.
# ---------------------------------------------------------------------------
PLATFORMS               ?=
PUSH                    ?=
BUILDX_EXTRA_ARGS       ?=
# Caller-supplied extra args passed last to the plain `docker build`
# targets (build-image-dev, build-image-csi-sanity). Positioned
# just before the build context so caller flags override any
# preceding hard-coded flags that buildx/docker treats as
# last-wins.
EXTRA_DOCKER_BUILD_ARGS ?=
# Selects which Dockerfile the buildx targets use. Defaults to the
# in-tree Dockerfile.bpfman; override to test an alternative
# dockerfile without editing the recipe.
BPFMAN_DOCKERFILE ?= Dockerfile.bpfman

# True (1) when $(OCI_BIN) is podman (either OCI_BIN=podman, or
# OCI_BIN=docker where docker is the podman compat shim). Buildah /
# podman does not honor the per-Dockerfile <dockerfile>.dockerignore
# convention that buildkit reads automatically, so when running
# under podman the build-image-dev recipe passes --ignorefile
# explicitly to point at the per-file dockerignore. Buildkit-
# backed `docker` does not need this and ignores --ignorefile
# (it is a buildah/podman flag), so detection is required.
#
# Safe to leave unguarded: $(OCI_BIN) is guaranteed non-empty (the
# "|| docker" fallback above), so the worst case is `docker
# --version 2>&1` inside a container where docker isn't installed,
# which produces "docker: command not found" and grep -c returns 0.
OCI_BIN_IS_PODMAN := $(shell $(OCI_BIN) --version 2>&1 | grep -ci podman)
# Optional path for buildx --metadata-file. When set, buildx writes
# the published index digest to this path after the push completes,
# and the cosign-sign target reads the digest from it. Empty by
# default; CI sets it to ${RUNNER_TEMP}/buildx-meta.json. Locally,
# any writable path works.
BUILDX_METADATA_FILE ?=

# Output-flag selection. Truth table:
#
#   PUSH=1                            -> --push
#   PLATFORMS contains a comma        -> no flag (cache-only)
#   otherwise                         -> --load
BUILDX_OUTPUT := $(if $(PUSH),--push,$(if $(findstring $(comma),$(PLATFORMS)),,--load))

# Provenance and SBOM attestations are only meaningful when pushing
# to a registry: the Docker image store strips OCI attestations on
# --load, and a cache-only build never produces an artifact to
# attest. Gating on PUSH avoids confusing buildx warnings.
BUILDX_ATTEST := $(if $(PUSH),--provenance=mode=max --sbom=true)

# ---------------------------------------------------------------------------
# Lint target lists.
#
# LINT_MAKE_TARGETS is the bundle that `make lint-make` runs under
# --warn-undefined-variables. Variables referenced only inside a
# recipe are deferred-expansion: a warning only fires once the recipe
# is selected for execution, so the bundle must exercise every recipe
# that pulls a caller-tunable variable (TEST, PARALLEL, PLATFORMS,
# EXTRA_*, etc.) for those references to get probed.
# ---------------------------------------------------------------------------
LINT_MAKE_TARGETS := \
	help \
	test test-e2e test-e2e-scripts \
	test-bpfman-ns test-bpfman-ns-amd64 test-bpfman-ns-arm64 test-bpfman-ns-cross \
	bpfman-compile \
	build-image build-image-amd64 build-image-dev \
	build-image-csi-sanity \
	ci-build ci-check-fmt ci-check-goimports ci-check-vendor ci-check-vet ci-image ci-lint ci-test ci-test-e2e ci-test-e2e-grpc ci-test-e2e-scripts \
	cosign-sign clean

# Lint every Dockerfile / Containerfile with hadolint. The existing
# `# hadolint ignore=...` pragmas in the repo are already set up
# for this tool; adding the target wires it into CI.
LINT_DOCKERFILES := \
	Dockerfile.bpfman.dev \
	Dockerfile.bpfman \
	Dockerfile.ci \
	Dockerfile.csi-sanity


# ============================================================================
# Targets
# ============================================================================

# ---------------------------------------------------------------------------
# Meta: default target, help, clean, version prints, bin directory.
# ---------------------------------------------------------------------------
.PHONY: all
all: bpfman-build bpfman-shell-build bpfman-e2e-cleanup-build

# Alias so 'make build-all' works as advertised in 'make help'.
# 'all' stays the canonical default-target name; 'build-all' is
# the spelling the help text and tab-completion expose.
.PHONY: build-all
build-all: all

.PHONY: help
help:
	@echo "Build:"
	@echo "  build-all                   Build all binaries"
	@echo "  clean                       Remove all build artifacts"
	@echo "  clean-mrproper              Like 'clean', plus wipe Go's shared build/test/fuzz caches (~/.cache/go-build); affects all Go projects on this machine"
	@echo ""
	@echo "Testing:"
	@printf "  %-31s %s\n" "test" "Run all tests"
	@printf "  %-31s %s\n" "test-all" "Run every host-side test surface in CI order (pre-push gate)"
	@printf "  %-31s %s\n" "bpfman-shell-fmt" "Format canonical .bpfman files"
	@printf "  %-31s %s\n" "bpfman-clang-format" "Format C sources with clang-format (LLVM style)"
	@printf "  %-31s %s\n" "update-lowered-goldens" "Regenerate the lowerer golden fixture"
	@printf "  %-31s %s\n" "test-e2e" "Run e2e tests (requires root)"
	@printf "  %-31s %s\n" "test-e2e-grpc" "Run the parallel gRPC e2e test against a real bpfman serve daemon (requires root)"
	@printf "  %-31s %s\n" "test-e2e-scripts" "Run .bpfman e2e scripts under e2e/scripts/ via the Go test binary in e2e/scriptrunner (requires root)"
	@printf "  %-31s %s\n" "test-e2e-scripts-file" "Run .bpfman e2e scripts in file-bytecode mode (requires root)"
	@printf "  %-31s %s\n" "test-e2e-scripts-image" "Run .bpfman e2e scripts in image-bytecode mode (requires root)"
	@printf "  %-31s %s\n" "test-e2e-scripts-image-ci" "Run the CI-shaped image-bytecode script suite (requires root)"
	@printf "  %-31s %s\n" "test-e2e-published-images" "Run published-image .bpfman scripts against quay.io (requires root and network)"
	@printf "  %-31s %s\n" "test-e2e-scripts-stress" "Run .bpfman e2e scripts with high repeat/parallel defaults (requires root)"
	@printf "  %-31s %s\n" "e2e-kmod-force-reload" "Delete managed bpfman state, then rebuild and reload the e2e kmod (requires root)"
	@printf "  %-31s %s\n" "test-bpfman-ns" "Run bpfman-ns transport tests (native amd64)"
	@printf "  %-31s %s\n" "test-bpfman-ns-cross" "Run bpfman-ns transport tests on amd64/arm64/ppc64le/s390x"
	@printf "  %-31s %s\n" "test-bpfman-ns-{arch}" "Run bpfman-ns transport tests for a single architecture"
	@printf "  %-31s %s\n" "lint" "Run golangci-lint"
	@echo ""
	@echo "Local CI reproducer (Dockerfile.ci):"
	@echo "  ci                          Run every ci-* target"
	@echo "  ci-build                    Compile bpfman binary inside the CI container"
	@echo "  ci-check-fmt                Verify Go formatting is tidy (matches CI check-fmt)"
	@echo "  ci-check-goimports          Verify Go imports are tidy (matches CI check-goimports)"
	@echo "  ci-check-vendor             Verify go.mod and vendor are tidy (matches CI check-vendor)"
	@echo "  ci-check-vet                Run go vet over every build-tag combo (matches CI check-vet)"
	@echo "  ci-image                    Build the CI base image (loaded as bpfman-ci)"
	@echo "  ci-lint                     Run \`make lint\` inside the CI container"
	@echo "  ci-test                     Run unit tests inside the CI container"
	@echo "  ci-test-e2e                 Extract e2e test bundle and run it on the host (sudo)"
	@echo "  ci-test-e2e-grpc            Extract bundle to source tree and run the parallel gRPC test (sudo)"
	@echo "  ci-test-e2e-scripts         Extract bundle to source tree and run .bpfman scripts (sudo)"
	@echo ""
	@echo "bpfman (with integrated CSI):"
	@echo "  bpfman-build                Build bpfman binary"
	@echo "  bpfman-compile              Compile bpfman (no fmt/vet/dispatchers)"
	@echo "  clean-bpfman                Remove generated files and binary"
	@echo "  bpfman-proto                Generate protobuf/gRPC stubs"
	@echo ""
	@echo "Container images:"
	@echo "  build-image                 Cross-compile current-arch image via Fedora Dockerfile (canonical pipeline)"
	@echo "  build-image-{arch}          Cross-compile single-arch image (arch in amd64/arm64/ppc64le/s390x)"
	@echo "  build-image-csi-sanity      Build csi-sanity container image"
	@echo "  build-image-dev             Build current-arch image from host-built binary (fast dev iteration)"
	@echo "  build-image-nix             Pure-Nix OCI image (no Docker daemon at build time; debug toolkit baked in)"
	@echo "  cosign-sign                 Sign a published image (requires BUILDX_METADATA_FILE)"
	@echo ""
	@echo "Documentation:"
	@echo "  doc                         Start pkgsite documentation server"
	@echo "  doc-text                    Print API documentation to stdout"
	@echo ""
	@echo "BPF:"
	@echo "  clean-bpf                   Remove BPF build artefacts"
	@echo "  (no bpf-build target -- consumers depend directly on .bpf.o outputs)"
	@echo ""
	@echo "E2E kmod:"
	@echo "  e2e-kmod-build              Build leased kernel-function target module via kbuild (override KDIR=... or KERNEL_DEV=...)"
	@echo "  e2e-kmod-insmod             Load the built module into the running kernel (idempotent; sudos internally)"
	@echo "  e2e-kmod-rmmod              Unload the module from the running kernel (idempotent; sudos internally)"
	@echo "  e2e-kmod-reload             Rebuild and reload the module; ensures the running .ko matches the latest source (sudos internally)"
	@echo "  e2e-kmod-force-reload       Delete managed bpfman state, apply e2e cleanup, then rebuild and reload the module (sudos internally)"
	@echo "  clean-e2e-kmod              Remove e2e kmod build artefacts and result symlink"
	@echo ""
	@echo "SQLite driver: mattn/go-sqlite3 (cgo; the only driver)."

.PHONY: print-go-version
print-go-version:
	@echo $(GO_VERSION)

.PHONY: print-fedora-version
print-fedora-version:
	@echo $(FEDORA_VERSION)

.PHONY: print-golangci-lint-version
print-golangci-lint-version:
	@echo $(GOLANGCI_LINT_VERSION)

.PHONY: clean
clean: clean-bpfman clean-bpfman-shell clean-bpfman-e2e-cleanup clean-bpf clean-e2e-kmod
	$(RM) -r $(BIN_DIR) $(CI_E2E_BUNDLE)

# Nuclear option, modeled on `make mrproper` in the kernel tree:
# wipe local build artifacts AND Go's shared caches under
# ~/.cache/go-build. Useful when chasing cache-coherence bugs whose
# inputs aren't in cmd/go's action key (environment variables the
# toolchain reads but does not hash). Affects every Go project
# sharing this user's cache, not
# just this checkout. The module cache is intentionally NOT wiped:
# `go clean -modcache` forces a full re-download on the next build.
.PHONY: clean-mrproper
clean-mrproper: clean
	go clean -cache -testcache -fuzzcache

# Ensure bin directory exists
$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

# ---------------------------------------------------------------------------
# Lint.
# ---------------------------------------------------------------------------
# Uber lint target: run every language-specific linter in turn.
# Keep each sub-target independently runnable so contributors can
# iterate on one layer at a time.
.PHONY: lint
lint: lint-go lint-make lint-hack lint-dockerfile

.PHONY: lint-go
lint-go: $(DISPATCHER_BPF_EMBEDS) $(BIN_DIR)/golangci-lint
	$(BIN_DIR)/golangci-lint run

$(BIN_DIR)/golangci-lint: | $(BIN_DIR)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(abspath $(BIN_DIR)) $(GOLANGCI_LINT_VERSION)

# Lint the Makefile itself.
#
# Layer 1: checkmake (reads checkmake.ini for rule thresholds).
#
# Layer 2: GNU Make's `--warn-undefined-variables` in dry-run mode
# against a bundle of representative targets (LINT_MAKE_TARGETS).
# Any warning is escalated to an error.
.PHONY: lint-make
lint-make:
	checkmake --config=checkmake.ini Makefile
	@echo "Probing --warn-undefined-variables across representative targets..."
	@if $(MAKE) --warn-undefined-variables --no-print-directory -n $(LINT_MAKE_TARGETS) 2>&1 \
	    | grep -E '^Makefile:.*warning:'; then \
	    echo "FAIL: --warn-undefined-variables reported issues"; \
	    exit 1; \
	fi
	@echo "--warn-undefined-variables: clean"

# Lint every shell script under hack/ recursively so any
# subdirectories are covered. -x lets shellcheck follow
# source-statements to other files in the tree.
.PHONY: lint-hack
lint-hack:
	find hack -type f -name '*.sh' -exec shellcheck -x {} +

.PHONY: lint-dockerfile
lint-dockerfile:
	hadolint $(LINT_DOCKERFILES)

# ---------------------------------------------------------------------------
# Tests.
# ---------------------------------------------------------------------------
# platform/ebpf unit tests read xdp_pass.bpf.o off disk relative
# to the package dir, so the object must exist when `go test` runs,
# and the dispatcher embeds are needed because the dispatcher Go
# package go:embeds its .bpf.o files at compile time. The OCI puller tests use
# e2e/testdata/bpf/xdp_pass.bpf.o as their real bytecode fixture,
# read at runtime: successful-pull cases extract and validate a
# genuine ELF, and the malformed-label cases share the same fixture
# for consistency even though they fail before extraction. Building
# the whole $(E2E_BPF_OBJECTS) set rather than that one object is a
# deliberate choice -- the e2e corpus is one small, coherent build
# set, and bpfman-vet already depends on it.
.PHONY: test
test: $(DISPATCHER_BPF_EMBEDS) $(PLATFORM_EBPF_BPF_OBJECTS) $(E2E_BPF_OBJECTS)
	$(strip go test $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(TEST_TAGS),-tags '$(TEST_TAGS)') $(if $(STATIC),-ldflags "$(TEST_LDFLAGS)") $(if $(filter-out 0,$(PARALLEL)),-parallel $(PARALLEL)) ./...)

# Local test-all: runs every host-side test surface in the order
# CI's tests/* matrix runs them, bypassing the Dockerfile.ci
# container build path. Intended as a pre-push gate so a change
# that touches code reachable from more than one suite (e.g. a
# shared BPF object loaded by both test-e2e and test-e2e-scripts)
# is exercised end-to-end before the change leaves the working
# tree. Order is cheapest-first so a regression fails fast and
# points at a small surface.
#
# Each phase is invoked via $(MAKE) in the recipe rather than as
# a prerequisite so GNU make sequences them strictly even under
# `make -j`, mirroring the test-e2e-scripts ordering shape.
.PHONY: test-all
test-all:
	$(Q)$(MAKE) test
	$(Q)$(MAKE) lint-go
	$(Q)$(MAKE) lint-make
	$(Q)$(MAKE) test-e2e-scripts
	$(Q)$(MAKE) test-e2e
	$(Q)$(MAKE) test-e2e-grpc

# bpfman-ns transport cross-architecture tests
#
# Proves the bpfman-ns transport's C constructor and nsexec code compile,
# link, and run on each target architecture. Uses cross-compilation
# GCC and QEMU user-mode emulation for foreign architectures.
#
# The CC is auto-detected: Nix-style triples are tried first
# (<prefix>-unknown-linux-gnu-gcc), then distro-style
# (<prefix>-linux-gnu-gcc). QEMU adds -L <sysroot> automatically
# when a distro sysroot directory exists (/usr/<prefix>-linux-gnu).
#
# Usage:
#   make test-bpfman-ns               # native amd64 only
#   make test-bpfman-ns-arm64         # single foreign architecture
#   make test-bpfman-ns-cross         # all architectures
.PHONY: test-bpfman-ns test-bpfman-ns-amd64
test-bpfman-ns test-bpfman-ns-amd64: | $(BIN_DIR)
	@echo "=== ns: amd64 ==="
	$(strip go test -c $(EXTRA_GOFLAGS) $(if $(BPFMAN_NS_TAGS),-tags=$(BPFMAN_NS_TAGS)) -o $(BPFMAN_NS_TEST_BIN) ./internal/bpfman/ns/)
	file $(BPFMAN_NS_TEST_BIN)
	sudo $(BPFMAN_NS_TEST_BIN) -test.v

.PHONY: test-bpfman-ns-arm64 test-bpfman-ns-ppc64le test-bpfman-ns-s390x
test-bpfman-ns-arm64 test-bpfman-ns-ppc64le test-bpfman-ns-s390x:
	BPFMAN_NS_TEST_BIN=$(BPFMAN_NS_TEST_BIN) BPFMAN_NS_TAGS=$(BPFMAN_NS_TAGS) \
		hack/test-bpfman-ns-cross.sh $(@:test-bpfman-ns-%=%)

.PHONY: test-bpfman-ns-cross
test-bpfman-ns-cross: $(addprefix test-bpfman-ns-,$(BPFMAN_NS_ARCHES))

.PHONY: $(BIN_DIR)/e2e.test
$(BIN_DIR)/e2e.test: $(DISPATCHER_BPF_EMBEDS) $(E2E_BPF_OBJECTS) | $(BIN_DIR)
	$(strip go test -c $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(E2E_TAGS),-tags=$(E2E_TAGS)) $(if $(STATIC),-ldflags "$(TEST_LDFLAGS)") -o $(BIN_DIR)/e2e.test ./e2e)

# STRESS_COUNT is honoured here too: `-test.count=$(STRESS_COUNT)`
# is harmless at the default 1 and turns the same recipe into a
# stress run when bumped (CI pins it to 5 so every PR gets a small
# count loop on top of the deterministic gate).
.PHONY: test-e2e
test-e2e: $(BIN_DIR)/e2e.test
	$(Q)$(MAKE) e2e-kmod-reload
	sudo BPFMAN_E2E_BYTECODE_DIR=$(abspath e2e) $(call forward-env,BPFMAN_E2E_ISOLATED_RUNTIME BPFMAN_E2E_POLICY_RULE_PREF BPFMAN_LOG) $(BIN_DIR)/e2e.test -test.v -test.failfast -test.count=$(STRESS_COUNT) $(if $(filter-out 0,$(PARALLEL)),-test.parallel $(PARALLEL)) $(if $(TEST),-test.run $(TEST))

# Parallel gRPC e2e: stands up a real `bpfman serve` subprocess and
# fans goroutines through load/get/attach/detach/unload over the
# socket. The test resolves bin/bpfman via the source tree, so the
# daemon binary must be built; bpfman-compile is a hard prereq.
# BPFMAN_GRPC_GOROUTINES and BPFMAN_GRPC_ITERATIONS are the
# concurrency knobs; BPFMAN_LOG controls the daemon-side log spec
# (e.g. info,lock=debug,store=debug). All three are forwarded into
# the sudo'd test process.
#
# Output: the spawned bpfman daemon writes its stdout/stderr to
# /tmp/bpfman-grpc-daemon.log (truncated each run), so the test
# binary's own stdout/stderr only carries the Go test framework's
# output -- short enough to read on the terminal, no tee/awk
# pipeline required.

# Where to look for the artefacts at runtime. Defaults point at
# the in-tree build outputs; ci-test-e2e-grpc overrides them to
# the docker-buildx extraction location. The daemon opens .bpf.o
# files off disk, so the build system tells it both where bpfman
# lives and where the testdata/bpf tree lives.
E2E_GRPC_TEST_BIN     ?= $(BIN_DIR)/e2e-grpc.test
E2E_GRPC_BPFMAN_BIN   ?= $(BIN_DIR)/bpfman
# Directory containing the testdata/bpf object tree the daemon
# loads from. Defaults to the source tree's e2e/ dir; ci-test-e2e-grpc
# overrides it to the extracted bundle's e2e/ dir.
E2E_GRPC_BYTECODE_DIR ?= $(abspath e2e)

# BPFMAN_* env vars forwarded into the sudo'd e2e-grpc test
# binary by run-e2e-grpc. Each entry is the env variable name
# AND the Make variable name (they must match for forward-env
# to pick it up); add a new knob by appending to this list and
# letting the caller (CI workflow, developer command line, env
# in the parent shell) set it.
E2E_GRPC_FORWARD_VARS := \
	BPFMAN_GRPC_GOROUTINES \
	BPFMAN_GRPC_ITERATIONS \
	BPFMAN_GRPC_PROGRESS_INTERVAL \
	BPFMAN_LOCK_TIMEOUT \
	BPFMAN_LOG \
	BPFMAN_SQLITE_BUSY_TIMEOUT \
	BPFMAN_SQLITE_TX_RETRY_BACKOFFS

.PHONY: $(BIN_DIR)/e2e-grpc.test
$(BIN_DIR)/e2e-grpc.test: $(DISPATCHER_BPF_EMBEDS) $(E2E_BPF_OBJECTS) | $(BIN_DIR)
	$(strip go test -c $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(E2E_TAGS),-tags=$(E2E_TAGS)) $(if $(STATIC),-ldflags "$(TEST_LDFLAGS)") -o $(BIN_DIR)/e2e-grpc.test ./e2e/grpc)

.PHONY: build-e2e-grpc
build-e2e-grpc: $(BIN_DIR)/e2e-grpc.test bpfman-compile

# Tell the test binary where bpfman lives explicitly. The test
# falls back to a PATH lookup if BPFMAN_BIN is unset, but the
# build system is the layer that knows the answer for sure --
# it just produced the binary at $(E2E_GRPC_BPFMAN_BIN) -- so we
# pass an absolute path through sudo rather than relying on PATH
# munging.
.PHONY: run-e2e-grpc
run-e2e-grpc:
	sudo BPFMAN_BIN=$(abspath $(E2E_GRPC_BPFMAN_BIN)) \
	    BPFMAN_E2E_BYTECODE_DIR=$(E2E_GRPC_BYTECODE_DIR) \
	    $(call forward-env,$(E2E_GRPC_FORWARD_VARS)) \
	    $(E2E_GRPC_TEST_BIN) -test.v -test.failfast \
	    -test.count=$(STRESS_COUNT) $(if $(TEST),-test.run $(TEST))

.PHONY: test-e2e-grpc
test-e2e-grpc: build-e2e-grpc
	$(Q)$(MAKE) e2e-kmod-reload
	$(Q)$(MAKE) run-e2e-grpc

# Run every .bpfman script under e2e/scripts/ against the built
# bpfman binary. Each script executes from e2e/ so
# testdata paths match the Go e2e tests. The target runs them
# sequentially, reports failures as it goes, and exits non-zero
# at the end if any script failed. Pass TEST=<name> to restrict
# to scripts whose filename contains <name>.
# Split into build + run so CI can extract pre-built artefacts
# from a hermetic container build (Dockerfile.ci's e2e-export
# stage) and invoke `run-e2e-scripts` directly on the runner
# without re-triggering the build deps. Local invocations of
# `test-e2e-scripts` still build first. The runner is a Go test
# binary built from e2e/scriptrunner; t.Run / t.Parallel /
# go test -json -run come for free, and the binary holds the
# shared /tmp/bpfman-e2e.lock so it is mutually exclusive with
# bin/e2e.test on a single host.

E2E_SCRIPTS_TEST_BIN := $(BIN_DIR)/e2e-scripts.test
E2E_SCRIPTS_TEST_PKG := github.com/bpfman/bpfman/e2e/scriptrunner
E2E_IMAGE_NO_VERIFY_CONFIG := $(abspath e2e/config/no-signature-verification.toml)
BPFMAN_CONFIG ?=

ifeq ($(BPFMAN_E2E_BYTECODE_SOURCE),image)
ifeq ($(BPFMAN_CONFIG),)
BPFMAN_CONFIG := $(E2E_IMAGE_NO_VERIFY_CONFIG)
endif
endif

# Env vars forwarded into the sudo'd test process.
# The script runner hosts a throwaway anonymous local registry for
# image build/load scripts. BPFMAN_E2E_BYTECODE_SOURCE=image also has
# bpfman-shell broker each `program load file` through
# `bpfman image build` plus `program load image`.
# Image-mode scripts use BPFMAN_CONFIG to disable signature
# verification for unsigned throwaway fixture images. Production
# verification policy is still tested separately and remains governed
# by the normal config file.
# BPFMAN_E2E_IMAGE_REGISTRY overrides the throwaway registry for
# explicit external-registry checks.
# BPFMAN_E2E_SCRIPT_TIMEOUT widens the per-script deadline;
# BPFMAN_E2E_SCRIPT_REPEATS turns the corpus into a stress run
# (each script registered N times, wave-diverse dispatch);
# BPFMAN_E2E_SCRIPT_SELECTOR selects scripts with matching #pragma
# labels; BPFMAN_LOG threads through to bpfman-shell when set.
# BPFMAN_LOCK_TIMEOUT is set directly in run-e2e-scripts below:
# high-parallel stress runs can leave many short-lived bpfman
# invocations queued behind the global writer lock, so the script
# target uses a wider default than the CLI's interactive 30s.
# BIN_DIR is passed explicitly below rather than via this list
# because the value gets abspath'd at the call site.
E2E_SCRIPTS_FORWARD_VARS := \
	BPFMAN_CONFIG \
	BPFMAN_E2E_BYTECODE_SOURCE \
	BPFMAN_E2E_IMAGE_REGISTRY \
	BPFMAN_E2E_POLICY_RULE_PREF \
	BPFMAN_E2E_SCRIPT_REPEATS \
	BPFMAN_E2E_SCRIPT_SELECTOR \
	BPFMAN_E2E_SCRIPT_TIMEOUT \
	BPFMAN_LOG

.PHONY: $(BIN_DIR)/e2e-scripts.test
$(BIN_DIR)/e2e-scripts.test: $(DISPATCHER_BPF_EMBEDS) $(E2E_BPF_OBJECTS) | $(BIN_DIR)
	$(strip go test -c $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(E2E_TAGS),-tags=$(E2E_TAGS)) $(if $(STATIC),-ldflags "$(TEST_LDFLAGS)") -o $(BIN_DIR)/e2e-scripts.test ./e2e/scriptrunner)

# Regenerate the lowerer golden fixture. The single dense fixture under
# shell/lower/testdata is the lowerer contract; TestLanguageLoweredGolden
# compares against it in `make test`. Run this after an intended lowerer
# change, then review the diff. Lowering is pure, so this builds but does
# not run the manager; the BPF prerequisites are for a clean checkout.
.PHONY: update-lowered-goldens
update-lowered-goldens: $(DISPATCHER_BPF_EMBEDS)
	$(strip go test $(if $(TEST_TAGS),-tags '$(TEST_TAGS)') $(if $(STATIC),-ldflags "$(TEST_LDFLAGS)") ./cmd/bpfman-shell/shell/lower -run '^TestLanguageLoweredGolden$$' -update)

.PHONY: bpfman-shell-fmt
bpfman-shell-fmt: bpfman-shell-compile
	@set -e; \
	for f in $(BPFMAN_SHELL_FORMAT_SOURCES); do \
	    printf "  BPFMAN-FMT %s\n" "$$f"; \
	    $(BIN_DIR)/bpfman-shell fmt -w "$$f"; \
	done

.PHONY: build-e2e-scripts
build-e2e-scripts: bpfman-compile bpfman-shell-compile $(E2E_SCRIPTS_TEST_BIN)

# PATH is arranged via `sudo env PATH=...` so the script test
# binary can resolve bpfman-shell regardless of how sudo's
# secure_path is configured on the host. Using `env` as the
# sudo'd program means the PATH assignment is an ordinary exec-
# time setting rather than a sudo env passthrough, so it works
# under any sudoers configuration. The test process trusts
# whatever PATH it inherits; no in-code path manipulation.
.PHONY: run-e2e-scripts
run-e2e-scripts:
	sudo env PATH=$(abspath $(BIN_DIR)):$$PATH \
	    BPFMAN_E2E_DIR=$(abspath e2e) \
	    BPFMAN_LOCK_TIMEOUT=$(if $(BPFMAN_LOCK_TIMEOUT),$(BPFMAN_LOCK_TIMEOUT),5m) \
	    $(call forward-env,$(E2E_SCRIPTS_FORWARD_VARS)) \
	    $(E2E_SCRIPTS_TEST_BIN) -test.v \
	    -test.count=$(STRESS_COUNT) $(if $(filter-out 0,$(PARALLEL)),-test.parallel $(PARALLEL)) \
	    -test.run "$(if $(TEST),$(TEST),TestBPFManScripts)"

# `run-e2e-scripts` lives in the recipe rather than the
# prerequisite list because GNU make does not sequence
# prerequisites against each other, only against the target. Under
# `make -j` the runner could otherwise start before
# e2e-kmod-reload finished, and the scripts would exercise the
# previously-loaded (or absent) module. The sub-make invocation
# enforces the phase boundary: build + reload complete first, then
# the runner kicks off.
.PHONY: test-e2e-scripts
test-e2e-scripts: build-e2e-scripts e2e-kmod-reload
	$(Q)$(MAKE) run-e2e-scripts

.PHONY: test-e2e-scripts-file
test-e2e-scripts-file:
	$(Q)$(MAKE) test-e2e-scripts BPFMAN_E2E_BYTECODE_SOURCE=

.PHONY: test-e2e-scripts-image
test-e2e-scripts-image:
	$(Q)$(MAKE) test-e2e-scripts BPFMAN_E2E_BYTECODE_SOURCE=image

.PHONY: test-e2e-scripts-image-ci
test-e2e-scripts-image-ci:
	$(Q)$(MAKE) test-e2e-scripts-image RACE=0 STRESS_COUNT=5

.PHONY: test-e2e-scripts-stress
test-e2e-scripts-stress:
	$(Q)$(MAKE) test-e2e-scripts \
	    BPFMAN_E2E_SCRIPT_REPEATS=$(if $(BPFMAN_E2E_SCRIPT_REPEATS),$(BPFMAN_E2E_SCRIPT_REPEATS),$(BPFMAN_E2E_SCRIPT_STRESS_REPEATS)) \
	    PARALLEL=$(if $(PARALLEL),$(PARALLEL),$(BPFMAN_E2E_SCRIPT_STRESS_PARALLEL)) \
	    BPFMAN_LOCK_TIMEOUT=$(if $(BPFMAN_LOCK_TIMEOUT),$(BPFMAN_LOCK_TIMEOUT),5m)

.PHONY: test-e2e-published-images
test-e2e-published-images:
	$(Q)$(MAKE) test-e2e-scripts \
	    BPFMAN_E2E_SCRIPT_SELECTOR=external \
	    TEST='TestBPFManScripts/scripts/TestPublishedImage'


# ---------------------------------------------------------------------------
# E2E kmod.
# ---------------------------------------------------------------------------
# Kept phony rather than a file rule keyed off source timestamps:
# a kmod's correctness is tied to the kernel it was built against,
# and a file-level rule would miss reboots into a different kernel
# or changes to KDIR / KERNEL_DEV / KERNEL_RELEASE. Letting kbuild
# decide is the conservative choice -- the prepare-kdir step plus
# a no-op kbuild are fast enough that the unconditional re-check
# costs little.
#
# `MAKEFLAGS=` on the kbuild sub-make scrubs any flags the parent
# pushed down through MAKEFLAGS before the sub-make starts; in
# practice this is here so `make lint-make`'s
# `--warn-undefined-variables` (which the parent make detects as
# recursive and runs even under -n via the `$(MAKE)` literal in
# this recipe) does not propagate into the kernel's top-level
# Makefile, which is not warning-clean and is not ours to fix.
.PHONY: e2e-kmod-build
e2e-kmod-build:
	$(call quiet_cmd,KMOD,$(E2E_KMOD))
	$(Q)if [ -f "$(E2E_KMOD_STAMP)" ] && \
	    [ "$$(cat "$(E2E_KMOD_STAMP)")" != "$(KERNEL_RELEASE)" ]; then \
	    echo "e2e kmod: built for $$(cat "$(E2E_KMOD_STAMP)"), now $(KERNEL_RELEASE); cleaning stale build" >&2; \
	    $(MAKE) clean-e2e-kmod; \
	fi
	$(Q)set -e; \
	    kdir=$$(KDIR="$(KDIR)" \
	    KERNEL_DEV="$(KERNEL_DEV)" \
	    KERNEL_RELEASE="$(KERNEL_RELEASE)" \
	    KERNEL_MOD_DIR_VERSION="$(KERNEL_MOD_DIR_VERSION)" \
	    E2E_KMOD_KBUILD="$(E2E_KMOD_KBUILD)" \
	    bash $(E2E_KMOD_PREPARE_KDIR)); \
	    if [ -f "$$kdir/.kernel-source" ]; then \
	        ksrc=$$(cat "$$kdir/.kernel-source"); \
	        MAKEFLAGS= $(MAKE) -C $(E2E_KMOD_DIR) KDIR="$$ksrc" KBUILD_OUTPUT="$$kdir"; \
	    else \
	        MAKEFLAGS= $(MAKE) -C $(E2E_KMOD_DIR) KDIR="$$kdir"; \
	    fi
	$(Q)test -f $(E2E_KMOD)
	$(Q)if ! readelf -S $(E2E_KMOD) 2>/dev/null | grep -q '\.BTF'; then \
	    { \
	        echo "error: $(E2E_KMOD) has no .BTF section"; \
	        echo ""; \
	        echo "The module built but kbuild produced no BTF, so fentry/fexit"; \
	        echo "attach to its functions fails at load time with a cryptic"; \
	        echo "\"not supported\". Usual causes: the BTF [M] step ran without a"; \
	        echo "vmlinux (see prepare-kdir.sh), pahole (dwarves) is missing, or a"; \
	        echo "stale .ko was reused without relinking across a kernel change."; \
	        echo "Run 'make clean-e2e-kmod' and rebuild; ensure pahole and"; \
	        echo "/sys/kernel/btf/vmlinux (or KERNEL_DEV=<path>/vmlinux) exist."; \
	    } >&2; \
	    exit 1; \
	fi
	$(Q)printf '%s\n' "$(KERNEL_RELEASE)" > $(E2E_KMOD_STAMP)

.PHONY: clean-e2e-kmod
clean-e2e-kmod:
	$(call quiet_cmd,CLEAN,$(E2E_KMOD_DIR))
	$(Q)if [ -d "$(E2E_KMOD_KBUILD)" ]; then \
	    find "$(E2E_KMOD_KBUILD)" -type d -exec chmod u+w {} +; \
	fi
	$(Q)if [ -d "$(KDIR)" ]; then \
	    MAKEFLAGS= $(MAKE) -C $(E2E_KMOD_DIR) KDIR=$(KDIR) clean; \
	else \
	    $(RM) $(E2E_KMOD_DIR)/*.ko $(E2E_KMOD_DIR)/*.o $(E2E_KMOD_DIR)/*.mod \
	        $(E2E_KMOD_DIR)/*.mod.c $(E2E_KMOD_DIR)/.*.cmd \
	        $(E2E_KMOD_DIR)/Module.symvers $(E2E_KMOD_DIR)/modules.order; \
	fi
	$(Q)$(RM) -r $(E2E_KMOD_KBUILD)
	$(Q)$(RM) $(E2E_KMOD_STAMP)

# Load the built module into the running kernel. Idempotent: if
# the module is already present in lsmod, the target succeeds
# without re-loading (insmod would otherwise fail with "File
# exists"). Depends on e2e-kmod-build so a stale .ko gets rebuilt
# before load. Requires root; the recipe sudos internally so the
# normal `make` invocation stays unprivileged.
.PHONY: e2e-kmod-insmod
e2e-kmod-insmod: e2e-kmod-build
	$(call quiet_cmd,INSMOD,$(E2E_KMOD))
	$(Q)if lsmod | awk '{print $$1}' | grep -qx bpfman_e2e_targets; then \
	    echo "  bpfman_e2e_targets already loaded; skipping"; \
	else \
	    sudo insmod $(E2E_KMOD); \
	fi

# Unload the module from the running kernel. Idempotent: if the
# module is not present in lsmod, the target succeeds without
# action (rmmod would otherwise fail with "Module ... is not
# currently loaded"). Requires root; the recipe sudos internally.
.PHONY: e2e-kmod-rmmod
e2e-kmod-rmmod:
	$(call quiet_cmd,RMMOD,bpfman_e2e_targets)
	$(Q)if lsmod | awk '{print $$1}' | grep -qx bpfman_e2e_targets; then \
	    sudo rmmod bpfman_e2e_targets; \
	else \
	    echo "  bpfman_e2e_targets not loaded; skipping"; \
	fi

# Rebuild and reload the module. Unlike e2e-kmod-insmod this is
# NOT idempotent on the "already loaded" path: it unloads the
# current module (if any) and inserts a freshly-built one, so a
# stale .ko from before a source edit cannot silently shadow the
# new build. Used as the test-e2e-scripts dep so a test run
# always exercises the just-built code. Requires root; the
# recipe sudos internally. If rmmod fails with "Module ... is in
# use", a previous interrupted run may have left managed bpfman
# programs attached to the module; use e2e-kmod-force-reload to
# delete that managed state before reloading.
.PHONY: e2e-kmod-reload
e2e-kmod-reload: e2e-kmod-build
	$(call quiet_cmd,RELOAD,$(E2E_KMOD))
	$(Q)if lsmod | awk '{print $$1}' | grep -qx bpfman_e2e_targets; then \
	    sudo rmmod bpfman_e2e_targets; \
	fi
	$(Q)sudo insmod $(E2E_KMOD)

# Force a clean e2e kmod reload after an interrupted stress run.
# This is intentionally opt-in: `bpfman program delete -r --all`
# tears down every managed bpfman program on the host, so it is
# appropriate for a dedicated e2e development machine but too broad
# for the ordinary e2e-kmod-reload path.
.PHONY: e2e-kmod-force-reload
e2e-kmod-force-reload: bpfman-build bpfman-e2e-cleanup-build
	$(call quiet_cmd,RESET,bpfman managed state)
	$(Q)sudo $(BIN_DIR)/bpfman program delete -r --all
	$(Q)sudo $(BIN_DIR)/bpfman-e2e-cleanup --apply
	$(Q)$(MAKE) e2e-kmod-reload

# ---------------------------------------------------------------------------
# Documentation.
# ---------------------------------------------------------------------------
.PHONY: doc
doc:
	@echo "Starting pkgsite documentation server..."
	@echo "Open http://localhost:$(DOC_PORT)/github.com/bpfman/bpfman"
	@echo "Press Ctrl+C to stop"
	@go run golang.org/x/pkgsite/cmd/pkgsite@latest -http=localhost:$(DOC_PORT) .

.PHONY: doc-text
doc-text:
	@echo "=== Public API ===" && echo
	@for pkg in ./bpfman ./client ./csi; do \
		echo "--- $$pkg ---" && go doc -all $$pkg 2>/dev/null && echo; \
	done

# ---------------------------------------------------------------------------
# bpfman build.
#
# Note: bpfman-proto is not a dependency here since pb files are committed.
# Run 'make bpfman-proto' explicitly after modifying proto/bpfman.proto.
# CGO is required for the bpfman-ns transport, which uses a C constructor to call
# setns() before Go runtime starts (needed for uprobe container attachment).
# ---------------------------------------------------------------------------
.PHONY: bpfman-build
bpfman-build: bpfman-fmt bpfman-compile

# Format every .go file in the tree. `go fmt ./...` skips files that
# don't compile under the default build tags (e.g. anything behind
# //go:build e2e), so we'd silently miss formatting drift in e2e/.
# gofmt invoked directly on the file list ignores build tags and
# formats every source file, matching what ci-check-fmt expects.
.PHONY: bpfman-fmt
bpfman-fmt:
	@find . -type f -name '*.go' -not -path './vendor/*' -print0 | xargs -0 gofmt -w

.PHONY: bpfman-clang-format
bpfman-clang-format:
	@git ls-files -- '*.c' '*.h' ':!vendor' | xargs clang-format -i

# Apply gofmt + goimports to every .go file via golangci-lint's `fmt`
# subcommand, which honours the formatters block in .golangci.yml --
# notably the goimports local-prefixes pin. Goes via golangci-lint
# rather than a standalone goimports binary because the static dev
# shell's glibc.static link path makes a freshly-installed
# goimports segfault at runtime (NSS dlopen). golangci-lint is
# already pinned for `make lint` so reusing it costs nothing.
.PHONY: bpfman-goimports
bpfman-goimports: $(BIN_DIR)/golangci-lint
	$(BIN_DIR)/golangci-lint fmt

# Run go vet over the tree with e2e+bpfman_ns set so the build-tagged
# files (entire e2e/ package, CGO-namespaced bpfman-ns transport) are
# vetted alongside the rest. No file in the tree uses negative tags
# like !e2e, so this pass supersets a tag-less one and a second pass
# would be redundant. A single SQLite driver (mattn/go-sqlite3) means
# one pass covers the store too.
.PHONY: bpfman-vet
bpfman-vet: $(DISPATCHER_BPF_EMBEDS)
	go vet -tags 'e2e,bpfman_ns' ./...

# Apply the Go modernisers (go fix) over the same tag set as
# bpfman-vet, so every file -- the e2e/bpfman_ns build-tagged packages
# included -- is rewritten to the latest idioms. GOTOOLCHAIN forces the
# moderniser toolchain on top of whatever build Go is active,
# downloading it on demand; see GOFIX_GO_VERSION for why the pin lives
# apart from GO_VERSION. Like bpfman-fmt this mutates the tree in
# place; ci-check-gofix wraps it with a git-diff gate. The example
# applications are excluded: they stay as their authors wrote them.
.PHONY: bpfman-gofix
bpfman-gofix: $(DISPATCHER_BPF_EMBEDS)
	GOTOOLCHAIN=$(GOFIX_GO_VERSION) go fix -tags 'e2e,bpfman_ns' \
		$$(go list ./... | grep -v '^github.com/bpfman/bpfman/examples')

# Compile bpfman. Depends on the dispatcher BPF embeds because
# the dispatcher Go package's go:embed directives need them at
# compile time. Make's pattern rules build them on demand if
# missing or out of date.
.PHONY: bpfman-compile
bpfman-compile: $(DISPATCHER_BPF_EMBEDS) | $(BIN_DIR)
	$(strip go build $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(BUILD_TAGS),-tags '$(BUILD_TAGS)') $(if $(BIN_LDFLAGS),-ldflags "$(BIN_LDFLAGS)") -o $(BIN_DIR)/bpfman ./cmd/bpfman)

.PHONY: clean-bpfman
clean-bpfman:
	$(RM) $(BIN_DIR)/bpfman

# bpfman-shell is the development / test / ops companion to bpfman.
# It hosts the DSL script runner and (in time) the test
# scaffolding subcommands. Production deployments must ship only
# bin/bpfman; bin/bpfman-shell is intended for dev and CI.
.PHONY: bpfman-shell-build
bpfman-shell-build: bpfman-fmt bpfman-shell-compile

# Depends on the dispatcher BPF embeds because the shell transitively
# imports the dispatcher package and its go:embed directives need the
# .bpf.o objects present at compile time. Make supplies those non-Go
# inputs; the Go toolchain owns Go freshness and decides what actually
# needs recompiling.
.PHONY: bpfman-shell-compile
bpfman-shell-compile: $(DISPATCHER_BPF_EMBEDS) | $(BIN_DIR)
	$(strip go build $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(BUILD_TAGS),-tags '$(BUILD_TAGS)') $(if $(BIN_LDFLAGS),-ldflags "$(BIN_LDFLAGS)") -o $(BIN_DIR)/bpfman-shell ./cmd/bpfman-shell)

.PHONY: clean-bpfman-shell
clean-bpfman-shell:
	$(RM) $(BIN_DIR)/bpfman-shell

# bpfman-e2e-cleanup is the host-state cleanup utility. It finds and
# removes kernel-side residue left behind by bpfman (orphan
# dispatcher links) and by the e2e harness (test interfaces and
# netns from interrupted runs). Ships only in dev / CI images.
.PHONY: bpfman-e2e-cleanup-build
bpfman-e2e-cleanup-build: bpfman-fmt bpfman-e2e-cleanup-compile

.PHONY: bpfman-e2e-cleanup-compile
bpfman-e2e-cleanup-compile: | $(BIN_DIR)
	$(strip go build $(if $(RACE),-race,) $(EXTRA_GOFLAGS) $(if $(BUILD_TAGS),-tags '$(BUILD_TAGS)') $(if $(BIN_LDFLAGS),-ldflags "$(BIN_LDFLAGS)") -o $(BIN_DIR)/bpfman-e2e-cleanup ./cmd/bpfman-e2e-cleanup)

.PHONY: clean-bpfman-e2e-cleanup
clean-bpfman-e2e-cleanup:
	$(RM) $(BIN_DIR)/bpfman-e2e-cleanup


# ---------------------------------------------------------------------------
# Proto generation for bpfman gRPC API.
# ---------------------------------------------------------------------------
.PHONY: bpfman-proto
bpfman-proto: $(BPFMAN_PB_DIR)/bpfman.pb.go $(BPFMAN_PB_DIR)/bpfman_grpc.pb.go

# protoc (downloaded into $(BIN_DIR)) discovers --go_out / --go-grpc_out
# plugins on PATH, so the generated-stub rule prepends $(BIN_DIR) before
# invoking it. protoc and the protoc-gen-* binaries are order-only
# prerequisites (after `|`) so a fresh checkout that lacks them fetches
# them once, but their mtime does not invalidate the committed .pb.go
# files.
# proto/bpfman.proto is upstream bpfman/bpfman's file verbatim; its
# go_package targets the upstream client stubs (clients/gobpfman/v1).
# The M mapping below overrides that per invocation so our server
# stubs generate under server/pb without editing the shared file.
$(BPFMAN_PB_DIR)/bpfman.pb.go $(BPFMAN_PB_DIR)/bpfman_grpc.pb.go: \
		$(BPFMAN_PROTO_DIR)/bpfman.proto \
		| $(BIN_DIR)/protoc $(BIN_DIR)/protoc-gen-go $(BIN_DIR)/protoc-gen-go-grpc
	mkdir -p $(BPFMAN_PB_DIR)
	PATH="$(abspath $(BIN_DIR)):$$PATH" \
	$(abspath $(BIN_DIR))/protoc --go_out=$(BPFMAN_PB_DIR) --go_opt=paths=source_relative \
		--go_opt='Mbpfman.proto=github.com/bpfman/bpfman/server/pb;pb' \
		--go-grpc_out=$(BPFMAN_PB_DIR) --go-grpc_opt=paths=source_relative \
		--go-grpc_opt='Mbpfman.proto=github.com/bpfman/bpfman/server/pb;pb' \
		--proto_path=$(BPFMAN_PROTO_DIR) \
		$<

# Download a pinned protoc into $(BIN_DIR). protoc is the C++ compiler,
# not a Go module, so it is fetched as a release binary rather than
# `go install`ed; PROTOC_VERSION pins the release. The proto file has no
# google.protobuf imports, so the release's include/ tree is not needed.
PROTOC_OS := linux
ifeq ($(HOST_ARCH),x86_64)
    PROTOC_ARCH := x86_64
else ifeq ($(HOST_ARCH),i686)
    PROTOC_ARCH := x86_32
else ifeq ($(HOST_ARCH),aarch64)
    PROTOC_ARCH := aarch_64
else ifeq ($(HOST_ARCH),ppc64le)
    PROTOC_ARCH := ppcle_64
else ifeq ($(HOST_ARCH),powerpc64le)
    PROTOC_ARCH := ppcle_64
else ifeq ($(HOST_ARCH),s390x)
    PROTOC_ARCH := s390_64
else
    $(error unsupported HOST_ARCH=$(HOST_ARCH) for protoc download)
endif
PROTOC_ZIP := protoc-$(PROTOC_VERSION)-$(PROTOC_OS)-$(PROTOC_ARCH).zip
PROTOC_URL := https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/$(PROTOC_ZIP)

$(BIN_DIR)/protoc: | $(BIN_DIR)
	@printf "  DOWNLOAD protoc %s (%s)\n" "$(PROTOC_VERSION)" "$(PROTOC_ARCH)"
	$(Q)tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
		curl -fsSL -o "$$tmp/protoc.zip" "$(PROTOC_URL)" && \
		unzip -q -o "$$tmp/protoc.zip" -d "$$tmp/out" && \
		install -m 0755 "$$tmp/out/bin/protoc" "$(BIN_DIR)/protoc"

# Build protoc plugins into $(BIN_DIR) from the versions pinned above so
# the Fedora-only build path does not need them on $PATH separately.
# Mirrors the golangci-lint pattern; bump PROTOC_GEN_*_VERSION here.
$(BIN_DIR)/protoc-gen-go: | $(BIN_DIR)
	GOBIN=$(abspath $(BIN_DIR)) go install \
		google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)

$(BIN_DIR)/protoc-gen-go-grpc: | $(BIN_DIR)
	GOBIN=$(abspath $(BIN_DIR)) go install \
		google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

# ---------------------------------------------------------------------------
# BPF build rules.
#
# No `bpf-build` umbrella target: consumers depend directly on
# the actual .bpf.o outputs they need ($(DISPATCHER_BPF_EMBEDS)
# for the production binary, $(E2E_BPF_OBJECTS) for tests that
# exercise e2e BPF programs). Make's dependency graph handles
# incremental rebuilds against the real outputs without needing
# a phony intermediary.
# ---------------------------------------------------------------------------
dispatcher/%.bpf.o: dispatcher/bpf/%.bpf.c Makefile
	$(call quiet_cmd,CLANG-BPF,$@)
	$(Q)clang $(LIBBPF_CFLAGS) $(BPF_CFLAGS) -g -O2 -target bpfel -c $(BPF_TARGET_ARCH) \
		-MD -MP -MF$(@:.bpf.o=.bpf.d) $< -o $@

e2e/testdata/bpf/%.bpf.o: e2e/testdata/bpf/%.bpf.c Makefile
	$(call quiet_cmd,CLANG-BPF,$@)
	$(Q)clang $(LIBBPF_CFLAGS) $(BPF_CFLAGS) -g -O2 -target bpfel -c $(BPF_TARGET_ARCH) \
		-MD -MP -MF$(@:.bpf.o=.bpf.d) $< -o $@

# platform/ebpf consumes the same .bpf.c sources as the e2e tests
# but its unit tests read the compiled object next to the Go test
# files. Mirrors the dispatcher pattern: emit straight into the
# consuming package's directory, no intermediate cp.
platform/ebpf/%.bpf.o: e2e/testdata/bpf/%.bpf.c Makefile
	$(call quiet_cmd,CLANG-BPF,$@)
	$(Q)clang $(LIBBPF_CFLAGS) $(BPF_CFLAGS) -g -O2 -target bpfel -c $(BPF_TARGET_ARCH) \
		-MD -MP -MF$(@:.bpf.o=.bpf.d) $< -o $@

.PHONY: clean-bpf
clean-bpf:
	$(RM) $(DISPATCHER_BPF_EMBEDS) $(DISPATCHER_BPF_DEPS) \
	      $(E2E_BPF_OBJECTS) $(E2E_BPF_DEPS) \
	      $(PLATFORM_EBPF_BPF_OBJECTS) $(PLATFORM_EBPF_BPF_DEPS)

-include $(DISPATCHER_BPF_DEPS) $(E2E_BPF_DEPS) $(PLATFORM_EBPF_BPF_DEPS)

# ---------------------------------------------------------------------------
# Docker image builds.
# ---------------------------------------------------------------------------

# Build bpfman image from the host-built binary. Intended for local
# development and operator integration testing.
.PHONY: build-image-dev
build-image-dev: bpfman-build
	$(OCI_BIN) build \
		$(if $(filter-out 0,$(OCI_BIN_IS_PODMAN)),--ignorefile=Dockerfile.bpfman.dev.dockerignore) \
		-t $(BPFMAN_IMG) \
		--build-arg RUNTIME_IMAGE=$(DEV_RUNTIME_IMAGE) \
		-f Dockerfile.bpfman.dev \
		$(EXTRA_DOCKER_BUILD_ARGS) .

# Canonical bpfman image build via buildx and the Fedora multiarch
# Dockerfile. The same recipe drives dev and CI; mode is selected
# by the variable knobs below.
#
# Modes:
#
#   make build-image
#       Default: current arch only, loaded into the local Docker
#       store. The cross-compile happens inside the container, so no
#       host toolchain is required (contrast with build-image-dev,
#       which packages a host-built binary).
#
#   make build-image-{amd64,arm64,ppc64le,s390x}
#       Per-arch presets that pin PLATFORMS to a single foreign arch
#       and --load. Useful when you want to run a foreign-arch image
#       under host binfmt + QEMU.
#
#   make build-image PLATFORMS=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x
#       Multi-arch, cache-only build (no output). The local Docker
#       store cannot hold a multi-arch manifest, so the manifest
#       stays in the BuildKit cache. Useful as a "does it all
#       compile?" sanity check.
#
#   make build-image \
#       PLATFORMS=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x \
#       PUSH=1 \
#       BPFMAN_IMG=<registry/repo:tag>
#       CI publish path: pushes a multi-arch manifest to the
#       registry, with SLSA build provenance (mode=max) and SBOM
#       attestations attached per platform.

# Pure-Nix OCI image, byte-reproducible and built without invoking
# a Docker daemon. Pulls the layered tarball that nix produces and
# `docker load`s it in one shot; --no-link keeps the workspace free
# of a stray result symlink that could collide with `nix build .`.
# See nix/image.nix for what is in the image and why.
.PHONY: build-image-nix
build-image-nix:
	$(OCI_BIN) load < $$(nix build .#bpfman-image --print-out-paths --no-link)

.PHONY: build-image
build-image:
	$(OCI_BIN) buildx build \
		$(if $(PLATFORMS),--platform $(PLATFORMS)) \
		$(BUILDX_OUTPUT) \
		$(BUILDX_ATTEST) \
		$(if $(BUILDX_METADATA_FILE),--metadata-file=$(BUILDX_METADATA_FILE)) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_BRANCH=$(GIT_BRANCH) \
		--build-arg GIT_VERSION=$(GIT_VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		$(if $(STATIC),--build-arg STATIC=1) \
		--build-arg RUNTIME_IMAGE=$(RUNTIME_IMAGE) \
		--build-arg EXTRA_GOFLAGS="$(EXTRA_GOFLAGS)" \
		--build-arg EXTRA_GO_LDFLAGS="$(EXTRA_GO_LDFLAGS)" \
		--build-arg IMAGE_REF="$(IMAGE_REF)" \
		--build-arg SIGNER_IDENTITY="$(SIGNER_IDENTITY)" \
		--build-arg OIDC_ISSUER="$(OIDC_ISSUER)" \
		-f $(BPFMAN_DOCKERFILE) \
		-t $(BPFMAN_IMG) \
		$(BUILDX_EXTRA_ARGS) .

# Per-arch presets pinning PLATFORMS to a single foreign arch.
# Each invocation builds one platform and --loads it into the local
# Docker store under the default $(BPFMAN_IMG) ref (e.g.
# quay.io/bpfman/bpfman:latest). The arch is implicit in the make
# target chosen, so the pullspec does not encode it; each
# invocation overwrites the previous one. To keep multiple arches
# loaded simultaneously, pass BPFMAN_IMG explicitly with distinct
# tags.
#
#   make build-image-amd64
#   make build-image-arm64
#   make build-image-ppc64le
#   make build-image-s390x
#
# The CI publish path uses `build-image` directly with a comma-
# separated PLATFORMS list; these presets are purely local-dev
# shortcuts.
.PHONY: build-image-amd64 build-image-arm64 build-image-ppc64le build-image-s390x
build-image-amd64 build-image-arm64 build-image-ppc64le build-image-s390x: build-image-%:
	$(MAKE) build-image PLATFORMS=linux/$*

# Sign a published multi-arch image with cosign, anchored to the
# immutable index digest rather than the mutable tag.
#
# This target reads the digest from the buildx metadata file
# produced by the previous build-image run, so the same Make
# recipe serves both CI and local testing.
#
# CI usage (keyless via GitHub Actions OIDC):
#
#   make build-image \
#     PUSH=1 \
#     BPFMAN_IMG=<registry/repo:tag> \
#     BUILDX_METADATA_FILE=$${RUNNER_TEMP}/buildx-meta.json \
#     ...
#   make cosign-sign \
#     BPFMAN_IMG=<registry/repo:tag> \
#     BUILDX_METADATA_FILE=$${RUNNER_TEMP}/buildx-meta.json
#
# Local usage (interactive OAuth signing identity):
#
#   nix shell nixpkgs#cosign      # cosign is not in the dev profile
#
#   make build-image \
#     PLATFORMS=linux/amd64 \
#     PUSH=1 \
#     BPFMAN_IMG=ttl.sh/me/bpfman-test:latest \
#     BUILDX_METADATA_FILE=/tmp/buildx-meta.json
#
#   make cosign-sign \
#     BPFMAN_IMG=ttl.sh/me/bpfman-test:latest \
#     BUILDX_METADATA_FILE=/tmp/buildx-meta.json
#
# The local invocation triggers an interactive browser OAuth flow;
# the resulting Rekor record is tied to the user's personal
# identity (Google, GitHub, etc.) rather than to a workflow OIDC
# token. The mechanics are otherwise identical to CI.
.PHONY: cosign-sign
cosign-sign:
	@command -v cosign >/dev/null 2>&1 || { \
		echo "error: cosign is not installed; try 'nix shell nixpkgs#cosign'" >&2; \
		exit 1; \
	}
	@command -v jq >/dev/null 2>&1 || { \
		echo "error: jq is not installed" >&2; \
		exit 1; \
	}
	@if [ -z "$(BUILDX_METADATA_FILE)" ]; then \
		echo "error: BUILDX_METADATA_FILE must be set" >&2; \
		echo "       (re-run build-image with the same value first)" >&2; \
		exit 1; \
	fi
	@if [ ! -f "$(BUILDX_METADATA_FILE)" ]; then \
		echo "error: $(BUILDX_METADATA_FILE) does not exist" >&2; \
		echo "       (run build-image first to produce it)" >&2; \
		exit 1; \
	fi
	@digest=$$(jq -r '."containerimage.digest" // empty' "$(BUILDX_METADATA_FILE)"); \
	if [ -z "$$digest" ]; then \
		echo "error: containerimage.digest missing from $(BUILDX_METADATA_FILE)" >&2; \
		cat "$(BUILDX_METADATA_FILE)" >&2; \
		exit 1; \
	fi; \
	echo "Signing $(BPFMAN_IMG)@$$digest"; \
	cosign sign -y "$(BPFMAN_IMG)@$$digest"

# CSI conformance testing
.PHONY: build-image-csi-sanity
build-image-csi-sanity:
	$(OCI_BIN) build -t $(CSI_SANITY_IMG) -f Dockerfile.csi-sanity $(EXTRA_DOCKER_BUILD_ARGS) .

# ---------------------------------------------------------------------------
# Local CI reproducer.
#
# `make ci` runs every pipeline the GH workflows run -- vendor /
# format checks, the bpfman binary build, the lint umbrella, the
# unit tests, and the two e2e jobs (Go binary + .bpfman scripts).
# The CI workflow YAML invokes the same `make ci-*` targets, so
# `make ci` locally is a faithful reproduction of what runs in
# CI; if it passes here, it passes there (modulo runner-specific
# behaviour like NOPASSWD sudo or GHA cache backend).
#
# See Dockerfile.ci for the build environment those targets run
# inside.
# ---------------------------------------------------------------------------

# Build the `base` stage of Dockerfile.ci as a tagged image, ready
# for `docker run` invocations against a mounted source tree. The
# `--load` is required for `docker run` to find the image in the
# local store.
.PHONY: ci-image
ci-image:
	$(OCI_BIN) buildx build --target=base -t $(CI_IMAGE) -f $(CI_DOCKERFILE) --load $(CI_BUILDX_CACHE) .

# Reproduce the workflow's build job locally. Verifies that the
# bpfman binary itself compiles -- separable from `ci-test`
# because `go test ./...` does not exercise the cmd/bpfman link
# path. STATIC=1 is intentionally omitted: static linking is a
# property we need when crossing the container/runner boundary
# (i.e. when we extract the artefact). Here the binary is
# verified-then-discarded inside the container, so the dynamic
# build is sufficient and avoids the noisy glibc-static
# warnings. The static-link path stays covered by the e2e jobs
# (which do extract) and by image.yaml (which ships).
#
# Go's build and module caches are persisted in named docker
# volumes so subsequent runs benefit from incremental compile.
# CI runners are ephemeral and don't benefit from this directly
# (each runner starts with empty volumes); the volumes are
# specifically for local iteration speed.
#
# `make clean-bpf` runs first because $(CI_RUN) bind-mounts the
# source tree (-v $(CURDIR):/src), and bind mounts bypass the
# dockerignore filter -- without it, host-built dispatcher/*.bpf.o
# would be visible inside the container, and Make would skip the
# in-container BPF compile based on host mtimes. The compile is
# cheap (~1-2s) and the Go cache volumes carry the rest of the
# incremental story. ci-test and ci-lint apply the same prefix
# for the same reason.
.PHONY: ci-build
ci-build: ci-image
	$(CI_RUN) make STAMP=1 clean-bpf bpfman-build bpfman-shell-build

# Reproduce the workflow's check-vendor job locally. Verifies
# go.mod / go.sum / vendor are tidy. Runs on the host (no
# container) to match the GH job, which uses actions/setup-go
# directly on the runner rather than the bpfman-ci image. Like
# the upstream CI job this assumes a clean tree; commit or
# stash work-in-progress changes before invoking, otherwise
# `git diff --exit-code` will fail on them.
.PHONY: ci-check-vendor
ci-check-vendor:
	go mod tidy
	go mod vendor
	git diff --exit-code

# Reproduce the workflow's check-fmt job locally. Same host /
# clean-tree contract as ci-check-vendor.
.PHONY: ci-check-fmt
ci-check-fmt:
	$(MAKE) bpfman-fmt
	git diff --exit-code

# Reproduce the workflow's check-goimports job locally. Same host /
# clean-tree contract as ci-check-fmt.
.PHONY: ci-check-goimports
ci-check-goimports:
	$(MAKE) bpfman-goimports
	git diff --exit-code

# Reproduce the workflow's lint job locally. Runs the full
# `make lint` umbrella (golangci-lint + hadolint + shellcheck +
# checkmake) inside the CI container.
.PHONY: ci-lint
ci-lint: ci-image
	$(CI_RUN) make clean-bpf lint

# Reproduce the workflow's check-vet job locally. Runs the vet
# pass (e2e/bpfman_ns) inside the CI container
# so the BPF embeds and CGO toolchain match what CI sees. Symmetric
# with ci-check-fmt and ci-check-vendor: a separate gate, not a
# side effect of bpfman-build.
.PHONY: ci-check-vet
ci-check-vet: ci-image
	$(CI_RUN) make clean-bpf bpfman-vet

# Reproduce the workflow's check-gofix job locally. Applies the Go
# modernisers inside the CI container, which supplies the clang,
# libbpf, and CGO toolchain the package load needs, then asserts the
# tree is unchanged. A non-empty diff means the branch carries
# unmodernised code; run `make bpfman-gofix` to apply the fixes. The
# container bind-mounts the source, so fixes applied inside are
# visible to the host git-diff, matching the ci-check-fmt contract.
.PHONY: ci-check-gofix
ci-check-gofix: ci-image
	$(CI_RUN) make clean-bpf bpfman-gofix
	git diff --exit-code

# Reproduce the workflow's check-bpfman-shell-fmt job locally. Formats
# the canonical .bpfman files inside the CI container -- which supplies
# the clang and libbpf toolchain bpfman-shell's in-process manager
# needs to build -- then asserts the tree is unchanged. A non-empty
# diff means a .bpfman file is not canonically formatted; run
# `make bpfman-shell-fmt` to apply it. Same bind-mount / host git-diff
# contract as ci-check-gofix.
.PHONY: ci-check-bpfman-shell-fmt
ci-check-bpfman-shell-fmt: ci-image
	$(CI_RUN) make clean-bpf bpfman-shell-fmt
	git diff --exit-code

# Reproduce the workflow's unit-test job locally. Source is
# mounted into the container so the test process sees the
# current working tree exactly as a host build would. Same Go
# cache volumes as ci-build for incremental-compile speed.
.PHONY: ci-test
ci-test: ci-image
	$(CI_RUN) make clean-bpf test PARALLEL=1 STATIC=1 RACE=$(RACE)

# Reproduce the workflow's e2e job locally. The `e2e-export`
# stage produces a hermetic bundle at $(CI_E2E_BUNDLE); the
# e2e.test binary and its on-disk BPF object tree are then run on
# the host with sudo so it has the kernel privileges the e2e suite
# needs.
.PHONY: ci-test-e2e
ci-test-e2e:
	$(RM) -r $(CI_E2E_BUNDLE)
	$(OCI_BIN) buildx build --target=e2e-export --output type=local,dest=$(CI_E2E_BUNDLE) -f $(CI_DOCKERFILE) --build-arg RACE=$(RACE) --build-arg EXTRA_TAGS=$(EXTRA_TAGS) $(CI_BUILDX_CACHE) .
	$(MAKE) e2e-kmod-reload
	sudo BPFMAN_E2E_BYTECODE_DIR=$(abspath $(CI_E2E_BUNDLE)/e2e) $(call forward-env,BPFMAN_E2E_ISOLATED_RUNTIME) $(CI_E2E_BUNDLE)/bin/e2e.test -test.v -test.count=$(STRESS_COUNT) $(if $(filter-out 0,$(PARALLEL)),-test.parallel $(PARALLEL))

# Reproduce the workflow's e2e-scripts job locally. The .bpfman
# scripts under e2e/scripts/ are driven by the Go test binary
# at bin/e2e-scripts.test, which execs the bundle's
# bpfman-shell per subtest. The bundle's binaries + testdata are
# extracted into the source tree (the layout matches); the host
# then builds and reloads the e2e kmod, after which the scripts
# run via `make run-e2e-scripts`, which sudo-execs
# e2e-scripts.test with BIN_DIR pointing at the bundle so
# bpfman-shell resolves despite sudo's secure_path.
#
# Pre-clean the exact set of paths the bundle is about to write
# (bin/bpfman, bin/bpfman-shell, bin/e2e.test, bin/e2e-scripts.test,
# and the BPF object tree). buildx --output overwrites individual
# files but does not prune anything stale, so leftover artefacts
# from a previous run could otherwise mask "didn't rebuild" bugs.
# golangci-lint under bin/ is preserved -- it has its own rule
# and re-fetching over the network is slow.
.PHONY: ci-test-e2e-scripts
ci-test-e2e-scripts:
	$(RM) bin/bpfman bin/bpfman-shell bin/e2e.test bin/e2e-scripts.test
	$(MAKE) clean-bpf
	$(OCI_BIN) buildx build --target=e2e-export --output type=local,dest=. -f $(CI_DOCKERFILE) --build-arg RACE=$(RACE) --build-arg EXTRA_TAGS=$(EXTRA_TAGS) $(CI_BUILDX_CACHE) .
	$(MAKE) e2e-kmod-reload
	$(MAKE) run-e2e-scripts

# Reproduce the workflow's gRPC parallel e2e job locally. The test
# binary, bpfman, and BPF object tree live in the bundle path; tell
# run-e2e-grpc where to find all three via the E2E_GRPC_* make
# variables. Mirrors ci-test-e2e (which also runs e2e.test directly
# from the bundle) rather than ci-test-e2e-scripts (which has to
# extract because the .bpfman scripts reference testdata via
# relative paths on disk).
.PHONY: ci-test-e2e-grpc
ci-test-e2e-grpc:
	$(RM) -r $(CI_E2E_BUNDLE)
	$(OCI_BIN) buildx build --target=e2e-export --output type=local,dest=$(CI_E2E_BUNDLE) -f $(CI_DOCKERFILE) --build-arg RACE=$(RACE) --build-arg EXTRA_TAGS=$(EXTRA_TAGS) $(CI_BUILDX_CACHE) .
	$(MAKE) e2e-kmod-reload
	$(MAKE) run-e2e-grpc E2E_GRPC_TEST_BIN=$(CI_E2E_BUNDLE)/bin/e2e-grpc.test E2E_GRPC_BPFMAN_BIN=$(CI_E2E_BUNDLE)/bin/bpfman E2E_GRPC_BYTECODE_DIR=$(abspath $(CI_E2E_BUNDLE)/e2e)

# Umbrella: run every CI pipeline locally. Cheap checks first
# (vendor/fmt) so failures surface fast; build before tests so
# the test job's container has a populated Go cache; e2e last.
#
# Run sequentially. CI gives each job its own runner, so the
# upstream workflow can fan out the e2e jobs in parallel; on a
# single dev box that fan-out collides on shared kernel state --
# bpffs mounts, dispatcher slot tables, the global program-id
# space, and the inode that uprobes attach to (for which both
# suites use the same e2e.test binary). Symptoms range from spurious
# attach failures to shell counter assertions seeing the other
# suite's events. Don't `make -j ci-test-e2e ci-test-e2e-scripts`
# locally, and don't run them in two shells at once.
.PHONY: ci
ci: ci-check-vendor ci-check-fmt ci-check-goimports ci-check-vet ci-check-gofix ci-check-bpfman-shell-fmt ci-build ci-lint ci-test ci-test-e2e ci-test-e2e-scripts ci-test-e2e-grpc
