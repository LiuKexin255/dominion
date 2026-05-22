package service

import (
	"context"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/runtime/domain"
)

const (
	spanCompletion    = "runtime.control.completion"
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
			logs.Info(ctx, "completion processed",
				event.String(logFieldSessionID, comp.SessionID),
			)
			w.handle(ctx, comp)
			span.End()
		}
	}
}

func (w *CompletionWorker) Stop(_ context.Context) error {
	return nil
}
