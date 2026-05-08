package gateway

import (
	"context"

	"dominion/projects/game/gateway/domain"
)

type routingWorker struct {
	messages <-chan *domain.RoutedMessage
	route    func(ctx context.Context, msg *domain.RoutedMessage)
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
			w.route(ctx, msg)
		}
	}
}

func (w *routingWorker) Stop(_ context.Context) error {
	return nil
}
