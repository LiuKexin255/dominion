package otel

import (
	"context"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
)

// initFn is a package-level variable pointing to Init, allowing tests
// to override it with a stub.
var initFn = Init

var logFieldComponent = "component"

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
		logs.Error(ctx, "otel start failed", event.String(logFieldComponent, c.name), event.Err(err))
		return err
	}
	c.shutdown = shutdown
	logs.Info(ctx, "otel started", event.String(logFieldComponent, c.name))
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
