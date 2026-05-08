package app

import (
	"context"
	"net/http"
	"strings"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/grpc"
	gateway "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/service"
	"dominion/projects/game/pkg/token"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
)

// Bootstrap holds the shared components for the gateway service.
type Bootstrap struct {
	Handler   *gateway.Handler
	WSHandler *gateway.WebSocketHandler
	Service   *service.GatewayService
}

// NewBootstrap assembles the gateway service components.
func NewBootstrap(tokenSecret, gatewayID string) *Bootstrap {
	verifier := token.NewHMACSigner(tokenSecret, 0)
	sessions := sessionmanager.NewManager(gatewayID)
	control := service.NewControlExecutor()
	svc := service.NewGatewayService(sessions, control, gatewayID, verifier)
	handler := gateway.NewHandler(svc)
	wsHandler := gateway.NewWebSocketHandler(svc)
	svc.SetAsyncSink(wsHandler)
	return &Bootstrap{
		Handler:   handler,
		WSHandler: wsHandler,
		Service:   svc,
	}
}

// Component returns a bootstrap Component for the gateway HTTP server.
func (b *Bootstrap) Component(httpAddr string) bootstrap.Component {
	grpcServer := grpcgo.NewServer(grpc.ServiceDefault()...)
	gateway.RegisterGameGatewayServiceServer(grpcServer, b.Handler)

	httpMux := runtime.NewServeMux()
	_ = gateway.RegisterGameGatewayServiceHandlerServer(context.Background(), httpMux, b.Handler)

	router := &gatewayRouter{wsHandler: b.WSHandler, grpcMux: httpMux}
	httpServer := &http.Server{Addr: httpAddr, Handler: router}

	return bootstrap.HTTPServer("gateway-http", httpServer)
}

type gatewayRouter struct {
	wsHandler http.Handler
	grpcMux   http.Handler
}

func (r *gatewayRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if isWebSocketPath(req.URL.Path) {
		r.wsHandler.ServeHTTP(w, req)
		return
	}
	r.grpcMux.ServeHTTP(w, req)
}

func isWebSocketPath(path string) bool {
	return strings.HasPrefix(path, "/v1/sessions/") && strings.HasSuffix(path, "/game/connect")
}
