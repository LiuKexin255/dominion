package owner

import (
	"testing"
)

func TestNewResolver(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	resolver, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	if resolver == nil {
		t.Fatal("NewResolver() = nil, want resolver")
	}
}
