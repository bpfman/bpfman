# EBPF Bytecode Image Specifications

## Introduction

The EBPF Bytecode Image specification defines how to package EBPF bytecode
as container images. The initial primary use case focuses on the containerization
and deployment of EBPF programs within container orchestration systems such as
Kubernetes, where it is necessary to provide a portable way to distribute
bytecode to all nodes which need it.

## Specifications

We provide two distinct spec variants here to ensure interoperatiblity with existing registries
and packages which do no support the new custom media types defined here.

- [custom-data-type-spec](#custom-oci-compatible-spec)
- [backwards-compatable-spec](#backwards-compatible-oci-compliant-spec)

## Backwards compatible OCI compliant spec

This variant makes use of existing OCI conventions to represent EBPF Bytecode
as container images.

### Image Layers

The container images following this variant must contain exactly one layer who's
media type is one of the following:

- `application/vnd.oci.image.layer.v1.tar+gzip` or the [compliant](https://github.com/opencontainers/image-spec/blob/main/media-types.md#applicationvndociimagelayerv1targzip) `application/vnd.docker.image.rootfs.diff.tar.gzip`

Additionally the image layer must contain a valid EBPF object file (generally containing
a `.o` extension) placed at the root of the layer `./`.

### Image Labels

To provide relevant metadata regarding the bytecode to any consumers, some relevant labels
**MUST** be defined on the image.

These labels are defined as follows:

- `io.ebpf.program_type`: The EBPF program type (i.e `xdp`,`tc`, `sockops`, ...).

- `io.ebpf.filename`: The Filename of the bytecode stored in the image.

- `io.ebpf.program_name`: The name of the EBPF Program represented in the bytecode.

- `io.ebpf.section_name`: The section name of the EBPF Program.

- `io.ebpf.kernel_version`: The Kernel version for which this bytecode was compiled
against.

### Building a Backwards compatible OCI compliant image

An Example Containerfile can be found at `/packaging/container/deployment/Containerfile.bytecode`

To use the provided templated Containerfile simply run a `podman build` command
like the following:

```bash
podman build \
 --build-arg PROGRAM_NAME=xdp_pass \
 --build-arg SECTION_NAME=pass \
 --build-arg PROGRAM_TYPE=xdp \
 --build-arg BYTECODE_FILENAME=pass.bpf.o \
 --build-arg KERNEL_COMPILE_VER=$(uname -r) \
 -f packaging/container-deployment/Containerfile.bytecode \
 /home/<USER>/bytecode -t quay.io/<USER>/xdp_pass:latest
```
Where `/home/<USER>/bytecode` is the directory the bytecode object file is located.


Users can also use `skopeo` to ensure the image follows the
backwards compatible version of the spec:

- `skopeo inspect` will show the correctly configured labels stored in the
configuration layer (`application/vnd.oci.image.config.v1+json`) of the image.

```bash
skopeo inspect docker://quay.io/astoycos/xdp_pass:latest
{
    "Name": "quay.io/<USER>/xdp_pass",
    "Digest": "sha256:db1f7dd03f9fba0913e07493238fcfaf0bf08de37b8e992cc5902775dfb9086a",
    "RepoTags": [
        "latest"
    ],
    "Created": "2022-08-14T14:27:20.147468277Z",
    "DockerVersion": "",
    "Labels": {
        "io.buildah.version": "1.26.1",
        "io.ebpf.filename": "pass.bpf.o",
        "io.ebpf.kernel_version": "5.18.6-200.fc36.x86_64",
        "io.ebpf.program_name": "xdp_counter",
        "io.ebpf.program_type": "xdp",
        "io.ebpf.section_name": "pass"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:5f6dae6f567601fdad15a936d844baac1f30c31bd3df8df0c5b5429f3e048000"
    ],
    "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    ]
}
```

- `skopeo inspect --raw` will show the correct layer type is used in the image.

```bash
skopeo inspect --raw  docker://quay.io/astoycos/xdp_pass:latest
{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:ff4108b8405a877b2df3e06f9287c509b9d62d6c241c9a5213d81a9abee80361","size":2385},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:5f6dae6f567601fdad15a936d844baac1f30c31bd3df8df0c5b5429f3e048000","size":1539}],"annotations":{"org.opencontainers.image.base.digest":"sha256:86b59a6cf7046c624c47e40a5618b383d763be712df2c0e7aaf9391c2c9ef559","org.opencontainers.image.base.name":""}}
```

## Custom OCI compatible spec

This variant of the EBPF bytecode image spec uses custom OCI medium types
to represent EBPF bytecode as container images. Many toolchains and registries
may not support this yet.

TODO(astoycos)