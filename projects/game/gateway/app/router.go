package app

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"dominion/projects/game/gateway/owner"
	"dominion/projects/game/gateway/token"
)

// Router routes requests based on owner claims.
// Local requests dispatch to WSHandler or GRPCMux; remote requests are proxied.
type Router struct {
	WSHandler     http.Handler
	GRPCMux       http.Handler
	OwnerRouter   *owner.Router
	TokenVerifier token.Verifier
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	tokenStr := req.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	claims, err := r.TokenVerifier.Verify(tokenStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid token: %v", err), http.StatusUnauthorized)
		return
	}
	if r.OwnerRouter.Decide(claims.OwnerGatewayID) == owner.TargetLocal {
		r.dispatchLocal(w, req)
		return
	}
	r.proxyRequest(w, req, claims.OwnerGatewayID)
}

func (r *Router) dispatchLocal(w http.ResponseWriter, req *http.Request) {
	if isWebSocketPath(req.URL.Path) {
		r.WSHandler.ServeHTTP(w, req)
	} else {
		r.GRPCMux.ServeHTTP(w, req)
	}
}

func (r *Router) proxyRequest(w http.ResponseWriter, req *http.Request, ownerGatewayID string) {
	endpoint, err := r.OwnerRouter.Resolver.Resolve(req.Context(), ownerGatewayID)
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

func isWebSocketPath(path string) bool {
	return strings.HasPrefix(path, "/v1/sessions/") && strings.HasSuffix(path, "/game/connect")
}
