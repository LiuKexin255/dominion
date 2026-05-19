package domain

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestErrRetryClassifications(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		match   error
		unmatch []error
	}{
		{
			name:    "retry counted wraps and matches",
			err:     fmt.Errorf("%w: detail", ErrRetryCounted),
			match:   ErrRetryCounted,
			unmatch: []error{ErrWorkerFatal, context.Canceled, context.DeadlineExceeded},
		},
		{
			name:    "worker fatal wraps and matches",
			err:     fmt.Errorf("%w: detail", ErrWorkerFatal),
			match:   ErrWorkerFatal,
			unmatch: []error{ErrRetryCounted, context.Canceled, context.DeadlineExceeded},
		},
		{
			name:    "context canceled does not match classifications",
			err:     context.Canceled,
			match:   nil,
			unmatch: []error{ErrRetryCounted, ErrWorkerFatal},
		},
		{
			name:    "context deadline exceeded does not match classifications",
			err:     context.DeadlineExceeded,
			match:   nil,
			unmatch: []error{ErrRetryCounted, ErrWorkerFatal},
		},
		{
			name:    "stale state wraps and matches",
			err:     fmt.Errorf("%w: generation moved on", ErrStaleState),
			match:   ErrStaleState,
			unmatch: []error{ErrStaleGeneration, ErrRetryCounted, ErrWorkerFatal},
		},
		{
			name:    "stale generation wraps and matches",
			err:     fmt.Errorf("%w: expected gen 3 got 4", ErrStaleGeneration),
			match:   ErrStaleGeneration,
			unmatch: []error{ErrStaleState, ErrRetryCounted, ErrWorkerFatal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			err := tt.err

			// when / then
			if tt.match != nil && !errors.Is(err, tt.match) {
				t.Fatalf("errors.Is(%v, %v) = false, want true", err, tt.match)
			}

			for _, target := range tt.unmatch {
				if errors.Is(err, target) {
					t.Fatalf("errors.Is(%v, %v) = true, want false", err, target)
				}
			}
		})
	}
}
