# Supporting OCI bytecode Images across Multiple Architectures

Today bpfman has [defined a spec](../developer-guide/shipping-bytecode.md) for shipping eBPF programs via OCI images, however
it assumes the image builder explicitly knows the operating system
architecture where the program will be deployed.  Ultimately the spec needs to
support running on **any** valid linux architecture, this doc will describe
the changes needed for the specification to support such a task.

## Building eBPF programs for multiple architectures

eBPF programs only have two different completion targets for big or little endian systems.
This can be clearly seen with the clang compiler:

```console
clang -print-targets

  Registered Targets:
    ...
    bpf         - BPF (host endian)
    bpfeb       - BPF (big endian)
    bpfel       - BPF (little endian)
    ...
```

Therefore, with those two targets a single eBPF program, which is written
following [current CO-RE best practices](https://developers.redhat.com/articles/2023/10/19/ebpf-application-development-beyond-basics), can essentially be compiled
to run successfully on any applicable linux target. However, occasionally
eBPF program types rely on kernel structures which may change across various
architectures, meaning that each architecture will need it's own dedicated build.

TODO(astoycos) I want to dive into the "problem" and describe it a bit more here.

## Proposal

Today the bpfman project packages eBPF bytecode in OCI container images
using well known OCI layers and media types to ensure compatibility with existing
container registries and tooling such as quay.io and podman. Currently eBPF
program endianness is not referenced, and it is assumed to always be the host
endian of the build machine.  In order to allow a single eBPF bytecode image
to be run on any linux architecture, both big and little endian variants will need
to be packaged into each image, along with some necessary metadata to allow bpfman
to easily determine where each one is located in the image blob. To facilitate
this while maintaining backwards compatibility two new labels will be added
to the oci image manifest, which specify the file names of the two bytecode
variants.

- `io.ebpf.filename_eb`
- `io.ebpf.filename_el`

If only the existing label is present i.e `io.ebpf.filename` bpfman will assume
that the image only contains a single bytecode file and that the builder has
ensured the endianness matches the machine where the eBPF program will be deployed.

Otherwise both new labels should be specified and present, and bpfman will determine
the endianness of the machine it's currently running on and ensure the correct
bytecode is loaded.

For example:

```console
cat Containerfile.bytecode 
FROM scratch

ARG PROGRAM_NAME
ARG BPF_FUNCTION_NAME
ARG EL_BYTECODE_FILE_PATH
ARG EB_BYTECODE_FILE_PATH

COPY  $EB_BYTECODE_FILE_PATH /
COPY  $EL_BYTECODE_FILE_PATH /
LABEL io.ebpf.file_eb $EB_BYTECODE_FILE_PATH
LABEL io.ebpf.file_el $EB_BYTECODE_FILE_PATH
LABEL io.ebpf.program_name $PROGRAM_NAME
LABEL io.ebpf.bpf_function_name $BPF_FUNCTION_NAME

docker build \
 --build-arg PROGRAM_NAME=go_xdp_counter \
 --build-arg BPF_FUNCTION_NAME=xdp_stats \
 --build-arg PROGRAM_TYPE=xdp \
 --build-arg EL_BYTECODE_FILE_PATH=bpf_bpfel.o \
 --build-arg EB_BYTECODE_FILE_PATH=bpf_bpfeb.o \
 -f Containerfile.bytecode \
 ./examples/go-xdp-counter -t quay.io/astoycos/go_xdp_counter:latest

[astoycos@nfvsdn-03 bpfman]$ skopeo inspect docker://quay.io/astoycos/go_xdp_counter_multi
{
    "Name": "quay.io/astoycos/go_xdp_counter_multi",
    "Digest": "sha256:1b5d9bf983e8f636034a5b84ae6bd116288563bf21b4cc900c0550eb3e7120ca",
    "RepoTags": [
        "latest"
    ],
    "Created": "2024-05-16T13:40:45.819239992-04:00",
    "DockerVersion": "",
    "Labels": {
        "io.ebpf.bpf_function_name": "xdp_stats",
        "io.ebpf.file_eb": "bpf_bpfeb.o",
        "io.ebpf.file_el": "bpf_bpfeb.o",
        "io.ebpf.program_name": "go_xdp_counter"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:8184c4c42704a1a0717630af8caf2cc29ca0c32e966036f5cf75e0bbf0efd379",
        "sha256:894e2011156116b36d6860f544a517bc7b29039f327c87e6fef010b3888b2527"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:8184c4c42704a1a0717630af8caf2cc29ca0c32e966036f5cf75e0bbf0efd379",
            "Size": 2642,
            "Annotations": null
        },
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:894e2011156116b36d6860f544a517bc7b29039f327c87e6fef010b3888b2527",
            "Size": 2656,
            "Annotations": null
        }
    ],
    "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    ]
}
```


For most types of programs which don't rely on architecture specific kernel headers
this single bytecode image will run on on any and all linux architectures, however
there are cases where this won't work, and different header files need to be
compiled into the program for various architectures. In this case bpfman will
support the use of traditional [OCI "fat manifests"](https://github.com/opencontainers/image-spec/blob/main/manifest.md) where each image referenced in the manifest will include el and eb variants, although
the bytecode itself will be different across architectures.





