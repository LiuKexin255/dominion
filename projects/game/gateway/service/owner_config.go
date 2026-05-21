package service

import (
	"errors"
	"os"
	"strings"
	"time"
)

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

// NewOwnerConfig reads environment variables and builds an OwnerConfig.
//
// Required env vars:
//   - HOSTNAME → GatewayID
//   - SESSION_TOKEN_SECRET → TokenSecret
//
// Optional env vars (with defaults):
//   - SESSION_TOKEN_TTL (15m)
//   - SESSION_TOKEN_REFRESH_GRACE (60m)
//   - SESSION_IDLE_TTL (30m)
//   - INTERNAL_GRPC_PORT (:8082)
func NewOwnerConfig() (*OwnerConfig, error) {
	gatewayID := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if gatewayID == "" {
		return nil, errors.New("missing required environment variable HOSTNAME")
	}

	tokenSecret := strings.TrimSpace(os.Getenv("SESSION_TOKEN_SECRET"))
	if tokenSecret == "" {
		return nil, errors.New("missing required environment variable SESSION_TOKEN_SECRET")
	}

	return &OwnerConfig{
		GatewayID:         gatewayID,
		TokenSecret:       tokenSecret,
		TokenTTL:          durationEnv("SESSION_TOKEN_TTL", 15*time.Minute),
		TokenRefreshGrace: durationEnv("SESSION_TOKEN_REFRESH_GRACE", 60*time.Minute),
		IdleTTL:           durationEnv("SESSION_IDLE_TTL", 30*time.Minute),
		InternalGRPCPort:  envOrDefault("INTERNAL_GRPC_PORT", ":8082"),
	}, nil
}

// envOrDefault reads an environment variable and returns its value, or the
// fallback if the variable is empty or unset.
func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// durationEnv reads an environment variable as a time.Duration string (e.g.
// "15m", "1h") and returns the parsed value, or defaultVal if the variable is
// empty or cannot be parsed.
func durationEnv(key string, defaultVal time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}
	return d
}
