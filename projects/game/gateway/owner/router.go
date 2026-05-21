// Package owner provides types and interfaces for owner gateway routing.
//
// The owner package defines the core abstractions for resolving which gateway
// instance owns a particular game session. Owner resolution determines whether
// a request should be handled locally or forwarded to a remote gateway.
package owner

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"dominion/common/gopkg/solver"
	"dominion/projects/game/gateway/token"
)

// Router routes requests to either a local handler or a remote owner gateway
// based on the claims embedded in the session token.
//
// When claims.OwnerGatewayID matches the router's gatewayID, the request is
// served by the local handler. Otherwise, the request is proxied to the
// resolved owner gateway endpoint via httputil.ReverseProxy, which supports
// regular HTTP and WebSocket upgrade requests transparently.
type Router struct {
	Resolver     OwnerResolver
	gatewayID    string
	localHandler http.Handler
}

// NewRouter creates a Router that routes requests based on owner gateway ID.
// It internally creates a DeployStatefulResolver and DeployOwnerResolver
// for resolving owner gateway endpoints. Returns an error if resolver
// creation fails.
func NewRouter(gatewayID string, localHandler http.Handler) (*Router, error) {
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		return nil, fmt.Errorf("owner: create stateful resolver: %w", err)
	}
	target := solver.MustParseTarget("game/gateway:internal-grpc")
	ownerResolver := NewDeployOwnerResolver(statefulResolver, target)
	return &Router{
		Resolver:     ownerResolver,
		gatewayID:    gatewayID,
		localHandler: localHandler,
	}, nil
}

// GatewayID returns the gateway ID of this router instance.
func (r *Router) GatewayID() string {
	return r.gatewayID
}

// Decide returns the routing target for the given ownerGatewayID.
// If ownerGatewayID matches the router's gatewayID, the request is local;
// otherwise it should be forwarded to a remote gateway.
func (r *Router) Decide(ownerGatewayID string) Target {
	if ownerGatewayID == r.gatewayID {
		return TargetLocal
	}
	return TargetRemote
}

// RouteRequest decides whether to handle the request locally or proxy it to
// the owner gateway. If claims.OwnerGatewayID matches the router's gatewayID,
// the request is served by the local handler. Otherwise, it is proxied.
func (r *Router) RouteRequest(w http.ResponseWriter, req *http.Request, claims *token.Claims) {
	if r.Decide(claims.OwnerGatewayID) == TargetLocal {
		r.localHandler.ServeHTTP(w, req)
		return
	}
	r.ProxyRequest(w, req, claims.OwnerGatewayID)
}

// ProxyRequest forwards the request to the owner gateway identified by
// ownerGatewayID. Same semantics as the internal proxy logic, but exported
// for handlers that decide local vs proxy outside of RouteRequest.
func (r *Router) ProxyRequest(w http.ResponseWriter, req *http.Request, ownerGatewayID string) {
	r.proxyRequest(w, req, ownerGatewayID)
}

// proxyRequest forwards the request to the owner gateway identified by
// ownerGatewayID. It resolves the owner's internal endpoint via the
// OwnerResolver, constructs a reverse proxy, and streams the response back
// to the client. WebSocket upgrade requests are supported transparently.
//
// Error responses:
//   - 503 Service Unavailable: owner gateway unreachable (resolver error)
//   - 502 Bad Gateway: proxy round-trip failed
func (r *Router) proxyRequest(w http.ResponseWriter, req *http.Request, ownerGatewayID string) {
	endpoint, err := r.Resolver.Resolve(req.Context(), ownerGatewayID)
	if err != nil {
		http.Error(w, fmt.Sprintf("owner gateway %q unreachable: %v", ownerGatewayID, err), http.StatusServiceUnavailable)
		return
	}

	target, err := url.Parse(endpoint)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid owner endpoint URL: %v", err), http.StatusBadGateway)
		return
	}

	proxy := &httputil.ReverseProxy{
		Director:  makeDirector(target, req.URL.Path, req.URL.RawQuery),
		Transport: http.DefaultTransport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, fmt.Sprintf("proxy request failed: %v", err), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, req)
}

// makeDirector returns a Director function that rewrites the proxy request to
// target the resolved owner endpoint while preserving the original path and
// query parameters.
func makeDirector(target *url.URL, origPath, origQuery string) func(*http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = origPath
		req.URL.RawQuery = origQuery
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "")
		}
	}
}

// ParseAndRoute extracts the session token from the "token" query parameter,
// verifies it using the provided verifier, and routes the request based on
// the embedded claims.
//
// Error responses:
//   - 401 Unauthorized: token is missing or invalid
func (r *Router) ParseAndRoute(w http.ResponseWriter, req *http.Request, verifier token.Verifier) {
	tokenStr := req.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := verifier.Verify(tokenStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid token: %v", err), http.StatusUnauthorized)
		return
	}

	r.RouteRequest(w, req, claims)
}
