// Package engine defines the lifecycle shared by RouteForge proxy engines.
package engine

import (
	"context"

	"github.com/routeforge/routeforge/internal/config"
)

// Engine serves a validated RouteForge configuration and stops gracefully.
type Engine interface {
	// Apply validates, compiles, and activates a configuration.
	Apply(*config.Config) error
	// Serve starts the engine until the context is canceled or a listener fails.
	Serve(context.Context) error
	// Shutdown stops listeners and waits for active requests to finish.
	Shutdown(context.Context) error
}
