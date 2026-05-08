// Package app provides shared bootstrap logic for the session service.
package app

import (
	"context"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/grpc"
	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session"
	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/service"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
)

// Bootstrap holds the shared components needed to run the session service.
type Bootstrap struct {
	Handler         *session.Handler
	Service         *service.SessionService
	Repo            domain.Repository
	GatewayRegistry gateway.Registry
	TokenIssuer     *token.HMACSigner
}

// NewBootstrap assembles the session service from pre-created components.
func NewBootstrap(repo domain.Repository, tokenIssuer *token.HMACSigner, gatewayReg gateway.Registry) *Bootstrap {
	svc := service.NewSessionService(repo, tokenIssuer, gatewayReg)
	handler := session.NewHandler(svc)
	return &Bootstrap{
		Handler:         handler,
		Service:         svc,
		Repo:            repo,
		GatewayRegistry: gatewayReg,
		TokenIssuer:     tokenIssuer,
	}
}

// Component returns a bootstrap Component for the session HTTP server.
func (b *Bootstrap) Component(httpAddr string) bootstrap.Component {
	grpcServer := grpcgo.NewServer(grpc.ServiceDefault()...)
	session.RegisterSessionServiceServer(grpcServer, b.Handler)

	httpMux := runtime.NewServeMux()
	_ = session.RegisterSessionServiceHandlerServer(context.Background(), httpMux, b.Handler)
	httpServer := &http.Server{Addr: httpAddr, Handler: httpMux}

	return bootstrap.HTTPServer("session-http", httpServer)
}
