// Package registryfixture owns the anonymous OCI registry fixture used by
// bpfman-shell image e2e scripts.
package registryfixture

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/go-containerregistry/pkg/registry"
)

const (
	// RegistryAlias is the stable host name scripts use before the shell
	// rewrites it to the live loopback registry.
	RegistryAlias = "bpfman-e2e-registry.example.com"
	// RepositoryPrefix is the repository namespace reserved for e2e images.
	RepositoryPrefix = "bpfman-e2e"
	// RegistryEnv names the environment variable whose value, when
	// non-empty, overrides the backing registry host for this process.
	RegistryEnv = "BPFMAN_E2E_IMAGE_REGISTRY"
)

var (
	lazyRegistryMu sync.Mutex
	lazyRegistry   *httptest.Server
	lazyHost       string
	refSeq         atomic.Uint64
)

// Host returns the backing registry host. An explicit RegistryEnv wins; when it
// is unset a process-local anonymous loopback registry is started lazily.
func Host() (string, error) {
	if registryHost := os.Getenv(RegistryEnv); registryHost != "" {
		return normaliseHost(registryHost)
	}

	lazyRegistryMu.Lock()
	defer lazyRegistryMu.Unlock()

	if lazyHost != "" {
		return lazyHost, nil
	}

	server, host, err := newRegistry()
	if err != nil {
		return "", err
	}
	lazyRegistry = server
	lazyHost = host
	return lazyHost, nil
}

// URL returns the backing registry URL. Loopback hosts use http because the
// fixture registry is plain HTTP; non-loopback overrides default to https.
func URL() (string, error) {
	if registryHost := os.Getenv(RegistryEnv); registryHost != "" {
		if strings.HasPrefix(registryHost, "http://") || strings.HasPrefix(registryHost, "https://") {
			host, err := normaliseHost(registryHost)
			if err != nil {
				return "", err
			}
			u, _ := url.Parse(registryHost)
			return u.Scheme + "://" + host, nil
		}
	}
	host, err := Host()
	if err != nil {
		return "", err
	}
	scheme := "https"
	if isLoopbackRegistry(host) {
		scheme = "http"
	}
	return scheme + "://" + host, nil
}

// StartShared starts an anonymous registry and returns its host together
// with a cleanup function that shuts the registry down. The registry's
// lifecycle is the caller's, managed through that function rather than
// through Close. The e2e runner uses it so child bpfman-shell processes
// share one registry through RegistryEnv.
func StartShared() (string, func(), error) {
	server, host, err := newRegistry()
	if err != nil {
		return "", nil, err
	}
	return host, server.Close, nil
}

// Close shuts down the lazily-started registry owned by this process --
// the one Host starts on demand -- and clears the cached host. It leaves
// a registry created by StartShared untouched, and is a no-op when no
// lazy registry has been started.
func Close() {
	lazyRegistryMu.Lock()
	defer lazyRegistryMu.Unlock()

	if lazyRegistry != nil {
		lazyRegistry.Close()
		lazyRegistry = nil
		lazyHost = ""
	}
}

// Ref returns a unique image reference in the e2e repository namespace, of
// the form RegistryAlias/RepositoryPrefix/<sanitised name>:<unique suffix>.
// name is lowered to a valid OCI path component by SanitiseComponent and
// the suffix is drawn from UniqueSuffix, so repeated calls -- even with the
// same name -- yield distinct references. It returns an error if name is
// empty after trimming surrounding whitespace.
func Ref(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("registry ref: NAME must not be empty")
	}
	return RegistryAlias + "/" + RepositoryPrefix + "/" + SanitiseComponent(name) + ":" + UniqueSuffix(), nil
}

// UniqueSuffix returns a process-local image tag suffix of the form
// "<pid>-<n>", where n is a counter incremented on every call. The pid
// keeps it distinct across concurrent processes and n across repeated
// calls within one process, so a freshly minted suffix never collides
// with one already handed out.
func UniqueSuffix() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), refSeq.Add(1))
}

// SanitiseComponent normalises s into a valid OCI repository path
// component: it lower-cases the input, keeps ASCII letters and digits,
// collapses every run of other characters into a single '-', then trims
// leading and trailing dashes. An input that reduces to the empty string
// yields "bytecode", so the result is always a usable component.
func SanitiseComponent(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "bytecode"
	}
	return out
}

func newRegistry() (*httptest.Server, string, error) {
	server := httptest.NewServer(registry.New(
		registry.Logger(log.New(io.Discard, "", 0)),
	))
	u, err := url.Parse(server.URL)
	if err != nil {
		server.Close()
		return nil, "", fmt.Errorf("start e2e image registry: %w", err)
	}
	return server, u.Host, nil
}

func normaliseHost(s string) (string, error) {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", RegistryEnv, err)
		}
		if u.Host == "" || u.Path != "" && u.Path != "/" {
			return "", fmt.Errorf("%s must name a registry host, got %q", RegistryEnv, s)
		}
		return u.Host, nil
	}
	host := strings.TrimSuffix(s, "/")
	if host == "" || strings.Contains(host, "/") {
		return "", fmt.Errorf("%s must name a registry host, got %q", RegistryEnv, s)
	}
	return host, nil
}

func isLoopbackRegistry(registryHost string) bool {
	host := registryHost
	if h, _, err := net.SplitHostPort(registryHost); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
