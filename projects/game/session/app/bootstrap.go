// Package app provides shared bootstrap logic for the session service.
package app

import (
	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session"
	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/service"
)

// Bootstrap holds the shared components needed to run the session service.
type Bootstrap struct {
	Handler         *session.Handler
	Service         *service.SessionService
	Repo            domain.Repository
	GatewayRegistry *gateway.StaticRegistry
	TokenIssuer     *token.HMACSigner
}

// NewBootstrap assembles the session service from pre-created components.
func NewBootstrap(repo domain.Repository, tokenIssuer *token.HMACSigner, gatewayReg *gateway.StaticRegistry, gatewayDomain string) *Bootstrap {
	svc := service.NewSessionService(repo, tokenIssuer, gatewayReg, gatewayDomain)
	handler := session.NewHandler(svc)

	return &Bootstrap{
		Handler:         handler,
		Service:         svc,
		Repo:            repo,
		GatewayRegistry: gatewayReg,
		TokenIssuer:     tokenIssuer,
	}
}
