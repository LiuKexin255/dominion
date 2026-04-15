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

func TestQueue_Dequeue_BasicOrder(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	env1 := mustEnvName(t, "scope1", "env1")
	env2 := mustEnvName(t, "scope1", "env2")

	// when
	_ = q.Start(ctx)
	defer q.Stop()

	if err := q.Enqueue(ctx, env1); err != nil {
		t.Fatalf("Enqueue env1 failed: %v", err)
	}
	if err := q.Enqueue(ctx, env2); err != nil {
		t.Fatalf("Enqueue env2 failed: %v", err)
	}

	// then - items come out in FIFO order
	got1, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok for first item")
	}
	if got1 != env1 {
		t.Fatalf("first Dequeue = %v, want %v", got1, env1)
	}

	got2, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok for second item")
	}
	if got2 != env2 {
		t.Fatalf("second Dequeue = %v, want %v", got2, env2)
	}
}

func TestQueue_PriorityBeforeNormal(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	normalEnv := mustEnvName(t, "scope1", "normal")
	priorityEnv := mustEnvName(t, "scope1", "delete")

	_ = q.Start(ctx)
	defer q.Stop()

	// when - enqueue normal first, then priority
	if err := q.Enqueue(ctx, normalEnv); err != nil {
		t.Fatalf("Enqueue normal failed: %v", err)
	}
	if err := q.EnqueueWithPriority(ctx, priorityEnv); err != nil {
		t.Fatalf("EnqueueWithPriority failed: %v", err)
	}

	// then - priority item comes out first
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok")
	}
	if got != priorityEnv {
		t.Fatalf("Dequeue = %v, want priority item %v", got, priorityEnv)
	}
}

func TestQueue_Dedup_SameEnvName(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	env := mustEnvName(t, "scope1", "dup")

	_ = q.Start(ctx)
	defer q.Stop()

	// when - enqueue same envName twice
	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("first Enqueue failed: %v", err)
	}
	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("second Enqueue failed: %v", err)
	}

	// then - only one item in queue
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok")
	}
	if got != env {
		t.Fatalf("Dequeue = %v, want %v", got, env)
	}

	// next Dequeue should block; verify with timeout
	deepeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(deepeCtx)
	if ok {
		t.Fatal("expected Dequeue to return false after dedup, but got item")
	}
}

func TestQueue_Dedup_PriorityUpgrade(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	env := mustEnvName(t, "scope1", "upgrade")

	_ = q.Start(ctx)
	defer q.Stop()

	// when - enqueue normal, then priority for same envName
	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	if err := q.EnqueueWithPriority(ctx, env); err != nil {
		t.Fatalf("EnqueueWithPriority failed: %v", err)
	}

	// then - env comes out (possibly upgraded to priority)
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok")
	}
	if got != env {
		t.Fatalf("Dequeue = %v, want %v", got, env)
	}

	// should be deduplicated - only one item
	deepeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(deepeCtx)
	if ok {
		t.Fatal("expected Dequeue to return false after dedup")
	}
}

func TestQueue_Dedup_NormalDoesNotUpgrade(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	env := mustEnvName(t, "scope1", "noup")

	_ = q.Start(ctx)
	defer q.Stop()

	// when - enqueue priority first, then normal for same envName
	if err := q.EnqueueWithPriority(ctx, env); err != nil {
		t.Fatalf("EnqueueWithPriority failed: %v", err)
	}
	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// then - single item, still in priority lane
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok")
	}
	if got != env {
		t.Fatalf("Dequeue = %v, want %v", got, env)
	}

	deepeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, ok = q.Dequeue(deepeCtx)
	if ok {
		t.Fatal("expected Dequeue to return false after dedup")
	}
}

func TestQueue_Dequeue_Blocking(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	_ = q.Start(ctx)
	defer q.Stop()

	// when - Dequeue on empty queue blocks until item arrives or context cancelled
	done := make(chan struct{})
	go func() {
		_, _ = q.Dequeue(ctx)
		close(done)
	}()

	// then - Dequeue should still be blocking after a short wait
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Dequeue should block when queue is empty")
	default:
	}
}

func TestQueue_Dequeue_ContextCancellation(t *testing.T) {
	// given
	q := NewQueue()
	_ = q.Start(context.Background())
	defer q.Stop()

	cancelCtx, cancel := context.WithCancel(context.Background())

	// when - cancel context while Dequeue is blocking
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// then
	_, ok := q.Dequeue(cancelCtx)
	if ok {
		t.Fatal("expected Dequeue to return false on context cancellation")
	}
}

func TestQueue_Stop_DrainsDequeue(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	_ = q.Start(ctx)

	// when - a goroutine is blocked on Dequeue, then Stop is called
	done := make(chan struct{})
	go func() {
		_, _ = q.Dequeue(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	q.Stop()

	// then - Dequeue should return after Stop
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Dequeue did not return after Stop")
	}
}

func TestQueue_Requeue_AfterDequeue(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	env := mustEnvName(t, "scope1", "requeue")

	_ = q.Start(ctx)
	defer q.Stop()

	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// when - dequeue then re-enqueue same envName
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok")
	}
	if got != env {
		t.Fatalf("Dequeue = %v, want %v", got, env)
	}

	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("second Enqueue failed: %v", err)
	}

	// then - can dequeue again
	got2, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("second Dequeue returned not ok")
	}
	if got2 != env {
		t.Fatalf("second Dequeue = %v, want %v", got2, env)
	}
}

func TestQueue_EnqueueBeforeStart(t *testing.T) {
	// given
	q := NewQueue()
	ctx := context.Background()

	env := mustEnvName(t, "scope1", "early")

	// when - enqueue before Start
	if err := q.Enqueue(ctx, env); err != nil {
		t.Fatalf("Enqueue before Start failed: %v", err)
	}

	_ = q.Start(ctx)
	defer q.Stop()

	// then - item is available
	got, ok := q.Dequeue(ctx)
	if !ok {
		t.Fatal("Dequeue returned not ok")
	}
	if got != env {
		t.Fatalf("Dequeue = %v, want %v", got, env)
	}
}
