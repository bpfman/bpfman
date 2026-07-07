package main

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/image/oci"
	"github.com/bpfman/bpfman/platform/image/verify"
)

// newImageManager creates a manager with the image puller needed by
// `program load image`. Image verification and registry support stay
// local to the bpfman binary; other runtime users should not link it.
func newImageManager(ctx context.Context, cli *runtime.CLI) (*manager.Manager, func() error, error) {
	puller, err := buildImagePuller(cli)
	if err != nil {
		return nil, nil, fmt.Errorf("create image puller: %w", err)
	}

	return newManagerWithImagePuller(ctx, cli, puller)
}

// buildImagePuller creates an image puller with signature verification
// settings from config.
func buildImagePuller(cli *runtime.CLI) (platform.ImagePuller, error) {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	cache, err := cli.EnsuredImageCache()
	if err != nil {
		return nil, fmt.Errorf("ensure image cache: %w", err)
	}

	logger := cli.Logger()
	verifier, err := verify.FromSigningConfig(cfg.Signing, logger)
	if err != nil {
		return nil, fmt.Errorf("configure signature verifier: %w", err)
	}

	return oci.NewPuller(cache, oci.WithLogger(logger), oci.WithVerifier(verifier))
}
