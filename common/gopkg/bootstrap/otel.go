package bootstrap

import (
	"context"

	"dominion/common/gopkg/otel"
)

// otelInit is a package-level variable pointing to otel.Init, allowing tests
// to override it with a stub.
var otelInit = otel.Init

// otelComponent adapts the otel package into a bootstrap Component.
type otelComponent struct {
	name     string
	opts     []otel.Option
	shutdown otel.Shutdown
}

// OTel returns a Component that initializes OpenTelemetry providers during Start
// and shuts them down during Stop.
func OTel(opts ...otel.Option) Component {
	return &otelComponent{
		name: "otel",
		opts: opts,
	}
}

// Name returns the component name "otel".
func (c *otelComponent) Name() string {
	return c.name
}

// Stage returns StageFoundation because OTel providers are foundational
// infrastructure required before clients or servers start.
func (c *otelComponent) Stage() Stage {
	return StageFoundation
}

// Start initializes OpenTelemetry providers via otelInit and saves the
// shutdown function for later cleanup.
func (c *otelComponent) Start(ctx context.Context) error {
	shutdown, err := otelInit(ctx, c.opts...)
	if err != nil {
		return err
	}
	c.shutdown = shutdown
	return nil
}

// Stop calls the saved shutdown function to flush and close OTel providers.
// Returns nil if shutdown is nil (Start was never called or failed).
func (c *otelComponent) Stop(ctx context.Context) error {
	if c.shutdown == nil {
		return nil
	}
	return c.shutdown(ctx)
}
