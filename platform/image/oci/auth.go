package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"slices"

	gcrAuthn "github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	orasAuth "oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// newCredentialStore creates a credential store checking explicit auth files
// and then the current process environment. When bpfman is run under sudo,
// callers can pass the invoking user's auth file explicitly with
// REGISTRY_AUTH_FILE.
func newCredentialStore(logger *slog.Logger) (credentials.Store, error) {
	for _, path := range credentialStorePaths() {
		if _, err := os.Stat(path); err != nil {
			continue
		}

		logger.Debug("found registry credentials", "path", path)
		return credentials.NewStore(path, credentials.StoreOptions{})
	}
	return nil, errors.New("no registry credential store found")
}

func credentialStorePaths() []string {
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		if slices.Contains(paths, path) {
			return
		}
		paths = append(paths, path)
	}

	add(os.Getenv("REGISTRY_AUTH_FILE"))

	if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
		add(filepath.Join(xdgRuntime, "containers/auth.json"))
	}
	if home := os.Getenv("HOME"); home != "" {
		add(filepath.Join(home, ".config/containers/auth.json"))
		if dockerConfig := os.Getenv("DOCKER_CONFIG"); dockerConfig != "" {
			add(filepath.Join(dockerConfig, "config.json"))
		} else {
			add(filepath.Join(home, ".docker/config.json"))
		}
	}

	return paths
}

func registryCredentialHelp(registry string) string {
	const prefix = "registry credentials are looked up in the current process context"

	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" || sudoUser == "root" {
		return prefix + "; run podman login or set REGISTRY_AUTH_FILE"
	}
	u, err := user.Lookup(sudoUser)
	if err != nil {
		return prefix + "; run podman login or set REGISTRY_AUTH_FILE"
	}

	authPath := filepath.Join(u.HomeDir, ".config/containers/auth.json")
	return fmt.Sprintf(
		"%s; bpfman is running under sudo, so to use %s's credentials for %s, rerun with REGISTRY_AUTH_FILE=%s",
		prefix, sudoUser, registry, authPath,
	)
}

func missingCredentialError(registry string, err error) error {
	return fmt.Errorf("%w; %s", err, registryCredentialHelp(registry))
}

func registryCredentialsFound(ctx context.Context, registry string, logger *slog.Logger) bool {
	if isLoopbackRegistry(registry) {
		return false
	}
	store, err := newCredentialStore(logger)
	if err != nil {
		return false
	}

	cred, err := store.Get(ctx, registry)
	if err != nil {
		return false
	}
	return cred != orasAuth.EmptyCredential
}

func credentialStoreForGoContainerRegistry(ctx context.Context, ref name.Reference, logger *slog.Logger) (gcrAuthn.Authenticator, bool, error) {
	if isLoopbackRegistry(ref.Context().RegistryStr()) {
		return gcrAuthn.Anonymous, false, nil
	}
	store, err := newCredentialStore(logger)
	if err != nil {
		return gcrAuthn.Anonymous, false, nil
	}

	cred, err := store.Get(ctx, ref.Context().RegistryStr())
	if err != nil {
		return nil, true, fmt.Errorf("failed to resolve registry credentials: %w", err)
	}

	if cred == orasAuth.EmptyCredential {
		return gcrAuthn.Anonymous, false, nil
	}
	return gcrAuthn.FromConfig(gcrAuthn.AuthConfig{
		Username:      cred.Username,
		Password:      cred.Password,
		IdentityToken: cred.RefreshToken,
		RegistryToken: cred.AccessToken,
	}), true, nil
}
