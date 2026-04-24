package app

import (
	gateway "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/service"
	"dominion/projects/game/pkg/token"
)

type Bootstrap struct {
	Handler   *gateway.Handler
	WSHandler *gateway.WebSocketHandler
	Service   *service.GatewayService
}

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
