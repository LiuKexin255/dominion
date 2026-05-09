package service

import (
	"context"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/gateway/domain"
)

const (
	spanCompletion    = "gateway.control.completion"
	logFieldSessionID = "session_id"
)

type CompletionWorker struct {
	completions <-chan domain.ControlCompletion
	handle      func(ctx context.Context, comp domain.ControlCompletion)
}

func NewCompletionWorker(
	completions <-chan domain.ControlCompletion,
	handle func(ctx context.Context, comp domain.ControlCompletion),
) *CompletionWorker {
	return &CompletionWorker{
		completions: completions,
		handle:      handle,
	}
}

func (w *CompletionWorker) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case comp, ok := <-w.completions:
			if !ok {
				return nil
			}
			ctx, span := otel.Tracer().Start(ctx, spanCompletion)
			logs.InfoContext(ctx, "completion processed",
				logFieldSessionID, comp.SessionID,
			)
			w.handle(ctx, comp)
			span.End()
		}
	}
}

func (w *CompletionWorker) Stop(_ context.Context) error {
	return nil
}
