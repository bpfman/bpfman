{
  description = "go-bpfman development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system:
        f (import nixpkgs { inherit system; }));
    in
    {
      packages = nixpkgs.lib.genAttrs systems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in rec {
        default = bpfman;
        # Statically linked against a scratch-compatible base; the
        # portable binary you can cp to any linux-x86_64 or
        # linux-aarch64 host regardless of its glibc version.
        bpfman = pkgs.callPackage ./nix/package.nix {
          inherit self;
          static = true;
        };
        # Nix-native dynamic build: lighter, quicker link, but the
        # produced binary's interpreter points into this Nix store
        # so it only runs on hosts that can resolve it.
        bpfman-dynamic = pkgs.callPackage ./nix/package.nix {
          inherit self;
          static = false;
        };
        # Pure-Nix OCI image: the static `bpfman` plus a small
        # debug toolkit (bash, coreutils, bpftool, iproute2, procps,
        # strace), built without a Docker daemon. See nix/image.nix
        # for rationale; `make build-image-nix` for the
        # build-and-load convenience target.
        bpfman-image = pkgs.callPackage ./nix/image.nix {
          inherit bpfman;
        };
      });


      devShells = forAllSystems (pkgs: rec {
        default = pkgs.mkShell {
          packages = with pkgs; [
            # Go toolchain and CGO.
            gcc
            git
            gnumake
            go_1_25
            pkg-config


            parallel

            # BPF build toolchain. Use clang-unwrapped: the cc-
            # wrapper warns "supplying --target bpfel != x86_64-
            # unknown-linux-gnu may not work correctly" because it
            # injects host-target -isystem (glibc-dev, ncurses,
            # zlib, ...), --gcc-toolchain, NIX_LDFLAGS, and
            # hardening flags (-fzero-call-used-regs, -fstack-
            # protector-strong) that clang either rejects or
            # ignores for bpfel. The unwrapped clang has none of
            # that. We supply the only header set BPF actually
            # needs from system paths (linuxHeaders) via CPATH in
            # the shellHook below; libbpf headers come via
            # pkg-config --cflags libbpf in the BPF Makefiles.
            bpftools
            llvmPackages.clang-unwrapped
            libbpf
            linuxHeaders
            llvm
            pahole

            # Proto/gRPC codegen (make bpfman-proto) pins its own
            # toolchain: protoc is downloaded at a fixed release
            # (PROTOC_VERSION) and the protoc-gen-* plugins are built
            # from Makefile-pinned versions, so none come from nixpkgs.

            # Lint, format, misc. taplo and yamllint check the
            # tree's toml and yaml formatting conventions;
            # clang-format for the C convention comes with clang
            # above.
            checkmake
            golangci-lint
            hadolint
            iproute2
            jq
            shellcheck
            taplo
            yamllint

            # Load generation for chasing async-teardown lag in
            # the e2e suite and for reproducing the timing flakes
            # contended CI runners hit. Both are kept: stress is
            # the simpler `--cpu N` shape from the original
            # package, stress-ng is the richer modern superset.
            # Use either alongside `bin/e2e.test -test.count N` or
            # a `go test -count=N -race` loop on a specific test
            # to surface races that arm64 CI hits naturally on
            # slower runners.
            stress
            stress-ng

            # SQLite CLI for inspecting the store database.
            sqlite
            sqlite.dev
          ];

          shellHook = ''
            # Build env values (CGO_ENABLED, linker mode, STATIC) are
            # owned by the Makefile so `make` behaves identically on
            # Fedora and in this Nix shell. The hook below only
            # handles Nix-shell-specific concerns (HOME, CPATH).
            #
            # `nix develop --ignore-env` (--pure) strips HOME, which
            # Go uses to locate ~/.cache/go-build and ~/go/pkg/mod
            # (and which `~`-using tools like git also need). When
            # HOME is absent, give it a per-user /tmp fallback so
            # caches still land somewhere writable. In normal
            # interactive use direnv inherits HOME from the user's
            # shell, the conditional is a no-op, and Go uses the
            # standard locations -- no `.cache/` polluting the
            # checkout.
            if [ -z "''${HOME:-}" ]; then
              export HOME="/tmp/nix-shell-home-$UID"
              mkdir -p "$HOME"
            fi
            export TMPDIR="''${TMPDIR:-/tmp}"
            # CPATH supplies linuxHeaders to the unwrapped clang
            # invocations done by the BPF Makefiles. clang reads
            # CPATH like gcc does, treating each entry as an
            # additional system-include directory. libbpf is
            # picked up separately via `pkg-config --cflags
            # libbpf`. Without this, unwrapped clang would fall
            # through to /usr/include/linux on a non-NixOS host
            # and silently use the host kernel headers.
            export CPATH="${pkgs.linuxHeaders}/include''${CPATH:+:}''${CPATH:-}"
          '';
        };

        # Static-link shell. glibc.static must stay out of the
        # default shell: its `-L` entry contains only archives, so
        # leaving it on the link path would make ld pick libc.a
        # over libc.so for ordinary dynamic builds and emit glibc's
        # NSS dlopen-at-runtime warnings. Keeping the two shells
        # separate makes the linker inputs unambiguous in either
        # direction; .envrc picks which one the daily loop uses.
        static = default.overrideAttrs (old: {
          buildInputs = (old.buildInputs or [ ]) ++ [ pkgs.glibc.static ];
          shellHook = (old.shellHook or "") + ''
            export STATIC=1
          '';
        });
      });
    };
}
