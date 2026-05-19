package app

import (
	"net/http"
	"strings"
)

// Router routes WebSocket paths to WSHandler and all other paths to the
// grpc-gateway mux.
type Router struct {
	WSHandler http.Handler
	GRPCMux   http.Handler
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if isWebSocketPath(req.URL.Path) {
		r.WSHandler.ServeHTTP(w, req)
		return
	}
	r.GRPCMux.ServeHTTP(w, req)
}

func isWebSocketPath(path string) bool {
	return strings.HasPrefix(path, "/v1/sessions/") && strings.HasSuffix(path, "/game/connect")
}
