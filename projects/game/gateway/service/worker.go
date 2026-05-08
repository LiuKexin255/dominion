package service

import (
	"context"

	"dominion/projects/game/gateway/domain"
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
			w.handle(ctx, comp)
		}
	}
}

func (w *CompletionWorker) Stop(_ context.Context) error {
	return nil
}
