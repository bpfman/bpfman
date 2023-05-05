#####################################################
#################### rust-build #####################
#####################################################
FROM rust:1 as rust-build

RUN --mount=type=cache,target=/var/cache/apt \
    apt-get update && apt-get install -y\
        git\
        clang\
        protobuf-compiler\
        libelf-dev\
        gcc-multilib\
        musl-tools

RUN git clone https://github.com/libbpf/libbpf --depth=1 --branch v0.8.0 /usr/src/bpfd/libbpf

RUN rustup target add x86_64-unknown-linux-musl

WORKDIR /usr/src/bpfd
COPY ./ /usr/src/bpfd

#####################################################
################### bpfctl-build ####################
#####################################################
FROM rust-build as bpfctl-build

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    cargo build -p bpfctl --release --target x86_64-unknown-linux-musl

#####################################################
###################### bpfctl #######################
#####################################################
FROM scratch as bpfctl
COPY --from=bpfctl-build  /usr/src/bpfd/target/x86_64-unknown-linux-musl/release/bpfctl .
ENTRYPOINT ["./bpfctl"]

#####################################################
##################### bpfd-build ####################
#####################################################
FROM rust-build as bpfd-build

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    cargo xtask build-ebpf --release --libbpf-dir /usr/src/bpfd/libbpf
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    cargo build -p bpfd --release --target x86_64-unknown-linux-musl

#####################################################
###################### bpfd #########################
#####################################################
FROM scratch as bpfd
COPY --from=bpfd-build /usr/src/bpfd/target/x86_64-unknown-linux-musl/release/bpfd .
ENTRYPOINT ["./bpfd"]

#####################################################
###################### local ########################
#####################################################
FROM fedora:36 as development

RUN  --mount=type=cache,target=/var/cache/dnf \
    dnf update -y && dnf -y install bpftool tcpdump

COPY --from=bpfd-build  ./usr/src/bpfd/bpfd .
COPY --from=bpfctl-build  ./usr/src/bpfd/bpfctl .

ENTRYPOINT ["./bpfd"]

#####################################################
###################### bundle #######################
#####################################################
FROM scratch as bundle

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=bpfd-operator
LABEL operators.operatorframework.io.bundle.channels.v1=alpha
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.26.0
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v3

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# Copy files to locations specified by labels.
COPY bpfd-operator/bundle/manifests /manifests/
COPY bpfd-operator/bundle/metadata /metadata/
COPY bpfd-operator/bundle/tests/scorecard /tests/scorecard/

#####################################################
###################### go-base ######################
#####################################################
FROM golang:1.19 as go-base
ARG TARGETOS
ARG TARGETARCH

WORKDIR /usr/src/bpfd/

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod\
    go mod download

# Copy the go source
COPY . .

#####################################################
################# bpfd-agent-build ##################
#####################################################
FROM go-base as bpfd-agent-build

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
WORKDIR /usr/src/bpfd/bpfd-operator
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o bpfd-agent ./cmd/bpfd-agent/main.go

#####################################################
#################### bpfd-agent #####################
#####################################################

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
# Uncomment for debug build
# FROM gcr.io/distroless/static:debug
FROM gcr.io/distroless/static:nonroot as bpfd-agent
WORKDIR /
COPY --from=bpfd-agent-build /usr/src/bpfd/bpfd-operator/bpfd-agent .
USER 65532:65532

ENTRYPOINT ["/bpfd-agent"]

#####################################################
############### bpfd-operator-build #################
#####################################################
FROM go-base as bpfd-operator-build

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
WORKDIR /usr/src/bpfd/bpfd-operator
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o bpfd-operator ./cmd/bpfd-operator/main.go

#####################################################
################### bpfd-operator ###################
#####################################################

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
# Uncomment for debug build
# FROM gcr.io/distroless/static:debug
FROM gcr.io/distroless/static:nonroot as bpfd-operator
WORKDIR /
COPY --from=bpfd-operator-build /usr/src/bpfd/bpfd-operator/config/bpfd-deployment/daemonset.yaml ./config/bpfd-deployment/daemonset.yaml
COPY --from=bpfd-operator-build /usr/src/bpfd/bpfd-operator/bpfd-operator .
USER 65532:65532

ENTRYPOINT ["/bpfd-operator"]
