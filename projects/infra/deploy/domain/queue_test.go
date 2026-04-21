package domain

import (
	"context"
	"testing"
	"time"
)

func mustEnvName(t *testing.T, scope, env string) EnvironmentName {
	t.Helper()
	name, err := NewEnvironmentName(scope, env)
	if err != nil {
		t.Fatalf("NewEnvironmentName(%q, %q) failed: %v", scope, env, err)
	}
	return name
}

func assertWorkItemEqual(t *testing.T, got *WorkItem, want WorkItem) {
	t.Helper()
	if got == nil {
		t.Fatal("got nil WorkItem")
	}
	if got.EnvName != want.EnvName || got.RetryCount != want.RetryCount {
		t.Fatalf("WorkItem = %+v, want %+v", *got, want)
	}
}

func TestQueue_Dequeue_FIFO(t *testing.T) {
	tests := []struct {
		name string
		envs []EnvironmentName
		want []WorkItem
	}{
		{
			name: "items come out in enqueue order",
			envs: []EnvironmentName{
				mustEnvName(t, "scope1", "env1"),
				mustEnvName(t, "scope1", "env2"),
			},
			want: []WorkItem{
				{EnvName: mustEnvName(t, "scope1", "env1"), RetryCount: 0},
				{EnvName: mustEnvName(t, "scope1", "env2"), RetryCount: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			q := NewQueue()
			ctx := context.Background()

			if err := q.Start(ctx); err != nil {
				t.Fatalf("Start() failed: %v", err)
			}
			defer q.Stop()

			// when
			for _, envName := range tt.envs {
				if err := q.Enqueue(ctx, envName); err != nil {
					t.Fatalf("Enqueue(%v) failed: %v", envName, err)
				}
			}

			// then
			for i, want := range tt.want {
				got, ok := q.Dequeue(ctx)
				if !ok {
					t.Fatalf("Dequeue() returned not ok at index %d", i)
				}
				assertWorkItemEqual(t, got, want)
				q.Complete(got.EnvName)
			}
		})
	}
}

func TestQueue_Enqueue_DedupQueued(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "env")

	if err := q.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer q.Stop()

	// when
	if err := q.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() failed: %v", err)
	}
	if err := q.Enqueue(ctx, envName); err != nil {
		t.Fatalf("second Enqueue() failed: %v", err)
	}

	// then
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue() returned not ok")
	}
	assertWorkItemEqual(t, got, WorkItem{EnvName: envName, RetryCount: 0})
	q.Complete(envName)

	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(timeoutCtx)
	if ok {
		t.Fatal("expected no second item after dedup")
	}
}

func TestQueue_Enqueue_UserOverridesQueuedRetry(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "override")
	retryItem := &WorkItem{EnvName: envName, RetryCount: 3}

	if err := q.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer q.Stop()

	if err := q.EnqueueRetry(ctx, retryItem); err != nil {
		t.Fatalf("EnqueueRetry() failed: %v", err)
	}

	// when
	if err := q.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() failed: %v", err)
	}

	// then
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue() returned not ok")
	}
	assertWorkItemEqual(t, got, WorkItem{EnvName: envName, RetryCount: 0})
}

func TestQueue_EnqueueRetry_DropsWhenUserTaskQueued(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "userq")

	if err := q.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer q.Stop()

	if err := q.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() failed: %v", err)
	}

	// when
	if err := q.EnqueueRetry(ctx, &WorkItem{EnvName: envName, RetryCount: 5}); err != nil {
		t.Fatalf("EnqueueRetry() failed: %v", err)
	}

	// then
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue() returned not ok")
	}
	assertWorkItemEqual(t, got, WorkItem{EnvName: envName, RetryCount: 0})
	q.Complete(envName)

	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(timeoutCtx)
	if ok {
		t.Fatal("expected retry to be dropped when user task already queued")
	}
}

func TestQueue_Complete_RequeuesFollowUpAfterInFlightUserEnqueue(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "inflight")

	if err := q.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer q.Stop()

	if err := q.EnqueueRetry(ctx, &WorkItem{EnvName: envName, RetryCount: 2}); err != nil {
		t.Fatalf("EnqueueRetry() failed: %v", err)
	}

	first, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("first Dequeue() returned not ok")
	}
	assertWorkItemEqual(t, first, WorkItem{EnvName: envName, RetryCount: 2})

	// when
	if err := q.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() while in flight failed: %v", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(timeoutCtx)
	if ok {
		t.Fatal("expected no parallel dequeue while item is in flight")
	}

	q.Complete(envName)

	// then
	second, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("second Dequeue() returned not ok")
	}
	assertWorkItemEqual(t, second, WorkItem{EnvName: envName, RetryCount: 0})
}

func TestQueue_EnqueueRetry_InFlightKeepsUserFollowUp(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "followup")

	if err := q.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer q.Stop()

	if err := q.EnqueueRetry(ctx, &WorkItem{EnvName: envName, RetryCount: 1}); err != nil {
		t.Fatalf("EnqueueRetry() failed: %v", err)
	}

	first, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("first Dequeue() returned not ok")
	}

	if err := q.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() while in flight failed: %v", err)
	}

	// when
	if err := q.EnqueueRetry(ctx, &WorkItem{EnvName: envName, RetryCount: 4}); err != nil {
		t.Fatalf("second EnqueueRetry() failed: %v", err)
	}

	q.Complete(first.EnvName)

	// then
	second, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("second Dequeue() returned not ok")
	}
	assertWorkItemEqual(t, second, WorkItem{EnvName: envName, RetryCount: 0})
	q.Complete(second.EnvName)

	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(timeoutCtx)
	if ok {
		t.Fatal("expected retry follow-up to be dropped when user follow-up exists")
	}
}

func TestQueue_Dequeue_ContextCancellation(t *testing.T) {
	// given
	q := NewQueue()
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer q.Stop()

	cancelCtx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// when
	_, ok := q.Dequeue(cancelCtx)

	// then
	if ok {
		t.Fatal("expected Dequeue() to return false when context is cancelled")
	}
}

func TestQueue_Stop_DrainsDequeue(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	if err := q.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	done := make(chan struct{})

	// when
	go func() {
		_, _ = q.Dequeue(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	q.Stop()

	// then
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Dequeue() did not return after Stop()")
	}
}
