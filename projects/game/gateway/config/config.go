package config

import "time"

// OwnerConfig holds the identity, token signing, and session lifecycle
// configuration for a gateway instance.
type OwnerConfig struct {
	GatewayID         string
	TokenSecret       string
	TokenTTL          time.Duration
	TokenRefreshGrace time.Duration
	IdleTTL           time.Duration
	InternalGRPCPort  string
}

// NewOwnerConfig creates an OwnerConfig with the given required parameters
// and sensible defaults for optional fields. This constructor does NOT read
// environment variables — the caller is responsible for providing values.
func NewOwnerConfig(gatewayID, tokenSecret string) *OwnerConfig {
	return &OwnerConfig{
		GatewayID:         gatewayID,
		TokenSecret:       tokenSecret,
		TokenTTL:          15 * time.Minute,
		TokenRefreshGrace: 60 * time.Minute,
		IdleTTL:           30 * time.Minute,
		InternalGRPCPort:  ":8082",
	}
}
