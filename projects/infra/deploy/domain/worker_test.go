package domain

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWorker_RunTerminalNoRequeue(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	queue := NewQueue()
	if err := queue.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	reconciler := &fakeReconcileService{
		fn: func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
			queue.stop()
			return &ProcessResult{Terminal: true}, nil
		},
	}
	worker := NewWorker(queue, reconciler)

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	waitWorkerDone(t, done)
	if reconciler.processOneCalls != 1 {
		t.Fatalf("ProcessOne calls = %d, want 1", reconciler.processOneCalls)
	}
}

func TestWorker_RunChangedRequeues(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	queue := NewQueue()
	defer queue.stop()
	if err := queue.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	reconciler := &fakeReconcileService{}
	done := make(chan struct{})
	reconciler.fn = func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
		if reconciler.processOneCalls >= 2 {
			close(done)
			queue.stop()
			return &ProcessResult{Terminal: true}, nil
		}
		return &ProcessResult{Changed: true}, nil
	}
	worker := NewWorker(queue, reconciler)

	go func() {
		worker.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not requeue after Changed=true")
	}
	if reconciler.processOneCalls < 2 {
		t.Fatalf("ProcessOne calls = %d, want >= 2", reconciler.processOneCalls)
	}
}

func TestWorker_RunErrRetryCountedRequeues(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	queue := NewQueue()
	defer queue.stop()
	if err := queue.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	reconciler := &fakeReconcileService{}
	done := make(chan struct{})
	reconciler.fn = func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
		if reconciler.processOneCalls >= 2 {
			close(done)
			queue.stop()
			return &ProcessResult{Terminal: true}, nil
		}
		return &ProcessResult{}, fmt.Errorf("apply failed: %w", ErrRetryCounted)
	}
	worker := NewWorker(queue, reconciler)

	go func() {
		worker.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not requeue after ErrRetryCounted")
	}
	if reconciler.processOneCalls < 2 {
		t.Fatalf("ProcessOne calls = %d, want >= 2", reconciler.processOneCalls)
	}
}

func TestWorker_RunMaxRetryExhausted(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	queue := NewQueue()
	if err := queue.EnqueueAfter(ctx, &WorkItem{
		EnvName:    envName,
		RetryCount: defaultMaxRetries,
		Source:     WorkItemSourceRetry,
	}, 0); err != nil {
		t.Fatalf("EnqueueAfter() error = %v", err)
	}

	reconciler := &fakeReconcileService{}
	done := make(chan struct{})
	reconciler.fn = func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
		return &ProcessResult{}, fmt.Errorf("apply failed: %w", ErrRetryCounted)
	}
	reconciler.markRetryExhaustedFn = func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
		close(done)
		queue.stop()
		return &ProcessResult{Changed: true, Terminal: true}, nil
	}
	worker := NewWorker(queue, reconciler)

	go func() {
		worker.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not call MarkRetryExhausted")
	}

	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	if reconciler.markRetryExhaustedCalls != 1 {
		t.Fatalf("MarkRetryExhausted calls = %d, want 1", reconciler.markRetryExhaustedCalls)
	}
}

func TestWorker_RunContextCanceledNoRequeue(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	queue := NewQueue()
	if err := queue.Enqueue(ctx, envName); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	reconciler := &fakeReconcileService{
		fn: func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
			queue.stop()
			return &ProcessResult{}, context.Canceled
		},
	}
	worker := NewWorker(queue, reconciler)

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	waitWorkerDone(t, done)
	if reconciler.processOneCalls != 1 {
		t.Fatalf("ProcessOne calls = %d, want 1 (no requeue)", reconciler.processOneCalls)
	}
}

func TestWorker_RunInfiniteRetry(t *testing.T) {
	// Verify retry continues even when RetryCount exceeds the old defaultMaxRetries
	// (the worker has no max retries cap). We check that EnqueueAfter with
	// RetryCount=5 is still accepted and ProcessOne is invoked.
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	queue := NewQueue()
	if err := queue.EnqueueAfter(ctx, &WorkItem{
		EnvName:    envName,
		RetryCount: 5,
		Source:     WorkItemSourceRetry,
	}, 0); err != nil {
		t.Fatalf("EnqueueAfter() error = %v", err)
	}

	done := make(chan struct{})
	reconciler := &fakeReconcileService{}
	reconciler.fn = func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
		close(done)
		queue.stop()
		// Return ErrRetryCounted to verify it still triggers a retry (not dropped)
		return &ProcessResult{}, fmt.Errorf("apply failed: %w", ErrRetryCounted)
	}
	worker := NewWorker(queue, reconciler)

	go func() {
		worker.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker dropped item at RetryCount=5")
	}

	if reconciler.processOneCalls != 1 {
		t.Fatalf("ProcessOne calls = %d, want 1", reconciler.processOneCalls)
	}
}

func TestWorker_RunCountedRetryBackoffIsNonBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	envNameA := mustWorkerEnvName(t, "dev", "alpha")
	envNameB := mustWorkerEnvName(t, "dev", "beta")
	queue := NewQueue()
	defer queue.stop()
	if err := queue.Enqueue(ctx, envNameA); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(ctx, envNameB); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	processedB := make(chan struct{})
	reconciler := &fakeReconcileService{
		fn: func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
			switch name {
			case envNameA:
				return &ProcessResult{}, fmt.Errorf("apply failed: %w", ErrRetryCounted)
			case envNameB:
				close(processedB)
				queue.stop()
				return &ProcessResult{Changed: true, Terminal: true}, nil
			default:
				return &ProcessResult{Terminal: true}, nil
			}
		},
	}
	worker := NewWorker(queue, reconciler)

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	select {
	case <-processedB:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() blocked on retry backoff")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return")
	}
}

func TestWorker_RunContinuesAfterFatalError(t *testing.T) {
	ctx := context.Background()
	envNameA := mustWorkerEnvName(t, "dev", "alpha")
	envNameB := mustWorkerEnvName(t, "dev", "beta")
	queue := NewQueue()
	defer queue.stop()
	if err := queue.Enqueue(ctx, envNameA); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(ctx, envNameB); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	processedB := make(chan struct{})
	reconciler := &fakeReconcileService{
		fn: func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
			if name == envNameA {
				return &ProcessResult{}, fmt.Errorf("storage unavailable")
			}
			close(processedB)
			queue.stop()
			return &ProcessResult{Changed: true, Terminal: true}, nil
		},
	}
	worker := NewWorker(queue, reconciler)

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	select {
	case <-processedB:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() stopped after fatal iteration error")
	}

	waitWorkerDone(t, done)
}

func TestWorker_RunReturnsAfterQueueStop(t *testing.T) {
	ctx := context.Background()
	queue := NewQueue()
	worker := NewWorker(queue, &fakeReconcileService{})

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	queue.stop()
	waitWorkerDone(t, done)
}

func TestWorker_RunIterationTimeout(t *testing.T) {
	ctx := context.Background()
	envNameA := mustWorkerEnvName(t, "dev", "alpha")
	envNameB := mustWorkerEnvName(t, "dev", "beta")
	queue := NewQueue()
	defer queue.stop()
	if err := queue.Enqueue(ctx, envNameA); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	startedA := make(chan struct{})
	processedB := make(chan struct{})
	reconciler := &fakeReconcileService{
		fn: func(ctx context.Context, name EnvironmentName) (*ProcessResult, error) {
			if name == envNameA {
				close(startedA)
				<-ctx.Done()
				return &ProcessResult{}, ctx.Err()
			}
			close(processedB)
			queue.stop()
			return &ProcessResult{Changed: true, Terminal: true}, nil
		},
	}
	worker := NewWorker(queue, reconciler)
	worker.iterTimeout = 20 * time.Millisecond

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Wait for envNameA to be picked up before enqueuing envNameB,
	// so the order is deterministic regardless of goroutine scheduling.
	select {
	case <-startedA:
	case <-time.After(2 * time.Second):
		t.Fatal("envNameA was not picked up")
	}

	if err := queue.Enqueue(ctx, envNameB); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	select {
	case <-processedB:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() stopped after iteration timeout")
	}

	waitWorkerDone(t, done)
	if reconciler.processOneCalls != 2 {
		t.Fatalf("ProcessOne calls = %d, want 2", reconciler.processOneCalls)
	}
}

// fakeReconcileService is a test double for ReconcileService.
type fakeReconcileService struct {
	mu                      sync.Mutex
	processOneCalls         int
	markRetryExhaustedFn    func(ctx context.Context, envName EnvironmentName) (*ProcessResult, error)
	markRetryExhaustedCalls int
	fn                      func(ctx context.Context, envName EnvironmentName) (*ProcessResult, error)
}

func (f *fakeReconcileService) ProcessOne(ctx context.Context, envName EnvironmentName) (*ProcessResult, error) {
	f.mu.Lock()
	f.processOneCalls++
	f.mu.Unlock()

	if f.fn != nil {
		return f.fn(ctx, envName)
	}
	return &ProcessResult{Terminal: true}, nil
}

func (f *fakeReconcileService) MarkRetryExhausted(ctx context.Context, envName EnvironmentName) (*ProcessResult, error) {
	f.mu.Lock()
	f.markRetryExhaustedCalls++
	f.mu.Unlock()

	if f.markRetryExhaustedFn != nil {
		return f.markRetryExhaustedFn(ctx, envName)
	}
	return &ProcessResult{Changed: true, Terminal: true}, nil
}

func mustWorkerEnvName(t *testing.T, scope, env string) EnvironmentName {
	t.Helper()
	name, err := NewEnvironmentName(scope, env)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}
	return name
}

func waitWorkerDone(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}
}
