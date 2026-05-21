package owner

import (
	"testing"
)

func TestDecide_Local(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	router, err := NewRouter("gw-0")
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	if got := router.Decide("gw-0"); got != TargetLocal {
		t.Fatalf("Decide(%q) = %v, want %v", "gw-0", got, TargetLocal)
	}
}

func TestDecide_Remote(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	router, err := NewRouter("gw-0")
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	if got := router.Decide("gw-1"); got != TargetRemote {
		t.Fatalf("Decide(%q) = %v, want %v", "gw-1", got, TargetRemote)
	}
}

func TestGatewayID(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	router, err := NewRouter("gw-42")
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	if got := router.GatewayID(); got != "gw-42" {
		t.Fatalf("GatewayID() = %q, want %q", got, "gw-42")
	}
}
