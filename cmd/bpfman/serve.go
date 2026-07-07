package main

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/server"
	"github.com/bpfman/bpfman/version"
)

// ServeCmd starts the gRPC daemon.
type ServeCmd struct {
	// CSISupport enables the CSI driver support in the running daemon.
	CSISupport bool `name:"csi-support" help:"Enable CSI driver support."`

	// SocketPath is the Unix socket path for the gRPC server. It defaults
	// to /run/bpfman-sock/bpfman.sock for compatibility with
	// bpfman-operator, which expects the socket at this location.
	SocketPath string `name:"socket-path" help:"Unix socket path for gRPC server." env:"BPFMAN_SOCKET_PATH" default:"/run/bpfman-sock/bpfman.sock"`
}

// Run starts the bpfman gRPC daemon: it builds the logger, runtime
// layout, image cache and application config, then runs the server on
// the configured Unix-socket address (optionally with CSI support)
// until the context is cancelled.
func (c *ServeCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	logger, err := cli.LoggerFromConfig()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	logger.Info("starting bpfman", "version", version.Get().String())

	appConfig, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	layout, err := cli.Layout()
	if err != nil {
		return fmt.Errorf("invalid runtime directory: %w", err)
	}

	imageCache, err := cli.EnsuredImageCache()
	if err != nil {
		return fmt.Errorf("ensure image cache directory: %w", err)
	}

	cfg := server.RunConfig{
		Layout:     layout,
		ImageCache: imageCache,
		CSISupport: c.CSISupport,
		SocketPath: c.SocketPath,
		Logger:     logger,
		Config:     appConfig,
	}

	return server.Run(ctx, cfg)
}
