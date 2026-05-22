package config

import "time"

// RuntimeConfig holds configuration for a runtime instance.
type RuntimeConfig struct {
	RuntimeID         string
	TokenSecret       string
	TokenTTL          time.Duration
	TokenRefreshGrace time.Duration
	IdleTTL           time.Duration
	HTTPPort          string
	GRPCPort          string
}

// NewRuntimeConfig creates a RuntimeConfig with sensible defaults.
func NewRuntimeConfig(runtimeID, tokenSecret string) *RuntimeConfig {
	return &RuntimeConfig{
		RuntimeID:         runtimeID,
		TokenSecret:       tokenSecret,
		TokenTTL:          15 * time.Minute,
		TokenRefreshGrace: 60 * time.Minute,
		IdleTTL:           30 * time.Minute,
		HTTPPort:          ":8080",
		GRPCPort:          ":8082",
	}
}
