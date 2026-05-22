// Package config holds the minimal configuration for the game gateway edge proxy.
package config

// GatewayConfig holds the HTTP server configuration for the gateway edge proxy.
// The gateway is stateless and does NOT hold token secrets, session state,
// or runtime ownership configuration.
type GatewayConfig struct {
	HTTPPort string
}

// NewGatewayConfig creates a GatewayConfig with sensible defaults.
// The caller may override individual fields after construction.
func NewGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		HTTPPort: ":8080",
	}
}
