package owner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dominion/projects/game/gateway/token"
)

// stubOwnerResolver implements OwnerResolver for testing.
type stubOwnerResolver struct {
	endpoint string
	err      error
}

func (s *stubOwnerResolver) Resolve(_ context.Context, _ string) (string, error) {
	return s.endpoint, s.err
}

// stubVerifier implements token.Verifier for testing.
type stubVerifier struct {
	claims *token.Claims
	err    error
}

func (s *stubVerifier) Verify(_ string) (*token.Claims, error) {
	return s.claims, s.err
}

func (s *stubVerifier) VerifyWithGrace(_ string, _ time.Duration) (*token.Claims, error) {
	return s.claims, s.err
}

// stubHandler records whether ServeHTTP was called.
type stubHandler struct {
	called bool
}

func (h *stubHandler) ServeHTTP(_ http.ResponseWriter, _ *http.Request) {
	h.called = true
}

func TestDecide_Local(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	if got := router.Decide("gw-0"); got != TargetLocal {
		t.Fatalf("Decide(%q) = %v, want %v", "gw-0", got, TargetLocal)
	}
}

func TestDecide_Remote(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	if got := router.Decide("gw-1"); got != TargetRemote {
		t.Fatalf("Decide(%q) = %v, want %v", "gw-1", got, TargetRemote)
	}
}

func TestRouteRequest_Local(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	// given
	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.Resolver = &stubOwnerResolver{endpoint: "http://should-not-be-called:8082"}

	claims := &token.Claims{
		OwnerGatewayID: "gw-0",
		SessionID:      "session-1",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/session-1/game/connect?token=abc", nil)
	w := httptest.NewRecorder()

	// when
	router.RouteRequest(w, req, claims)

	// then
	if !handler.called {
		t.Fatal("RouteRequest() local handler was not called for local owner")
	}
}

func TestRouteRequest_Remote(t *testing.T) {
	// given
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/session-1/game/connect" {
			t.Errorf("backend received path %q, want %q", r.URL.Path, "/v1/sessions/session-1/game/connect")
		}
		if r.URL.Query().Get("token") != "abc" {
			t.Errorf("backend received token %q, want %q", r.URL.Query().Get("token"), "abc")
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "backend response")
	}))
	defer backend.Close()

	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.Resolver = &stubOwnerResolver{endpoint: backend.URL}

	claims := &token.Claims{
		OwnerGatewayID:  "gw-1",
		SessionID:  "session-1",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/session-1/game/connect?token=abc", nil)
	w := httptest.NewRecorder()

	// when
	router.RouteRequest(w, req, claims)

	// then
	if handler.called {
		t.Fatal("RouteRequest() local handler should not be called for remote request")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("RouteRequest() status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "backend response" {
		t.Fatalf("RouteRequest() body = %q, want %q", w.Body.String(), "backend response")
	}
}

func TestRouteRequest_Remote_Unreachable(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	// given
	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.Resolver = &stubOwnerResolver{err: fmt.Errorf("instance not found")}

	claims := &token.Claims{
		OwnerGatewayID:  "gw-1",
		SessionID:  "session-1",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/session-1/game/connect", nil)
	w := httptest.NewRecorder()

	// when
	router.RouteRequest(w, req, claims)

	// then
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("RouteRequest() status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestParseAndRoute_MissingToken(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	// given
	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.Resolver = &stubOwnerResolver{endpoint: "http://localhost:8082"}
	verifier := &stubVerifier{}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/session-1/game/connect", nil)
	w := httptest.NewRecorder()

	// when
	router.ParseAndRoute(w, req, verifier)

	// then
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ParseAndRoute() status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if handler.called {
		t.Fatal("ParseAndRoute() local handler should not be called without token")
	}
}

func TestParseAndRoute_InvalidToken(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	// given
	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.Resolver = &stubOwnerResolver{endpoint: "http://localhost:8082"}
	verifier := &stubVerifier{err: fmt.Errorf("bad token")}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/session-1/game/connect?token=bad-token", nil)
	w := httptest.NewRecorder()

	// when
	router.ParseAndRoute(w, req, verifier)

	// then
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ParseAndRoute() status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if handler.called {
		t.Fatal("ParseAndRoute() local handler should not be called with invalid token")
	}
}

func TestProxy_WebSocket_Upgrade(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	// given
	var receivedConnection string
	var receivedUpgrade string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedConnection = r.Header.Get("Connection")
		receivedUpgrade = r.Header.Get("Upgrade")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	handler := &stubHandler{}
	router, err := NewRouter("gw-0", handler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.Resolver = &stubOwnerResolver{endpoint: backend.URL}

	claims := &token.Claims{
		OwnerGatewayID:  "gw-1",
		SessionID:  "session-1",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/session-1/game/connect?token=abc", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()

	// when
	router.RouteRequest(w, req, claims)

	// then
	if receivedConnection != "Upgrade" {
		t.Fatalf("proxy Connection header = %q, want %q", receivedConnection, "Upgrade")
	}
	if receivedUpgrade != "websocket" {
		t.Fatalf("proxy Upgrade header = %q, want %q", receivedUpgrade, "websocket")
	}
}
