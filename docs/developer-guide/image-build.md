# bpfman Container Images

Container images for the `bpfman` and `bpfctl` binaries are automatically built and
pushed to `quay.io/bpfman` whenever code is merged into the `main` branch of the
`github.com/bpfman/bpfman` repository under the `:latest` tag.

## Building the images locally

### bpfman

```sh
docker build -f /packaging/container-deployment/Containerfile.bpfman . -t bpfman:local
```

### bpfctl

```sh
docker build -f /packaging/container-deployment/Containerfile.bpfctl . -t bpfctl:local
```

## Running locally in container

### bpfman

```sh
sudo docker run --init --privileged --net=host -v /etc/bpfman/certs/:/etc/bpfman/certs/ -v /sys/fs/bpf:/sys/fs/bpf quay.io/bpfman/bpfman:latest
```

### bpfctl 

```sh
sudo docker run --init --privileged --net=host -v /etc/bpfman/certs/:/etc/bpfman/certs/ quay.io/bpfman/bpfctl:latest <COMMANDS>
```
