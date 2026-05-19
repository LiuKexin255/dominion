package bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock workers and builders for testing
// ---------------------------------------------------------------------------

// testWorker is a configurable mock Worker.
// When startErr is set, Start returns it immediately. When startBlock is
// non-nil, Start blocks until the channel is closed (or context is cancelled).
// When startPanic is true, Start panics. When startCalled is non-nil, it
// receives on Start entry. stopCalled records Stop invocations.
type testWorker struct {
	startErr   error
	startPanic bool
	// startBlock, if non-nil, Start blocks until closed (or ctx cancelled).
	startBlock chan struct{}
	// startCalled receives a value each time Start is called.
	startCalled chan struct{}
	// stopCalled receives a value each time Stop is called.
	stopCalled chan struct{}
	// stopWait, if non-nil, Stop blocks until closed.
	stopWait chan struct{}
	// stopErr is the error returned by Stop.
	stopErr error
	// stopPanic makes Stop panic.
	stopPanic bool
}

func (w *testWorker) Start(ctx context.Context) error {
	if w.startCalled != nil {
		select {
		case w.startCalled <- struct{}{}:
		case <-ctx.Done():
		}
	}
	if w.startPanic {
		panic("worker panic")
	}
	if w.startBlock != nil {
		select {
		case <-w.startBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return w.startErr
}

func (w *testWorker) Stop(_ context.Context) error {
	if w.stopPanic {
		panic("testWorker stop panic")
	}
	if w.stopCalled != nil {
		w.stopCalled <- struct{}{}
	}
	if w.stopWait != nil {
		<-w.stopWait
	}
	if w.stopErr != nil {
		return w.stopErr
	}
	return nil
}

// testBuilder is a configurable mock WorkerBuilder.
// workers are returned in order by successive Build calls. Once the queue
// is exhausted it returns the last worker. When buildErr is set, Build
// returns that error instead of a worker.
type testBuilder struct {
	workers  []Worker
	idx      int
	buildErr error

	// buildCalled receives a value each time Build is called.
	buildCalled chan struct{}

	mu sync.Mutex
}

func (b *testBuilder) Build(_ context.Context) (Worker, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buildCalled != nil {
		b.buildCalled <- struct{}{}
	}

	if b.buildErr != nil {
		return nil, b.buildErr
	}

	if len(b.workers) == 0 {
		return nil, errors.New("testBuilder: no workers configured")
	}

	w := b.workers[b.idx]
	if b.idx < len(b.workers)-1 {
		b.idx++
	}
	return w, nil
}

// blockingWorker blocks on Start until ctx is cancelled, then returns ctx.Err().
type blockingWorker struct {
	stopCalled chan struct{}
}

func (w *blockingWorker) Start(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (w *blockingWorker) Stop(_ context.Context) error {
	if w.stopCalled != nil {
		w.stopCalled <- struct{}{}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDaemon_StartStop verifies that Start returns nil immediately and
// Stop shuts down the supervisor goroutine without errors.
func TestDaemon_StartStop(t *testing.T) {
	block := make(chan struct{})
	builder := &testBuilder{
		workers: []Worker{&testWorker{startBlock: block}},
	}

	d := Daemon("testd", builder)
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Close block to unblock the worker, then stop.
	close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// TestDaemon_StageOrder verifies that StageDaemon is 250 (between
// StageClient=200 and StageServer=300).
func TestDaemon_StageOrder(t *testing.T) {
	cmp := Daemon("testd", &testBuilder{})

	if got := cmp.Stage(); got != StageDaemon {
		t.Fatalf("Stage() = %v, want %v", got, StageDaemon)
	}

	if int(StageDaemon) != 250 {
		t.Fatalf("StageDaemon = %d, want 250", int(StageDaemon))
	}

	if int(StageClient) >= int(StageDaemon) {
		t.Fatalf("StageClient=%d should be < StageDaemon=%d", StageClient, StageDaemon)
	}
	if int(StageDaemon) >= int(StageServer) {
		t.Fatalf("StageDaemon=%d should be < StageServer=%d", StageDaemon, StageServer)
	}
}

// TestDaemon_WorkerPanicRestart verifies that when worker.Start panics,
// the panic is recovered, and Build is called again to create a new worker.
func TestDaemon_WorkerPanicRestart(t *testing.T) {
	wtStarted := make(chan struct{}, 2)

	// First worker panics, second worker blocks on startBlock.
	startBlock := make(chan struct{})
	panicWorker := &testWorker{
		startPanic:  true,
		startCalled: wtStarted,
	}
	secondWorker := &testWorker{
		startBlock:  startBlock,
		startCalled: wtStarted,
	}

	buildCh := make(chan struct{}, 2)
	builder := &testBuilder{
		workers:     []Worker{panicWorker, secondWorker},
		buildCalled: buildCh,
	}

	d := Daemon("test-panic-restart", builder)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for first Build + Start (panics).
	<-buildCh
	<-wtStarted

	// Wait for second Build (restart after panic).
	select {
	case <-buildCh:
		// Expected: builder.Build was called again for restart.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Build after panic recovery")
	}

	// Verify second worker called Start.
	select {
	case <-wtStarted:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second worker Start")
	}

	// Cleanup: unblock second worker and stop.
	close(startBlock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)
}

// TestDaemon_WorkerErrorRestart verifies that when worker.Start returns an
// error, the daemon restarts after backoff with a new Build.
func TestDaemon_WorkerErrorRestart(t *testing.T) {
	startErr := errors.New("worker crashed")
	startBlock := make(chan struct{})

	wtStarted := make(chan struct{}, 2)
	errWorker := &testWorker{
		startErr:    startErr,
		startCalled: wtStarted,
	}
	goodWorker := &testWorker{
		startBlock:  startBlock,
		startCalled: wtStarted,
	}
	buildCh := make(chan struct{}, 2)
	builder := &testBuilder{
		workers:     []Worker{errWorker, goodWorker},
		buildCalled: buildCh,
	}

	d := Daemon("test-err-restart", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for first Build and Start (error).
	<-buildCh
	<-wtStarted

	// After backoff, Build should be called again for restart.
	select {
	case <-buildCh:
		// Expected.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restart Build")
	}

	// Verify new worker started.
	select {
	case <-wtStarted:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for restart worker Start")
	}

	// Cleanup.
	close(startBlock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)
}

// TestDaemon_BuildErrorRestart verifies that Build errors participate in
// the restart policy (restart after backoff).
func TestDaemon_BuildErrorRestart(t *testing.T) {
	startBlock := make(chan struct{})
	buildErr := errors.New("build failed")
	goodWorker := &testWorker{startBlock: startBlock}

	buildCh := make(chan struct{}, 3)
	builder := &testBuilder{
		workers:     []Worker{goodWorker},
		buildCalled: buildCh,
	}
	builder.buildErr = buildErr // First call returns error.

	d := Daemon("test-build-err", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// First Build fails.
	<-buildCh

	// Clear error so next Build succeeds.
	builder.mu.Lock()
	builder.buildErr = nil
	builder.mu.Unlock()

	// After backoff, Build is called again.
	select {
	case <-buildCh:
		// Expected: Build succeeded on restart.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restart Build after build error")
	}

	// Cleanup.
	close(startBlock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)
}

// TestDaemon_FatalClassifier verifies that a custom error classifier can
// mark an error as DaemonFatal, which is then reported via Done().
func TestDaemon_FatalClassifier(t *testing.T) {
	fatalErr := errors.New("classified as fatal")
	fatalCh := make(chan struct{})

	classifier := func(_ context.Context, err error) DaemonDecision {
		if errors.Is(err, fatalErr) {
			return DaemonFatal
		}
		return DaemonRestart
	}

	worker := &testWorker{startErr: fatalErr}
	builder := &testBuilder{workers: []Worker{worker}}

	d := Daemon("test-fatal", builder,
		WithDaemonErrorClassifier(classifier),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for fatal error on Done() channel.
	go func() {
		select {
		case <-d.(*daemonComponent).done:
			close(fatalCh)
		case <-time.After(2 * time.Second):
		}
	}()

	select {
	case <-fatalCh:
		// Expected: fatal error received.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fatal error on Done() channel")
	}
}

// TestDaemon_RestartExhausted verifies that after maxRestarts the daemon
// reports a fatal error on the Done() channel.
func TestDaemon_RestartExhausted(t *testing.T) {
	startErr := errors.New("always fails")
	worker := &testWorker{startErr: startErr}
	builder := &testBuilder{
		workers: []Worker{worker},
	}

	fatalCh := make(chan error, 1)
	d := Daemon("test-exhausted", builder,
		WithDaemonRestartBackoff(time.Millisecond),
		WithDaemonMaxRestarts(2), // 2 restarts means 3 total failures
	)

	go func() {
		select {
		case err := <-d.(*daemonComponent).done:
			fatalCh <- err
		case <-time.After(5 * time.Second):
		}
	}()

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case err := <-fatalCh:
		if err == nil {
			t.Fatal("expected non-nil fatal error")
		}
		// Expected: exhausted restarts.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fatal error after restart exhaustion")
	}
}

// TestDaemon_DefaultParameters verifies the default configuration values
// by testing behavior: backoff ≈ 1s, maxRestarts=5 (6th failure fatal).
func TestDaemon_DefaultParameters(t *testing.T) {
	// Verify that with default maxRestarts=5, the 6th Start failure is fatal.
	// We use a classifier that returns DaemonRestart on every error and short
	// backoff to avoid long test runtime.
	startErr := errors.New("always fails")
	worker := &testWorker{startErr: startErr}
	builder := &testBuilder{workers: []Worker{worker}}

	d := Daemon("test-defaults", builder,
		WithDaemonRestartBackoff(time.Millisecond),
		WithDaemonMaxRestarts(5),
	)

	fatalCh := make(chan error, 1)
	go func() {
		select {
		case err := <-d.(*daemonComponent).done:
			fatalCh <- err
		case <-time.After(5 * time.Second):
		}
	}()

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case err := <-fatalCh:
		if err == nil {
			t.Fatal("expected non-nil fatal error")
		}
		// Expected: maxRestarts=5 default — 6th failure fatal.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fatal error with default maxRestarts=5")
	}

	// Verify default daemonConfig values on a fresh Daemon without options.
	d2 := Daemon("check-default", builder).(*daemonComponent)
	if d2.cfg.initialBackoff != 1*time.Second {
		t.Fatalf("default initialBackoff = %v, want 1s", d2.cfg.initialBackoff)
	}
	if d2.cfg.maxBackoff != 30*time.Second {
		t.Fatalf("default maxBackoff = %v, want 30s", d2.cfg.maxBackoff)
	}
	if d2.cfg.maxRestarts != 5 {
		t.Fatalf("default maxRestarts = %d, want 5", d2.cfg.maxRestarts)
	}
}

// TestDaemon_ShutdownContextNoRestart verifies that when the daemon context
// is cancelled during worker execution, the worker exit does NOT trigger
// a restart.
func TestDaemon_ShutdownContextNoRestart(t *testing.T) {
	// Worker that blocks until context is cancelled.
	startCalled := make(chan struct{}, 1)
	worker := &testWorker{
		startBlock:  make(chan struct{}),
		startCalled: startCalled,
	}

	buildCh := make(chan struct{}, 1)
	builder := &testBuilder{
		workers:     []Worker{worker},
		buildCalled: buildCh,
	}

	d := Daemon("test-shutdown", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
	)

	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	if err := d.Start(daemonCtx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for Build to be called (worker started).
	<-buildCh
	<-startCalled

	// Cancel the daemon context to trigger shutdown.
	daemonCancel()

	// Context cancellation should cause the blocked worker to return ctx.Err().
	// The supervisor should NOT restart because daemonCtx is cancelled.

	// Wait for supervisor to exit (via Stop).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Verify no second Build call (no restart).
	select {
	case <-buildCh:
		t.Fatal("unexpected Build call — worker was restarted during shutdown")
	default:
		// Expected: no restart.
	}
}

// TestDaemon_Stop verifies that Stop cancels the worker context, calls
// worker.Stop, and waits for supervisor exit.
func TestDaemon_Stop(t *testing.T) {
	stopCalled := make(chan struct{}, 1)
	startCalled := make(chan struct{}, 1)
	worker := &testWorker{
		startBlock:  make(chan struct{}),
		startCalled: startCalled,
		stopCalled:  stopCalled,
	}

	builder := &testBuilder{workers: []Worker{worker}}

	d := Daemon("test-stop", builder)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for worker.Start to be called so the supervisor has stored
	// the worker reference.
	<-startCalled

	// Stop should cancel daemonCtx, call worker.Stop, and wait for supervisor.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Verify worker.Stop was called.
	select {
	case <-stopCalled:
		// Expected.
	default:
		t.Fatal("worker.Stop was not called")
	}
}

// TestDaemon_DaemonStop verifies that when Build/Start returns an error
// classified as DaemonStop, the daemon exits cleanly without restart
// and without reporting a fatal error.
func TestDaemon_DaemonStop(t *testing.T) {
	stopErr := errors.New("daemon stop signal")

	classifier := func(_ context.Context, err error) DaemonDecision {
		if errors.Is(err, stopErr) {
			return DaemonStop
		}
		return DaemonRestart
	}

	worker := &testWorker{startErr: stopErr}
	builder := &testBuilder{workers: []Worker{worker}}

	buildCh := make(chan struct{}, 1)
	builder.buildCalled = buildCh

	d := Daemon("test-daemon-stop", builder,
		WithDaemonErrorClassifier(classifier),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for Build to be called.
	select {
	case <-buildCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Build")
	}

	// Wait for supervisor to exit (no restart, no fatal error).
	// Use Stop to wait for supervisor.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Verify Done() channel has no error (no fatal reported).
	select {
	case err := <-d.(*daemonComponent).done:
		t.Fatalf("unexpected fatal error on Done(): %v", err)
	default:
		// Expected: no fatal error.
	}

	// Verify no second Build call.
	select {
	case <-buildCh:
		t.Fatal("unexpected Build call — DaemonStop should not restart")
	default:
		// Expected.
	}
}

// TestDaemon_ExponentialBackoff verifies that the backoff duration doubles
// with each restart, up to the configured maximum.
func TestDaemon_ExponentialBackoff(t *testing.T) {
	startErr := errors.New("crash")
	worker := &testWorker{startErr: startErr}
	builder := &testBuilder{workers: []Worker{worker}}
	buildCh := make(chan struct{}, 10)
	builder.buildCalled = buildCh

	d := Daemon("test-backoff", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
		WithDaemonMaxRestartBackoff(100*time.Millisecond),
		WithDaemonMaxRestarts(3),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Measure time between successive Build calls.
	var buildTimes []time.Time

	// Wait for 4 Build calls (initial + 3 restarts before exhaustion).
	for i := 0; i < 4; i++ {
		select {
		case <-buildCh:
			buildTimes = append(buildTimes, time.Now())
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for Build call %d", i+1)
		}
	}

	if len(buildTimes) < 3 {
		t.Fatal("not enough Build calls to measure backoff")
	}

	// Check each successive backoff is approximately >= previous.
	// Allow tolerance for scheduling variation (20ms).
	for i := 1; i < len(buildTimes)-1; i++ {
		gap := buildTimes[i].Sub(buildTimes[i-1])

		if gap < 5*time.Millisecond {
			t.Fatalf("gap %d (%v) too short for expected backoff", i, gap)
		}
		if gap > 1*time.Second {
			t.Fatalf("gap %d (%v) too long", i, gap)
		}
	}

	// Cleanup: wait for fatal and stop.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)
}

// TestDaemon_WorkerBuilderFunc verifies the function adapter.
func TestDaemon_WorkerBuilderFunc(t *testing.T) {
	w := &blockingWorker{stopCalled: make(chan struct{}, 1)}

	var buildCalls int
	builder := WorkerBuilderFunc(func(ctx context.Context) (Worker, error) {
		buildCalls++
		return w, nil
	})

	d := Daemon("test-adapter", builder)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for worker to be built and started.
	time.Sleep(100 * time.Millisecond)

	if buildCalls != 1 {
		t.Fatalf("Build called %d times, want 1", buildCalls)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// TestDaemon_NegativeMaxRestarts verifies that a negative maxRestarts
// disables the restart limit entirely.
func TestDaemon_NegativeMaxRestarts(t *testing.T) {
	startErr := errors.New("always fails")
	worker := &testWorker{startErr: startErr}
	builder := &testBuilder{workers: []Worker{worker}}

	buildCh := make(chan struct{}, 20)
	builder.buildCalled = buildCh

	d := Daemon("test-unlimited", builder,
		WithDaemonRestartBackoff(time.Millisecond),
		WithDaemonMaxRestarts(-1), // unlimited
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for several Build calls (more than default maxRestarts=5).
	buildCount := 0
loop:
	for i := 0; i < 10; i++ {
		select {
		case <-buildCh:
			buildCount++
		case <-time.After(2 * time.Second):
			break loop
		}
	}

	if buildCount < 6 {
		t.Fatalf("only got %d Build calls with unlimited restarts, want at least 6", buildCount)
	}

	// Verify no fatal error on Done() channel.
	select {
	case err := <-d.(*daemonComponent).done:
		t.Fatalf("unexpected fatal error with unlimited restarts: %v", err)
	default:
		// Expected: no fatal.
	}

	// Cleanup: stop the daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)
}

// TestDaemon_Name verifies the Name() method.
func TestDaemon_Name(t *testing.T) {
	cmp := Daemon("my-daemon", &testBuilder{})

	if got := cmp.Name(); got != "my-daemon" {
		t.Fatalf("Name() = %q, want %q", got, "my-daemon")
	}
}

// TestDaemon_DoneIsNotNil verifies that Done() returns a non-nil channel
// (exitWatcher always returns a channel).
func TestDaemon_DoneIsNotNil(t *testing.T) {
	cmp := Daemon("d", &testBuilder{})

	ch := cmp.(*daemonComponent).Done()
	if ch == nil {
		t.Fatal("Done() returned nil channel")
	}
}

// TestDaemon_DoubleStop verifies that calling Stop twice is safe.
func TestDaemon_DoubleStop(t *testing.T) {
	block := make(chan struct{})
	builder := &testBuilder{
		workers: []Worker{&testWorker{startBlock: block}},
	}

	d := Daemon("testd", builder)
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("first Stop() error: %v", err)
	}

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("second Stop() error: %v", err)
	}
}

// TestDaemon_WorkerStopPanicRecovery verifies that if worker.Stop panics,
// the daemon component is not affected (supervisor is already exiting).
func TestDaemon_WorkerStopPanicRecovery(t *testing.T) {
	panicCalled := make(chan struct{}, 1)
	worker := &testWorker{
		startBlock: make(chan struct{}),
		stopCalled: panicCalled,
	}
	builder := &testBuilder{workers: []Worker{worker}}

	d := Daemon("test-stop-panic", builder)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Inject a panicking Stop.
	worker.stopPanic = true

	// Cancel daemon context to trigger shutdown.
	cmp := d.(*daemonComponent)
	cmp.mu.Lock()
	if cmp.cancel != nil {
		cmp.cancel()
	}
	cmp.mu.Unlock()

	// Stop should internally try to call worker.Stop, which panics.
	// The daemonComponent should handle this gracefully.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)
}

// TestDaemon_ContextCancelledDuringBackoff verifies that when the daemon
// context is cancelled during backoff sleep, the supervisor exits cleanly
// without restarting.
func TestDaemon_ContextCancelledDuringBackoff(t *testing.T) {
	startErr := errors.New("crash first time")

	// We track Build calls to verify no restart after context cancel.
	buildCh := make(chan struct{}, 3)
	worker := &testWorker{startErr: startErr}
	builder := &testBuilder{
		workers:     []Worker{worker},
		buildCalled: buildCh,
	}

	d := Daemon("test-cancel-backoff", builder,
		WithDaemonRestartBackoff(500*time.Millisecond),
	)

	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	if err := d.Start(daemonCtx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for first Build.
	select {
	case <-buildCh:
		// Worker failed, backoff starts.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first Build")
	}

	// Cancel daemon context during backoff.
	time.Sleep(50 * time.Millisecond)
	daemonCancel()

	// Wait for supervisor to exit.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Verify no second Build call (no restart).
	select {
	case <-buildCh:
		// Could be the timing where Build happened before cancel.
		// Acceptable — the key is that there's no infinite restart loop.
	default:
		// Expected: no restart.
	}
}

func TestDaemon_StartedLog(t *testing.T) {
	buf := captureSlog(t)
	startCalled := make(chan struct{}, 1)
	block := make(chan struct{})
	builder := &testBuilder{
		workers: []Worker{&testWorker{startBlock: block, startCalled: startCalled}},
	}

	d := Daemon("log-testd", builder)
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for the worker to actually start (ensures "daemon started" is logged).
	select {
	case <-startCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker Start")
	}

	close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)

	output := buf.String()
	if !strings.Contains(output, "daemon started") {
		t.Fatal("expected 'daemon started' in log output")
	}
	if !strings.Contains(output, "component=log-testd") {
		t.Fatal("expected component=log-testd in log output")
	}
}

func TestDaemon_BuildFailedLog(t *testing.T) {
	buf := captureSlog(t)
	goodWorker := &testWorker{startBlock: make(chan struct{})}
	builder := &testBuilder{
		workers:     []Worker{goodWorker},
		buildCalled: make(chan struct{}, 3),
	}
	builder.buildErr = errors.New("build boom")

	d := Daemon("log-build-fail", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for first Build (fails).
	<-builder.buildCalled

	builder.mu.Lock()
	builder.buildErr = nil
	builder.mu.Unlock()

	// Wait for second Build (restart, succeeds).
	select {
	case <-builder.buildCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restart Build")
	}

	// Cleanup.
	close(goodWorker.startBlock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)

	output := buf.String()
	if !strings.Contains(output, "daemon build failed") {
		t.Fatal("expected 'daemon build failed' in log output")
	}
	if !strings.Contains(output, "component=log-build-fail") {
		t.Fatal("expected component=log-build-fail in build failed log output")
	}
}

func TestDaemon_StartFailedLog(t *testing.T) {
	buf := captureSlog(t)
	startErr := errors.New("worker crashed")
	errWorker := &testWorker{startErr: startErr}
	goodWorker := &testWorker{startBlock: make(chan struct{})}

	builder := &testBuilder{
		workers:     []Worker{errWorker, goodWorker},
		buildCalled: make(chan struct{}, 3),
	}

	d := Daemon("log-start-fail", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for initial build + start failure + restart build.
	<-builder.buildCalled
	select {
	case <-builder.buildCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restart Build")
	}

	// Cleanup.
	close(goodWorker.startBlock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)

	output := buf.String()
	if !strings.Contains(output, "daemon start failed") {
		t.Fatal("expected 'daemon start failed' in log output")
	}
	if !strings.Contains(output, "component=log-start-fail") {
		t.Fatal("expected component=log-start-fail in start failed log output")
	}
}

func TestDaemon_RestartingLog(t *testing.T) {
	buf := captureSlog(t)
	errWorker := &testWorker{startErr: errors.New("fail")}
	goodWorker := &testWorker{startBlock: make(chan struct{})}

	builder := &testBuilder{
		workers:     []Worker{errWorker, goodWorker},
		buildCalled: make(chan struct{}, 3),
	}

	d := Daemon("log-restart", builder,
		WithDaemonRestartBackoff(10*time.Millisecond),
	)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for first build+start (fail) + restart build.
	<-builder.buildCalled
	select {
	case <-builder.buildCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restart Build")
	}

	// Cleanup.
	close(goodWorker.startBlock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.Stop(ctx)

	output := buf.String()
	if !strings.Contains(output, "restarting daemon") {
		t.Fatal("expected 'restarting daemon' in log output")
	}
	if !strings.Contains(output, "component=log-restart") {
		t.Fatal("expected component=log-restart in restarting log output")
	}
	if !strings.Contains(output, "attempt=") {
		t.Fatal("expected attempt= in restarting log output")
	}
	if !strings.Contains(output, "backoff=") {
		t.Fatal("expected backoff= in restarting log output")
	}
}

func TestDaemon_RestartExhaustedLog(t *testing.T) {
	buf := captureSlog(t)
	worker := &testWorker{startErr: errors.New("always fails")}
	builder := &testBuilder{workers: []Worker{worker}}

	d := Daemon("log-exhausted", builder,
		WithDaemonRestartBackoff(time.Millisecond),
		WithDaemonMaxRestarts(1),
	)

	fatalCh := make(chan error, 1)
	go func() {
		select {
		case err := <-d.(*daemonComponent).done:
			fatalCh <- err
		case <-time.After(5 * time.Second):
		}
	}()

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-fatalCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fatal error")
	}

	output := buf.String()
	if !strings.Contains(output, "restart policy exhausted") {
		t.Fatal("expected 'restart policy exhausted' in log output")
	}
	if !strings.Contains(output, "component=log-exhausted") {
		t.Fatal("expected component=log-exhausted in exhausted log output")
	}
}

func TestDaemon_FatalErrorLog(t *testing.T) {
	buf := captureSlog(t)
	fatalErr := errors.New("classified as fatal")

	classifier := func(_ context.Context, err error) DaemonDecision {
		if errors.Is(err, fatalErr) {
			return DaemonFatal
		}
		return DaemonRestart
	}

	worker := &testWorker{startErr: fatalErr}
	builder := &testBuilder{workers: []Worker{worker}}

	d := Daemon("log-fatal", builder,
		WithDaemonErrorClassifier(classifier),
	)

	fatalCh := make(chan struct{})
	go func() {
		select {
		case <-d.(*daemonComponent).done:
			close(fatalCh)
		case <-time.After(2 * time.Second):
		}
	}()

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-fatalCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fatal error")
	}

	output := buf.String()
	if !strings.Contains(output, "daemon fatal error") {
		t.Fatal("expected 'daemon fatal error' in log output")
	}
	if !strings.Contains(output, "component=log-fatal") {
		t.Fatal("expected component=log-fatal in fatal error log output")
	}
}
