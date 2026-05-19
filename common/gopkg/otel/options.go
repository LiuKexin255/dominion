package otel

// Option configures Init behavior.
type Option func(*config)

// config holds the OTel provider configuration.
type config struct {
	collectorEndpoint string
}

func defaultConfig() *config {
	return &config{
		collectorEndpoint: defaultCollectorEndpoint,
	}
}

// WithCollectorEndpoint overrides the default OTLP collector endpoint.
func WithCollectorEndpoint(endpoint string) Option {
	return func(c *config) {
		c.collectorEndpoint = endpoint
	}
}
