# bpfd Container Images

Container images for the `bpfd` and `bpfctl` binaries are automatically built and
pushed to `quay.io/bpfd` whenever code is merged into the `main` branch of the
`github.com/bpfd-dev/bpfd` repository under the `:latest` tag.

## Building the images locally

### bpfd

```sh
docker build -f /packaging/container-deployment/Containerfile.bpfd . -t bpfd:local
```

### bpfctl

```sh
docker build -f /packaging/container-deployment/Containerfile.bpfctl . -t bpfctl:local
```

## Running locally in container

### bpfd

```sh
sudo docker run --init --privileged --net=host -v /etc/bpfd/certs/:/etc/bpfd/certs/ -v /sys/fs/bpf:/sys/fs/bpf quay.io/bpfd/bpfd:latest
```

### bpfctl 

```sh
sudo docker run --init --privileged --net=host -v /etc/bpfd/certs/:/etc/bpfd/certs/ quay.io/bpfd/bpfctl:latest <COMMANDS>
```