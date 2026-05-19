package bootstrap

import "context"

// Component represents a startable and stoppable application component.
type Component interface {
	// Name returns the component name.
	Name() string
	// Stage returns the lifecycle stage this component belongs to.
	Stage() Stage
	// Start initializes and starts the component.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the component.
	Stop(ctx context.Context) error
}
