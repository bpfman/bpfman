# eBPF Bytecode Image Specifications

## Introduction

The eBPF Bytecode Image specification defines how to package eBPF bytecode
as container images. The initial primary use case focuses on the containerization
and deployment of eBPF programs within container orchestration systems such as
Kubernetes, where it is necessary to provide a portable way to distribute
bytecode to all nodes which need it.

## Specifications

We provide two distinct spec variants here to ensure interoperability with existing registries
and packages which do not support the new custom media types defined here.

- [custom-data-type-spec](#custom-oci-compatible-spec)
- [backwards-compatable-spec](#backwards-compatible-oci-compliant-spec)

## Backwards compatible OCI compliant spec

This variant makes use of existing OCI conventions to represent eBPF Bytecode
as container images.

### Image Layers

The container images following this variant must contain exactly one layer who's
media type is one of the following:

- `application/vnd.oci.image.layer.v1.tar+gzip` or the [compliant](https://github.com/opencontainers/image-spec/tree/main/media-types.md#applicationvndociimagelayerv1targzip) `application/vnd.docker.image.rootfs.diff.tar.gzip`

Additionally the image layer must contain a valid eBPF object file (generally containing
a `.o` extension) placed at the root of the layer `./`.

### Image Labels

To provide relevant metadata regarding the bytecode to any consumers, some relevant labels
**MUST** be defined on the image.

These labels are dynamic and defined as follows:

- `io.ebpf.programs`: A label which defines the eBPF programs stored in the bytecode image.
   The value of the label is a list which must contain a valid JSON object with
   Key's specifying the program name, and values specifying the program type i.e:
   "{ "pass" : "xdp" , "counter" : "tc", ...}".

- `io.ebpf.maps`: A label which defines the eBPF maps stored in the bytecode image.
   The value of the label is a list which must contain a valid JSON object with
   Key's specifying the map name, and values specifying the map type i.e:
   "{ "xdp_stats_map" : "per_cpu_array", ...}".

### Building a Backwards compatible OCI compliant image

Bpfman does not provide wrappers around compilers like clang since many eBPF
libraries (i.e aya, libbpf, cilium-ebpf) already do so, meaning users are expected
to pass in the correct ebpf program bytecode for the appropriate platform. However,
bpfman does provide a few image builder commands to make this whole process easier.

Example Containerfiles for single-arch and multi-arch can be found at `Containerfile.bytecode` and `Containerfile.bytecode.multi.arch`.

#### Host Platform Architecture Image Build

```console
bpfman image build -b ./examples/go-xdp-counter/bpf_x86_bpfel.o -f Containerfile.bytecode --tag quay.io/<USER>/go-xdp-counter
```

Where `./examples/go-xdp-counter/bpf_x86_bpfel.o` is the path to the bytecode object file.

Users can also use `skopeo` to ensure the image follows the
backwards compatible version of the spec:

- `skopeo inspect` will show the correctly configured labels stored in the
  configuration layer (`application/vnd.oci.image.config.v1+json`) of the image.

```bash
skopeo inspect docker://quay.io/bpfman-bytecode/go-xdp-counter
{
    "Name": "quay.io/bpfman-bytecode/go-xdp-counter",
    "Digest": "sha256:e8377e94c56272937689af88a1a6231d4d594f83218b5cda839eaeeea70a30d3",
    "RepoTags": [
        "latest"
    ],
    "Created": "2024-05-30T09:17:15.327378016-04:00",
    "DockerVersion": "",
    "Labels": {
        "io.ebpf.maps": "{\"xdp_stats_map\":\"per_cpu_array\"}",
        "io.ebpf.programs": "{\"xdp_stats\":\"xdp\"}"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:c0d921d3f0d077da7cdfba8c0240fb513789e7698cdf326f80f30f388c084cff"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:c0d921d3f0d077da7cdfba8c0240fb513789e7698cdf326f80f30f388c084cff",
            "Size": 2656,
            "Annotations": null
        }
    ],
    "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    ]
}
```

#### Multi-Architecture Image build

```console
bpfman image build -t quay.io/bpfman-bytecode/go-xdp-counter-multi --container-file ./Containerfile.bytecode.multi.arch --bc-amd64-el ./examples/go-xdp-counter/bpf_arm64_bpfel.o --bc-s390x-eb ./examples/go-xdp-counter/bpf_s390_bpfeb.o
```

To better understand the available architectures users can use `podman manifest-inspect`

```console
podman manifest inspect quay.io/bpfman-bytecode/go-xdp-counter:test-manual-build
{
    "schemaVersion": 2,
    "mediaType": "application/vnd.docker.distribution.manifest.list.v2+json",
    "manifests": [
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "size": 478,
            "digest": "sha256:aed62d2e5867663fac66822422512a722003b40453325fd873bbb5840d78cba9",
            "platform": {
                "architecture": "amd64",
                "os": "linux"
            }
        },
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "size": 478,
            "digest": "sha256:a348fe2f26dc0851518d8d82e1049d2c39cc2e4f37419fe9231c1967abc4828c",
            "platform": {
                "architecture": "arm64",
                "os": "linux"
            }
        },
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "size": 478,
            "digest": "sha256:d5c5d41d2d21e0cb5fb79fe9f343e540942c9a1657cf0de96b8f63e43d369743",
            "platform": {
                "architecture": "ppc64le",
                "os": "linux"
            }
        },
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "size": 478,
            "digest": "sha256:7915c83838d73268690381b313fb84b5509912aa351c98c78204584cced50efd",
            "platform": {
                "architecture": "s390x",
                "os": "linux"
            }
        },
    ]
}
```

## Custom OCI compatible spec

This variant of the eBPF bytecode image spec uses custom OCI medium types
to represent eBPF bytecode as container images. Many toolchains and registries
may not support this yet.

TODO https://github.com/bpfman/bpfman/issues/1162
