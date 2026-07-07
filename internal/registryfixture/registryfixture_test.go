package registryfixture

import (
	"strings"
	"testing"
)

func TestHostUsesEnvOverride(t *testing.T) {
	t.Setenv(RegistryEnv, "https://registry.example.test")

	host, err := Host()
	if err != nil {
		t.Fatalf("Host returned error: %v", err)
	}
	if host != "registry.example.test" {
		t.Fatalf("Host = %q, want registry.example.test", host)
	}
}

func TestURLPreservesEnvScheme(t *testing.T) {
	t.Setenv(RegistryEnv, "http://registry.example.test")

	u, err := URL()
	if err != nil {
		t.Fatalf("URL returned error: %v", err)
	}
	if u != "http://registry.example.test" {
		t.Fatalf("URL = %q, want http://registry.example.test", u)
	}
}

func TestURLUsesHTTPForLoopback(t *testing.T) {
	t.Setenv(RegistryEnv, "127.0.0.1:5000")

	u, err := URL()
	if err != nil {
		t.Fatalf("URL returned error: %v", err)
	}
	if u != "http://127.0.0.1:5000" {
		t.Fatalf("URL = %q, want http://127.0.0.1:5000", u)
	}
}

func TestStartSharedStartsRegistry(t *testing.T) {
	t.Parallel()

	host, closeFn, err := StartShared()
	if err != nil {
		t.Fatalf("StartShared returned error: %v", err)
	}
	t.Cleanup(closeFn)
	if host == "" {
		t.Fatal("host is empty")
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		t.Fatalf("host = %q, want host without scheme", host)
	}
}

func TestRefReturnsAliasReference(t *testing.T) {
	t.Parallel()

	ref, err := Ref("Explicit XDP Pass")
	if err != nil {
		t.Fatalf("Ref returned error: %v", err)
	}
	prefix := RegistryAlias + "/" + RepositoryPrefix + "/explicit-xdp-pass:"
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("Ref = %q, want prefix %q", ref, prefix)
	}
}
