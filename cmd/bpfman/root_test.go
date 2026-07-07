package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bpfman/bpfman/lock"
)

//nolint:paralleltest // mutates the os.Args process global via newCLIForArgs; cannot run in parallel.
func TestSelectedCommandAllowsRootless(t *testing.T) {
	for _, args := range [][]string{
		{"bpfman", "version"},
		{"bpfman", "image", "build", "example.test/x:latest", "x.o"},
		{"bpfman", "image", "build", "example.test/x:latest", "linux/amd64=x.o"},
		{"bpfman", "image", "generate-build-args", "x.o"},
		{"bpfman", "image", "generate-build-args", "linux/amd64=x.o"},
		{"bpfman", "image", "inspect", "example.test/x:latest"},
		{"bpfman", "image", "verify", "example.test/x:latest"},
		{
			"bpfman", "image", "verify", "example.test/x:latest",
			"--certificate-identity", "signer@example.com",
			"--certificate-oidc-issuer", "https://github.com/login/oauth",
		},
	} {
		t.Run(args[1], func(t *testing.T) {
			cli := newCLIForArgs(t, args)
			if !selectedCommandAllowsRootless(cli.kctx) {
				t.Fatalf("selectedCommandAllowsRootless(%v) = false, want true", args)
			}
		})
	}
}

//nolint:paralleltest // mutates the os.Args process global via newCLIForArgs; cannot run in parallel.
func TestSelectedCommandRequiresRootByDefault(t *testing.T) {
	for _, args := range [][]string{
		{"bpfman", "program", "load", "file", "x.o", "--programs", "xdp:pass"},
		{"bpfman", "program", "load", "image", "example.test/x:latest", "--programs", "xdp:pass"},
		{"bpfman", "program", "list"},
		{"bpfman", "link", "list"},
		{"bpfman", "serve"},
	} {
		t.Run(args[1], func(t *testing.T) {
			cli := newCLIForArgs(t, args)
			if selectedCommandAllowsRootless(cli.kctx) {
				t.Fatalf("selectedCommandAllowsRootless(%v) = true, want false", args)
			}
		})
	}
}

//nolint:paralleltest // mutates os.Args and process environment.
func TestNewCLIReadsConfigFromEnv(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bpfman.toml")
	if err := os.WriteFile(configPath, nil, 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	t.Setenv("BPFMAN_CONFIG", configPath)

	oldArgs := os.Args
	os.Args = []string{"bpfman", "version"}
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	cli, err := NewCLI()
	if err != nil {
		t.Fatalf("NewCLI: %v", err)
	}

	if cli.Config != configPath {
		t.Fatalf("Config = %q, want %q", cli.Config, configPath)
	}
}

func TestFormatErrorAddsLockTimeoutHint(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("open runtime: %w", &lock.TimeoutError{
		Path:    "/run/bpfman/.lock",
		Timeout: 30 * time.Second,
	})

	got := (&CLI{}).formatError(err).Error()
	want := "timed out waiting for lock /run/bpfman/.lock (--lock-timeout=30s)"
	if got != want {
		t.Fatalf("formatError() = %q, want %q", got, want)
	}
}

func newCLIForArgs(t *testing.T, args []string) *CLI {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "bpfman.toml")
	if err := os.WriteFile(configPath, nil, 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	args = append([]string{args[0], "--config", configPath}, args[1:]...)

	oldArgs := os.Args
	os.Args = append([]string(nil), args...)
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	cli, err := NewCLI()
	if err != nil {
		t.Fatalf("NewCLI(%v): %v", args, err)
	}

	return cli
}
