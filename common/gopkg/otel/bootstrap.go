package otel

import (
	"context"
	"log/slog"

	"dominion/common/gopkg/bootstrap"
)

// initFn is a package-level variable pointing to Init, allowing tests
// to override it with a stub.
var initFn = Init

// component adapts the otel package into a bootstrap.Component.
type component struct {
	name     string
	opts     []Option
	shutdown Shutdown
}

// Component returns a bootstrap.Component that initializes OpenTelemetry
// providers during Start and shuts them down during Stop.
func Component(opts ...Option) bootstrap.Component {
	return &component{
		name: "otel",
		opts: opts,
	}
}

// Name returns the component name "otel".
func (c *component) Name() string {
	return c.name
}

// Stage returns StageFoundation because OTel providers are foundational
// infrastructure required before clients or servers start.
func (c *component) Stage() bootstrap.Stage {
	return bootstrap.StageFoundation
}

// Start initializes OpenTelemetry providers via Init and saves the
// shutdown function for later cleanup.
func (c *component) Start(ctx context.Context) error {
	shutdown, err := initFn(ctx, c.opts...)
	if err != nil {
		slog.ErrorContext(ctx, "otel start failed", "component", c.name, "error", err)
		return err
	}
	c.shutdown = shutdown
	slog.InfoContext(ctx, "otel started", "component", c.name)
	return nil
}

// Stop calls the saved shutdown function to flush and close OTel providers.
// Returns nil if shutdown is nil (Start was never called or failed).
func (c *component) Stop(ctx context.Context) error {
	if c.shutdown == nil {
		return nil
	}
	return c.shutdown(ctx)
}
