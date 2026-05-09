package gateway

import (
	"context"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/gateway/domain"
)

const (
	spanRoute          = "gateway.worker.route"
	logFieldTargetConn = "target_conn_id"
)

type routingWorker struct {
	messages <-chan *domain.RoutedMessage
	route    func(ctx context.Context, msg *domain.RoutedMessage)
}

func NewRoutingWorker(messages <-chan *domain.RoutedMessage, route func(ctx context.Context, msg *domain.RoutedMessage)) *routingWorker {
	return &routingWorker{messages: messages, route: route}
}

func (w *routingWorker) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-w.messages:
			if !ok {
				return nil
			}
			ctx, span := otel.Tracer().Start(ctx, spanRoute)
			logs.InfoContext(ctx, "routing message",
				logFieldSessionID, msg.Message.SessionID,
				logFieldTargetConn, msg.TargetConnID,
			)
			w.route(ctx, msg)
			span.End()
		}
	}
}

func (w *routingWorker) Stop(_ context.Context) error {
	return nil
}
