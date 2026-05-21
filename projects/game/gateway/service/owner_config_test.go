package service

import (
	"os"
	"testing"
	"time"
)

func TestNewOwnerConfig(t *testing.T) {
	t.Run("all env vars set", func(t *testing.T) {
		// given
		os.Setenv("HOSTNAME", "gw-1")
		os.Setenv("SESSION_TOKEN_SECRET", "my-secret")
		os.Setenv("SESSION_TOKEN_TTL", "5m")
		os.Setenv("SESSION_TOKEN_REFRESH_GRACE", "30m")
		os.Setenv("SESSION_IDLE_TTL", "10m")
		os.Setenv("INTERNAL_GRPC_PORT", ":9090")

		// when
		cfg, err := NewOwnerConfig()

		// then
		if err != nil {
			t.Fatalf("NewOwnerConfig() unexpected error: %v", err)
		}
		if cfg.GatewayID != "gw-1" {
			t.Fatalf("GatewayID = %q, want %q", cfg.GatewayID, "gw-1")
		}
		if cfg.TokenSecret != "my-secret" {
			t.Fatalf("TokenSecret = %q, want %q", cfg.TokenSecret, "my-secret")
		}
		if cfg.TokenTTL != 5*time.Minute {
			t.Fatalf("TokenTTL = %v, want %v", cfg.TokenTTL, 5*time.Minute)
		}
		if cfg.TokenRefreshGrace != 30*time.Minute {
			t.Fatalf("TokenRefreshGrace = %v, want %v", cfg.TokenRefreshGrace, 30*time.Minute)
		}
		if cfg.IdleTTL != 10*time.Minute {
			t.Fatalf("IdleTTL = %v, want %v", cfg.IdleTTL, 10*time.Minute)
		}
		if cfg.InternalGRPCPort != ":9090" {
			t.Fatalf("InternalGRPCPort = %q, want %q", cfg.InternalGRPCPort, ":9090")
		}
	})

	t.Run("defaults applied when optional env vars missing", func(t *testing.T) {
		// given
		os.Setenv("HOSTNAME", "gw-2")
		os.Setenv("SESSION_TOKEN_SECRET", "another-secret")
		os.Unsetenv("SESSION_TOKEN_TTL")
		os.Unsetenv("SESSION_TOKEN_REFRESH_GRACE")
		os.Unsetenv("SESSION_IDLE_TTL")
		os.Unsetenv("INTERNAL_GRPC_PORT")

		// when
		cfg, err := NewOwnerConfig()

		// then
		if err != nil {
			t.Fatalf("NewOwnerConfig() unexpected error: %v", err)
		}
		if cfg.TokenTTL != 15*time.Minute {
			t.Fatalf("TokenTTL = %v, want %v", cfg.TokenTTL, 15*time.Minute)
		}
		if cfg.TokenRefreshGrace != 60*time.Minute {
			t.Fatalf("TokenRefreshGrace = %v, want %v", cfg.TokenRefreshGrace, 60*time.Minute)
		}
		if cfg.IdleTTL != 30*time.Minute {
			t.Fatalf("IdleTTL = %v, want %v", cfg.IdleTTL, 30*time.Minute)
		}
		if cfg.InternalGRPCPort != ":8082" {
			t.Fatalf("InternalGRPCPort = %q, want %q", cfg.InternalGRPCPort, ":8082")
		}
	})

	t.Run("missing HOSTNAME returns error", func(t *testing.T) {
		// given
		os.Unsetenv("HOSTNAME")
		os.Setenv("SESSION_TOKEN_SECRET", "some-secret")

		// when
		_, err := NewOwnerConfig()

		// then
		if err == nil {
			t.Fatal("expected error for missing HOSTNAME, got nil")
		}
	})

	t.Run("missing SESSION_TOKEN_SECRET returns error", func(t *testing.T) {
		// given
		os.Setenv("HOSTNAME", "gw-3")
		os.Unsetenv("SESSION_TOKEN_SECRET")

		// when
		_, err := NewOwnerConfig()

		// then
		if err == nil {
			t.Fatal("expected error for missing SESSION_TOKEN_SECRET, got nil")
		}
	})
}
