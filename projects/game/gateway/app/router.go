// Package app provides the HTTP routing layer for the game gateway edge proxy.
//
// The gateway is a pure edge aggregation layer:
//   - Non-WebSocket requests are passed through to the grpc-gateway mux, which
//     forwards to session and runtime gRPC backends.
//   - WebSocket upgrade requests (GET /v1/sessions/{id}/game/connect?token=...)
//     are reverse-proxied to the owner runtime instance.
//
// The gateway does NOT verify tokens, issue tokens, hold session state,
// or implement business logic.
package app

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"dominion/projects/game/gateway/runtime/owner"
	"dominion/projects/game/pkg/token"
)

// Router routes HTTP requests for the gateway edge proxy.
type Router struct {
	GRPCMux        http.Handler
	OwnerResolver  owner.OwnerResolver
	OwnerExtractor token.OwnerExtractor
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if isWebSocketPath(req.URL.Path) {
		r.proxyWebSocket(w, req)
		return
	}
	r.GRPCMux.ServeHTTP(w, req)
}

// proxyWebSocket extracts the token from the query, parses routing claims,
// resolves the owner runtime endpoint, and reverse-proxies the WebSocket
// upgrade request to the owner runtime.
func (r *Router) proxyWebSocket(w http.ResponseWriter, req *http.Request) {
	tokenStr := req.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := r.OwnerExtractor.ParseRoutingClaims(tokenStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid token: %v", err), http.StatusUnauthorized)
		return
	}

	if claims.OwnerRuntimeID == "" {
		http.Error(w, "missing owner_runtime_id in token", http.StatusBadRequest)
		return
	}

	endpoint, err := r.OwnerResolver.Resolve(req.Context(), claims.OwnerRuntimeID)
	if err != nil {
		http.Error(w, fmt.Sprintf("owner runtime %q unreachable: %v", claims.OwnerRuntimeID, err), http.StatusServiceUnavailable)
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

// makeDirector returns a ReverseProxy director that rewrites the request to
// target the resolved runtime endpoint while preserving the original path
// and query string.
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

// isWebSocketPath returns true if the path matches the WebSocket connect
// pattern: /v1/sessions/{id}/game/connect
func isWebSocketPath(path string) bool {
	return strings.HasPrefix(path, "/v1/sessions/") && strings.HasSuffix(path, "/game/connect")
}
