package oci

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialStorePathsUseCurrentProcessContext(t *testing.T) {
	t.Setenv("REGISTRY_AUTH_FILE", "/home/aim/.config/containers/auth.json")
	t.Setenv("SUDO_USER", "aim")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/0")
	t.Setenv("HOME", "/root")
	t.Setenv("DOCKER_CONFIG", "/root/docker")

	got := credentialStorePaths()
	want := []string{
		"/home/aim/.config/containers/auth.json",
		"/run/user/0/containers/auth.json",
		"/root/.config/containers/auth.json",
		"/root/docker/config.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("credentialStorePaths() = %#v, want %#v", got, want)
	}
	for _, path := range got {
		if strings.Contains(path, "/run/user/1000/") || strings.Contains(path, "/home/aim/.docker/") {
			t.Fatalf("credentialStorePaths() included implicit sudo-user path %q", path)
		}
	}
}

func TestCredentialStorePathsUseDockerConfigFallback(t *testing.T) {
	t.Setenv("REGISTRY_AUTH_FILE", "")
	t.Setenv("SUDO_USER", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("HOME", "/home/aim")
	t.Setenv("DOCKER_CONFIG", "")

	got := credentialStorePaths()
	want := []string{
		"/run/user/1000/containers/auth.json",
		"/home/aim/.config/containers/auth.json",
		"/home/aim/.docker/config.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("credentialStorePaths() = %#v, want %#v", got, want)
	}
}

func TestRegistryCredentialHelpMentionsCredentialSetup(t *testing.T) {
	t.Setenv("SUDO_USER", "root")
	got := registryCredentialHelp("quay.io")
	for _, want := range []string{"current process context", "podman login", "REGISTRY_AUTH_FILE"} {
		if !strings.Contains(got, want) {
			t.Fatalf("registryCredentialHelp() = %q, want substring %q", got, want)
		}
	}
}

func TestRegistryCredentialsFoundUsesConfiguredStore(t *testing.T) {
	authFile := writeAuthFile(t, "quay.io")
	t.Setenv("REGISTRY_AUTH_FILE", authFile)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("DOCKER_CONFIG", "")

	if !registryCredentialsFound(context.Background(), "quay.io", discardLogger()) {
		t.Fatal("registryCredentialsFound returned false, want true")
	}
	if registryCredentialsFound(context.Background(), "ghcr.io", discardLogger()) {
		t.Fatal("registryCredentialsFound returned true for missing registry")
	}
	if registryCredentialsFound(context.Background(), "127.0.0.1:5000", discardLogger()) {
		t.Fatal("registryCredentialsFound returned true for loopback registry")
	}
}

func TestCredentialStoreForGoContainerRegistryReportsMissingRegistryEntry(t *testing.T) {
	authFile := writeAuthFile(t, "quay.io")
	t.Setenv("REGISTRY_AUTH_FILE", authFile)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("DOCKER_CONFIG", "")

	ref, err := parseRegistryReference("ghcr.io/acme/private:latest")
	if err != nil {
		t.Fatalf("parseRegistryReference returned error: %v", err)
	}

	_, found, err := credentialStoreForGoContainerRegistry(context.Background(), ref, discardLogger())
	if err != nil {
		t.Fatalf("credentialStoreForGoContainerRegistry returned error: %v", err)
	}

	if found {
		t.Fatal("found = true, want false")
	}

	ref, err = parseRegistryReference("quay.io/acme/private:latest")
	if err != nil {
		t.Fatalf("parseRegistryReference returned error: %v", err)
	}

	_, found, err = credentialStoreForGoContainerRegistry(context.Background(), ref, discardLogger())
	if err != nil {
		t.Fatalf("credentialStoreForGoContainerRegistry returned error: %v", err)
	}

	if !found {
		t.Fatal("found = false, want true")
	}
}

func writeAuthFile(t *testing.T, registry string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	content := `{"auths":{"` + registry + `":{"auth":"dXNlcjpwYXNz"}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	return path
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
