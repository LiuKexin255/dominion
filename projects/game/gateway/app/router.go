package app

import (
	"net/http"
	"strings"

	"dominion/projects/game/gateway/owner"
	"dominion/projects/game/gateway/token"
)

// Router routes WebSocket paths to WSHandler and all other paths through the
// owner routing middleware. When OwnerRouter is nil, gRPC paths fall back to
// the GRPCMux directly.
type Router struct {
	WSHandler     http.Handler
	GRPCMux       http.Handler
	OwnerRouter   *owner.Router
	TokenVerifier token.Verifier
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if isWebSocketPath(req.URL.Path) {
		r.WSHandler.ServeHTTP(w, req)
		return
	}
	if r.OwnerRouter != nil {
		r.OwnerRouter.ParseAndRoute(w, req, r.TokenVerifier)
		return
	}
	r.GRPCMux.ServeHTTP(w, req)
}

func isWebSocketPath(path string) bool {
	return strings.HasPrefix(path, "/v1/sessions/") && strings.HasSuffix(path, "/game/connect")
}
