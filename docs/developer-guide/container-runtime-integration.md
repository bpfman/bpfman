## 3. Developer Documentation - Container Runtime Integration

```markdown
# Container Runtime Integration

bpfman now integrates with your local container runtime (Docker or Podman) to improve the development workflow. This document explains the implementation details and how to use this feature during development.

## Overview

Previously, bpfman maintained its own image storage database, which meant that any eBPF program container image had to be explicitly pulled from a registry, even for local development. With container runtime integration, bpfman can now use images directly from your local Docker or Podman storage.

## Implementation Details

The integration is implemented in the [ImageManager](http://_vscodecontentref_/4) in [image_manager.rs](http://_vscodecontentref_/5). Key components include:

1. **Container Runtime Detection**: Automatically detects available container runtimes (Docker, Podman)
2. **Image Lookup Flow**:
   - Checks if image exists in bpfman database first
   - If not found or policy is [Always](http://_vscodecontentref_/6), checks container runtime storage
   - Extracts bytecode and metadata from container runtime if found
   - Stores extracted data in bpfman database for faster future access
   - Falls back to direct OCI client pull if not found in runtime

## Development Workflow

The container runtime integration enables a more efficient development workflow:

### Local Development

1. Build your eBPF program container image:
   ```shell
   docker build -t my-ebpf-prog:dev .
   ```

2. Load the image into bpfman and attach it to an interface:

   ```shell
   sudo bpfman load image --image-url my-ebpf-prog:dev xdp --iface eth0
   ```

3. Alternatively, use the following command to load the image:

   ```shell
   kind load image my-ebpf-prog:dev
   ```

4. Define your eBPF program using the following YAML configuration:

   ```yaml
   apiVersion: bpfman.io/v1alpha1
   kind: XdpProgram
   metadata:
     name: my-ebpf-program
   spec:
     bpfProgram:
       image: my-ebpf-prog:dev
       # ...
   ```
