{ dockerTools
, bpfman
, bashInteractive
, coreutils
, iproute2
, bpftools
, procps
, strace
, tag ? "dev"
}:

# Pure-Nix OCI image of bpfman. Built without a Docker daemon at
# build time; byte-reproducible given the same flake inputs.
#
# The reason this exists alongside `build-image-dev` is convenience
# for debugging: each entry in the `contents` list is a layer added
# to the image, so adding strace, gdb, bpftrace, perf, lsof, etc.
# is literally one line here -- no Dockerfile rewrite, no apt-get,
# no rebuild of a base image. Compare to Dockerfile.bpfman.dev,
# which is deliberately minimal (just the binary on ubi9-minimal)
# and where adding tools means modifying the runtime base or
# layering on a second image.
#
# Local usage:
#
#   nix build .#bpfman-image          # produces ./result tarball
#   docker load < result              # or `podman load`
#   docker run --rm bpfman:dev version
#
# The Makefile target `build-image-nix` wraps both steps and skips
# the result symlink for a cleaner workspace.
#
# Distinct from:
#   build-image-dev   single-arch local Docker build of the host
#                     binary onto ubi9-minimal; meant for cluster
#                     iteration where you want `kubectl exec`.
#                     Image tag collides on purpose -- both
#                     produce bpfman:dev, last writer wins.
#   build-image       buildx-driven publish path used by CI; carries
#                     cosign signing and SBOM attestation, neither
#                     of which this image builder reproduces.

dockerTools.buildLayeredImage {
  name = "bpfman";
  inherit tag;

  # Each entry becomes its own layer, so a contributor adding a
  # debug tool only invalidates that tool's layer, not the rest of
  # the image. Keep this list short and oriented at runtime
  # debugging; build-time tools (clang, llvm, go) do not belong
  # here.
  contents = [
    bpfman

    # Shell + GNU coreutils for `docker exec -it <id> bash`.
    bashInteractive
    coreutils

    # eBPF and network-plane inspection. bpftool is the obvious one;
    # iproute2 supplies `ip`, `tc`, `ss` -- what you reach for first
    # when something looks wrong with attachment or traffic.
    bpftools
    iproute2

    # General-purpose runtime debugging.
    procps
    strace
  ];

  # bpfman-operator and Dockerfile.bpfman.dev both invoke the
  # binary as /bpfman (rooted, not /bin/bpfman). The Nix package
  # installs to $out/bin/bpfman, so symlink /bpfman to that path
  # at image-assembly time to keep the contract identical.
  extraCommands = ''
    ln -s /bin/bpfman bpfman
  '';

  config = {
    Entrypoint = [ "/bpfman" ];
  };
}
