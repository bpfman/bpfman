package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman/cliformat"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

// LoadImageCmd loads BPF programs from an OCI container image.
type LoadImageCmd struct {
	loadFlags

	// ImageURL is the OCI image reference to pull the bytecode from.
	ImageURL string `arg:"" name:"image" help:"OCI image reference (e.g., quay.io/bpfman-bytecode/xdp_pass:latest)."`

	// PullPolicy controls when the image is pulled (Always, IfNotPresent,
	// or Never); it defaults to IfNotPresent.
	PullPolicy bpfman.ImagePullPolicy `short:"p" name:"pull-policy" help:"Image pull policy (Always, IfNotPresent, Never)." default:"IfNotPresent"`

	// RegistryAuth carries base64-encoded "username:password" registry
	// credentials for pulling the image. Prefer the BPFMAN_REGISTRY_AUTH
	// environment variable so the credentials do not appear in process
	// listings.
	RegistryAuth string `name:"registry-auth" env:"BPFMAN_REGISTRY_AUTH" help:"Base64-encoded registry auth (username:password). Prefer BPFMAN_REGISTRY_AUTH env var to avoid exposing credentials in process listings."`
}

// Run pulls the OCI image at ImageURL (honouring the pull policy and any
// registry credentials), loads the selected programs from it (applying
// metadata, global data, application grouping and any map-owner share),
// and renders the loaded programs in the chosen output format.
func (c *LoadImageCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	format, err := c.OutputFlags.Format()
	if err != nil {
		return err
	}

	logger := cli.Logger()

	mgr, cleanup, err := newImageManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	logger.Info("loading BPF programs from OCI image", "image", c.ImageURL, "programs", len(c.Programs), "pull_policy", c.PullPolicy.String())

	// Parse auth config from base64-encoded registry-auth.
	auth, err := registryAuthFromFlag(c.RegistryAuth)
	if err != nil {
		return err
	}

	if auth != nil {
		logger.Debug("using registry auth", "username", auth.Username)
	}

	ref := platform.ImageRef{
		URL:        c.ImageURL,
		PullPolicy: c.PullPolicy,
		Auth:       auth,
	}

	// Manager.Load decides whether post-pull work needs the writer
	// flock: ordinary loads stay lockless, while explicit map-owner
	// joins and PinByName loads serialise internally.
	req := manager.NewLoadRequest(manager.LoadSource{Image: &ref}, loadProgramSpecs(c.Programs), c.requestOpts())

	loaded, err := mgr.LoadFromRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to load from image: %w", err)
	}

	return cliformat.RenderLoadedPrograms(cli.Out, loaded, format)
}

// registryAuthFromFlag decodes a base64-encoded registry-auth flag
// value into ImageAuth, returning nil when the flag is empty.
func registryAuthFromFlag(encoded string) (*platform.ImageAuth, error) {
	if encoded == "" {
		return nil, nil
	}
	username, password, err := parseRegistryAuth(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid registry-auth: %w", err)
	}

	return &platform.ImageAuth{
		Username: username,
		Password: password,
	}, nil
}

// parseRegistryAuth parses a base64-encoded "username:password" string.
func parseRegistryAuth(encoded string) (username, password string, err error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 encoding: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected 'username:password' format")
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("username and password must both be non-empty")
	}

	return parts[0], parts[1], nil
}
