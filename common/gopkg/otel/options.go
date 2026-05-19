package otel

// Option configures Init behavior.
type Option func(*config)

// config holds the OTel provider configuration.
type config struct {
	collectorEndpoint string
	loggerName        string
}

func defaultConfig() *config {
	return &config{
		collectorEndpoint: defaultCollectorEndpoint,
		loggerName:        "dominion/common/gopkg/otel",
	}
}

// WithCollectorEndpoint overrides the default OTLP collector endpoint.
func WithCollectorEndpoint(endpoint string) Option {
	return func(c *config) {
		c.collectorEndpoint = endpoint
	}
}

// WithLoggerName overrides the default instrumentation scope name used for
// the OTel log reporter bridge.
func WithLoggerName(name string) Option {
	return func(c *config) {
		c.loggerName = name
	}
}
