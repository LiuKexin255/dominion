package domain

import (
	"context"
	"fmt"
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

func TestQueue_Dequeue_AllItemsDelivered(t *testing.T) {
	tests := []struct {
		name string
		envs []EnvironmentName
		want []WorkItem
	}{
		{
			name: "all enqueued items are delivered",
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

			defer q.stop()

			// when
			for _, envName := range tt.envs {
				if err := q.Enqueue(ctx, envName); err != nil {
					t.Fatalf("Enqueue(%v) failed: %v", envName, err)
				}
			}

			// then
			got := make(map[EnvironmentName]*WorkItem)
			for range tt.want {
				item, ok := q.Dequeue(ctx)
				if !ok {
					t.Fatal("Dequeue() returned not ok")
				}
				got[item.EnvName] = item
				q.Complete(item.EnvName)
			}
			for _, w := range tt.want {
				item, ok := got[w.EnvName]
				if !ok {
					t.Fatalf("expected item for %v, not found", w.EnvName)
				}
				assertWorkItemEqual(t, item, w)
			}
		})
	}
}

func TestQueue_Enqueue_DedupQueued(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "env")

	defer q.stop()

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
	retryItem := &WorkItem{EnvName: envName, RetryCount: 3, Source: WorkItemSourceRetry}

	defer q.stop()

	if err := q.EnqueueAfter(ctx, retryItem, 0); err != nil {
		t.Fatalf("EnqueueAfter() failed: %v", err)
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

func TestQueue_Complete_RequeuesFollowUpAfterInFlightUserEnqueue(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()
	envName := mustEnvName(t, "scope1", "inflight")

	defer q.stop()

	if err := q.EnqueueAfter(ctx, &WorkItem{EnvName: envName, RetryCount: 2, Source: WorkItemSourceRetry}, 0); err != nil {
		t.Fatalf("EnqueueAfter() failed: %v", err)
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

func TestQueue_Dequeue_ContextCancellation(t *testing.T) {
	// given
	q := NewQueue()
	defer q.stop()

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

	done := make(chan struct{})

	// when
	go func() {
		_, _ = q.Dequeue(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	q.stop()

	// then
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Dequeue() did not return after Stop()")
	}
}

func TestQueue_EnqueueAfter_Delay(t *testing.T) {
	tests := []struct {
		name  string
		delay time.Duration
	}{
		{name: "zero delay enqueues immediately", delay: 0},
		{name: "delayed item appears after wait", delay: 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQueue()
			ctx := context.Background()
			envName := mustEnvName(t, "scope1", "delay")
			item := &WorkItem{EnvName: envName, RetryCount: 0, Source: WorkItemSourcePoll}

			defer q.stop()

			if err := q.EnqueueAfter(ctx, item, tt.delay); err != nil {
				t.Fatalf("EnqueueAfter() failed: %v", err)
			}

			if tt.delay == 0 {
				got, ok := q.Dequeue(ctx)
				if !ok {
					t.Fatal("Dequeue() returned not ok for zero delay")
				}
				assertWorkItemEqual(t, got, WorkItem{EnvName: envName, RetryCount: 0})
				q.Complete(got.EnvName)
				return
			}

			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			_, ok := q.Dequeue(timeoutCtx)
			if ok {
				t.Fatal("expected item NOT to appear before delay expires")
			}

			got, ok := q.Dequeue(ctx)
			if !ok {
				t.Fatal("Dequeue() returned not ok after delay")
			}
			assertWorkItemEqual(t, got, WorkItem{EnvName: envName, RetryCount: 0})
			q.Complete(got.EnvName)
		})
	}
}

func TestQueue_EnqueueAfter_Dedup(t *testing.T) {
	tests := []struct {
		name       string
		setupFirst *WorkItem   // first item enqueued (via EnqueueAfter with 0 delay)
		inFlight   bool        // whether to make the first item in-flight via a dummy
		second     *WorkItem   // second item enqueued via EnqueueAfter
		wantSource WorkItemSource // expected source of dequeued item
	}{
		{
			name:       "poll does not override user in items",
			setupFirst: &WorkItem{Source: WorkItemSourceUser, RetryCount: 0},
			second:     &WorkItem{Source: WorkItemSourcePoll, RetryCount: 0},
			wantSource: WorkItemSourceUser,
		},
		{
			name:       "poll does not override retry in items",
			setupFirst: &WorkItem{Source: WorkItemSourceRetry, RetryCount: 3},
			second:     &WorkItem{Source: WorkItemSourcePoll, RetryCount: 0},
			wantSource: WorkItemSourceRetry,
		},
		{
			name:       "poll overrides poll in items",
			setupFirst: &WorkItem{Source: WorkItemSourcePoll, RetryCount: 1},
			second:     &WorkItem{Source: WorkItemSourcePoll, RetryCount: 2},
			wantSource: WorkItemSourcePoll,
		},
		{
			name:       "retry overrides poll in items",
			setupFirst: &WorkItem{Source: WorkItemSourcePoll, RetryCount: 1},
			second:     &WorkItem{Source: WorkItemSourceRetry, RetryCount: 3},
			wantSource: WorkItemSourceRetry,
		},
		{
			name:       "user overrides poll in items",
			setupFirst: &WorkItem{Source: WorkItemSourcePoll, RetryCount: 1},
			second:     &WorkItem{Source: WorkItemSourceUser, RetryCount: 0},
			wantSource: WorkItemSourceUser,
		},
		{
			name:       "user overrides retry in items",
			setupFirst: &WorkItem{Source: WorkItemSourceRetry, RetryCount: 3},
			second:     &WorkItem{Source: WorkItemSourceUser, RetryCount: 0},
			wantSource: WorkItemSourceUser,
		},
		{
			name:       "retry does not override user in items",
			setupFirst: &WorkItem{Source: WorkItemSourceUser, RetryCount: 0},
			second:     &WorkItem{Source: WorkItemSourceRetry, RetryCount: 3},
			wantSource: WorkItemSourceUser,
		},
		{
			name:       "poll does not override user in followUp",
			setupFirst: &WorkItem{Source: WorkItemSourceUser, RetryCount: 0},
			inFlight:   true,
			second:     &WorkItem{Source: WorkItemSourcePoll, RetryCount: 0},
			wantSource: WorkItemSourceUser,
		},
		{
			name:       "poll does not override retry in followUp",
			setupFirst: &WorkItem{Source: WorkItemSourceRetry, RetryCount: 3},
			inFlight:   true,
			second:     &WorkItem{Source: WorkItemSourcePoll, RetryCount: 0},
			wantSource: WorkItemSourceRetry,
		},
		{
			name:       "poll overrides poll in followUp",
			setupFirst: &WorkItem{Source: WorkItemSourcePoll, RetryCount: 1},
			inFlight:   true,
			second:     &WorkItem{Source: WorkItemSourcePoll, RetryCount: 2},
			wantSource: WorkItemSourcePoll,
		},
		{
			name:       "retry overrides poll in followUp",
			setupFirst: &WorkItem{Source: WorkItemSourcePoll, RetryCount: 1},
			inFlight:   true,
			second:     &WorkItem{Source: WorkItemSourceRetry, RetryCount: 3},
			wantSource: WorkItemSourceRetry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQueue()
			ctx := context.Background()
			envName := mustEnvName(t, "scope1", "dedup")

			defer q.stop()

			first := *tt.setupFirst
			first.EnvName = envName
			second := *tt.second
			second.EnvName = envName

			if tt.inFlight {
				dummy := &WorkItem{EnvName: envName, RetryCount: 0, Source: WorkItemSourcePoll}
				if err := q.EnqueueAfter(ctx, dummy, 0); err != nil {
					t.Fatalf("EnqueueAfter(dummy) failed: %v", err)
				}
				_, ok := q.Dequeue(ctx)
				if !ok {
					t.Fatal("Dequeue() for dummy returned not ok")
				}

				followUpSetter := first
				followUpSetter.EnvName = envName
				if err := q.EnqueueAfter(ctx, &followUpSetter, 0); err != nil {
					t.Fatalf("EnqueueAfter(followUp) failed: %v", err)
				}
			} else {
				if err := q.EnqueueAfter(ctx, &first, 0); err != nil {
					t.Fatalf("EnqueueAfter(first) failed: %v", err)
				}
			}

			if err := q.EnqueueAfter(ctx, &second, 0); err != nil {
				t.Fatalf("EnqueueAfter(second) failed: %v", err)
			}

			if tt.inFlight {
				q.Complete(envName)
			}

			got, ok := q.Dequeue(ctx)
			if !ok {
				t.Fatal("Dequeue() returned not ok")
			}
			if got.Source != tt.wantSource {
				t.Fatalf("Source = %v, want %v", got.Source, tt.wantSource)
			}
			q.Complete(got.EnvName)

			timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			_, ok = q.Dequeue(timeoutCtx)
			if ok {
				t.Fatal("expected no second item after dedup")
			}
		})
	}
}

func TestQueue_EnqueueAfter_ContextCancel(t *testing.T) {
	q := NewQueue()
	envName := mustEnvName(t, "scope1", "cancel")
	item := &WorkItem{EnvName: envName, RetryCount: 0, Source: WorkItemSourcePoll}

	defer q.stop()

	cancelCtx, cancel := context.WithCancel(context.Background())

	if err := q.EnqueueAfter(cancelCtx, item, 200*time.Millisecond); err != nil {
		t.Fatalf("EnqueueAfter() failed: %v", err)
	}

	cancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer timeoutCancel()
	_, ok := q.Dequeue(timeoutCtx)
	if ok {
		t.Fatal("expected cancelled EnqueueAfter item to be dropped")
	}
}

func TestQueue_EnqueueAfter_NoDeadlock(t *testing.T) {
	q := NewQueue()
	ctx := context.Background()

	defer q.stop()

	for i := 0; i < maxQueueCap; i++ {
		envName := mustEnvName(t, "scope1", fmt.Sprintf("fill%d", i))
		if err := q.Enqueue(ctx, envName); err != nil {
			t.Fatalf("Enqueue %d failed: %v", i, err)
		}
	}

	consumerStarted := make(chan struct{})
	go func() {
		close(consumerStarted)
		for {
			item, ok := q.Dequeue(ctx)
			if !ok {
				return
			}
			q.Complete(item.EnvName)
		}
	}()

	<-consumerStarted
	time.Sleep(50 * time.Millisecond)

	envName := mustEnvName(t, "scope1", "overflow")
	done := make(chan error, 1)
	go func() {
		done <- q.Enqueue(ctx, envName)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK: Enqueue blocked on full pendingCh")
	}
}

func TestQueue_RetryCount(t *testing.T) {
	q := NewQueue()
	ctx := context.Background()

	defer q.stop()

	t.Run("retry preserves incremented count", func(t *testing.T) {
		envName := mustEnvName(t, "scope1", "retrycnt")
		item := &WorkItem{EnvName: envName, RetryCount: 3, Source: WorkItemSourceRetry}

		if err := q.EnqueueAfter(ctx, item, 0); err != nil {
			t.Fatalf("EnqueueAfter() failed: %v", err)
		}

		got, ok := q.Dequeue(ctx)
		if !ok {
			t.Fatal("Dequeue() returned not ok")
		}
		if got.RetryCount != 3 {
			t.Fatalf("RetryCount = %d, want 3", got.RetryCount)
		}
		q.Complete(got.EnvName)
	})

	t.Run("poll does not change retry count", func(t *testing.T) {
		envName := mustEnvName(t, "scope1", "pollcnt")
		item := &WorkItem{EnvName: envName, RetryCount: 2, Source: WorkItemSourcePoll}

		if err := q.EnqueueAfter(ctx, item, 0); err != nil {
			t.Fatalf("EnqueueAfter() failed: %v", err)
		}

		got, ok := q.Dequeue(ctx)
		if !ok {
			t.Fatal("Dequeue() returned not ok")
		}
		if got.RetryCount != 2 {
			t.Fatalf("RetryCount = %d, want 2", got.RetryCount)
		}
		q.Complete(got.EnvName)
	})
}
