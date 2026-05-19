package bootstrap

import "time"

// Option configures the Bootstrap behavior.
type Option func(*config)

// config holds the Bootstrap configuration.
type config struct {
	shutdownTimeout time.Duration
}

func defaultConfig() *config {
	return &config{
		shutdownTimeout: 5 * time.Second,
	}
}

// WithShutdownTimeout sets the grace period for component shutdown.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(c *config) {
		c.shutdownTimeout = timeout
	}
}
