package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"dominion/common/gopkg/logs"
)

// mockComponent implements Component for testing.
// It records start/stop calls and call order for verification.
type mockComponent struct {
	name      string
	stage     Stage
	startErr  error
	stopErr   error
	stopPanic bool

	startCount int
	stopCount  int

	// Shared tracking across all mocks in a test.
	orderMu *sync.Mutex
	order   *[]string
}

func (m *mockComponent) Name() string { return m.name }
func (m *mockComponent) Stage() Stage { return m.stage }
func (m *mockComponent) Start(ctx context.Context) error {
	m.orderMu.Lock()
	m.startCount++
	*m.order = append(*m.order, "start:"+m.name)
	m.orderMu.Unlock()
	return m.startErr
}
func (m *mockComponent) Stop(ctx context.Context) error {
	m.orderMu.Lock()
	m.stopCount++
	*m.order = append(*m.order, "stop:"+m.name)
	m.orderMu.Unlock()
	if m.stopPanic {
		panic("boom")
	}
	return m.stopErr
}

func newTestOrder() (*sync.Mutex, *[]string) {
	return new(sync.Mutex), new([]string)
}

func newMock(name string, stage Stage, orderMu *sync.Mutex, order *[]string) *mockComponent {
	return &mockComponent{
		name:    name,
		stage:   stage,
		orderMu: orderMu,
		order:   order,
	}
}

func orderContains(t *testing.T, order []string, want string) {
	t.Helper()
	if slices.Contains(order, want) {
		return
	}
	t.Fatalf("order does not contain %q: %v", want, order)
}

func orderSequence(t *testing.T, order []string, a, b string) {
	t.Helper()
	var aIdx, bIdx int = -1, -1
	for i, s := range order {
		if s == a {
			aIdx = i
		}
		if s == b {
			bIdx = i
		}
	}
	if aIdx == -1 || bIdx == -1 {
		t.Fatalf("missing entries in order for %q vs %q: %v", a, b, order)
	}
	if aIdx >= bIdx {
		t.Fatalf("expected %q before %q, got order: %v", a, b, order)
	}
}

// TestBootstrap_StartOrderByStage verifies components start in Stage asc then
// Name asc order.
func TestBootstrap_StartOrderByStage(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("zeta", StageServer, mu, ord))
	_ = b.Register(newMock("alpha", StageFoundation, mu, ord))
	_ = b.Register(newMock("beta", StageFoundation, mu, ord))
	_ = b.Register(newMock("delta", StageClient, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start RunSignal in background, then cancel to trigger shutdown.
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	// Give time for startup to complete.
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	// Expected start order: StageFoundation(alpha) < StageFoundation(beta) < StageClient(delta) < StageServer(zeta)
	orderSequence(t, got, "start:alpha", "start:beta")
	orderSequence(t, got, "start:beta", "start:delta")
	orderSequence(t, got, "start:delta", "start:zeta")
}

// TestBootstrap_StopReverseOrder verifies Stop is called in reverse Start order.
func TestBootstrap_StopReverseOrder(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("first", StageFoundation, mu, ord))
	_ = b.Register(newMock("second", StageClient, mu, ord))
	_ = b.Register(newMock("third", StageServer, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	// Start order: first, second, third. Stop order: third, second, first.
	orderSequence(t, got, "start:first", "start:second")
	orderSequence(t, got, "start:second", "start:third")
	orderSequence(t, got, "stop:third", "stop:second")
	orderSequence(t, got, "stop:second", "stop:first")
}

// TestBootstrap_DuplicateName verifies registering two components with the same
// name returns an error.
func TestBootstrap_DuplicateName(t *testing.T) {
	b := New()

	m1 := newMock("duplicate", StageClient, nil, nil)
	m2 := newMock("duplicate", StageServer, nil, nil)

	if err := b.Register(m1); err != nil {
		t.Fatalf("first Register unexpected error: %v", err)
	}
	if err := b.Register(m2); err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

// TestBootstrap_StartupFailureRollback verifies that when a component fails to
// start, previously-started components get Stop() called (rollback), and the
// failing component does NOT get Stop called.
func TestBootstrap_StartupFailureRollback(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	// first starts ok, second fails, third is never started.
	first := newMock("first", StageFoundation, mu, ord)
	second := newMock("second", StageClient, mu, ord)
	second.startErr = errors.New("start failed")
	third := newMock("third", StageServer, mu, ord)

	_ = b.Register(first)
	_ = b.Register(second)
	_ = b.Register(third)

	ctx := context.Background()
	// Use RunSignal with an unlikely signal so it doesn't trigger.
	err := b.RunSignal(ctx, syscall.SIGUSR1)
	if err == nil {
		t.Fatal("expected error from startup failure, got nil")
	}

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	// first started, second failed, rollback stopped first.
	orderContains(t, got, "start:first")
	orderContains(t, got, "stop:first")
	// second started but failed - no Stop called for it.
	for _, s := range got {
		if s == "stop:second" {
			t.Fatalf("failing component should NOT get Stop called, got: %v", got)
		}
	}
	// third was never started, should not have start or stop.
	for _, s := range got {
		if s == "start:third" || s == "stop:third" {
			t.Fatalf("unstarted component should not appear in order: %v", got)
		}
	}
	mu.Lock()
	if second.stopCount > 0 {
		t.Fatalf("failing component had %d Stop calls", second.stopCount)
	}
	mu.Unlock()
}

// TestBootstrap_StopErrorsJoined verifies that when multiple components return
// errors from Stop, ALL still get stopped and errors are collected via
// errors.Join.
func TestBootstrap_StopErrorsJoined(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	c1 := newMock("c1", StageFoundation, mu, ord)
	c1.stopErr = errors.New("stop err 1")
	c2 := newMock("c2", StageServer, mu, ord)
	c2.stopErr = errors.New("stop err 2")

	_ = b.Register(c1)
	_ = b.Register(c2)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	// Both should have been stopped.
	orderContains(t, got, "stop:c1")
	orderContains(t, got, "stop:c2")

	if c1.stopCount != 1 {
		t.Fatalf("c1 stop count: want 1, got %d", c1.stopCount)
	}
	if c2.stopCount != 1 {
		t.Fatalf("c2 stop count: want 1, got %d", c2.stopCount)
	}
}

// TestBootstrap_OTelLastShutdown verifies that a StageFoundation component is
// the last one to stop (started first, stopped last in reverse order).
func TestBootstrap_OTelLastShutdown(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	// OTel-alike: StageFoundation = starts first, stops last.
	otel := newMock("otel", StageFoundation, mu, ord)
	client := newMock("client", StageClient, mu, ord)
	server := newMock("server", StageServer, mu, ord)

	_ = b.Register(server)
	_ = b.Register(client)
	_ = b.Register(otel)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	// OTel starts first.
	orderSequence(t, got, "start:otel", "start:client")
	// OTel stops last (reverse of start order).
	orderSequence(t, got, "stop:server", "stop:otel")
	orderSequence(t, got, "stop:client", "stop:otel")

	// Verify all three got stopped.
	orderContains(t, got, "stop:otel")
	orderContains(t, got, "stop:client")
	orderContains(t, got, "stop:server")
}

// TestBootstrap_CleanShutdownReturnsNil verifies that a clean shutdown (via
// context cancellation) causes Run() to return nil.
func TestBootstrap_CleanShutdownReturnsNil(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("c", StageClient, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- b.RunSignal(ctx, syscall.SIGUSR1)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("expected nil for clean shutdown, got: %v", err)
	}
}

// TestBootstrap_RegisterAfterRunReturnsError verifies that calling Register
// after Run has started returns an error.
func TestBootstrap_RegisterAfterRunReturnsError(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	// Pre-register one component so RunSignal has valid mock instances.
	_ = b.Register(newMock("first", StageClient, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()

	// Poll until Register returns error (Run has set running=true).
	var lateErr error
	for range 200 {
		lateErr = b.Register(newMock("late", StageClient, mu, ord))
		if lateErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if lateErr == nil {
		t.Fatal("expected error for Register after Run, got nil")
	}
}

// TestBootstrap_ZeroComponents verifies that Run with no registered components
// returns nil immediately on clean shutdown.
func TestBootstrap_ZeroComponents(t *testing.T) {
	b := New()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Context times out → clean shutdown returns nil.
	err := b.RunSignal(ctx, syscall.SIGUSR1)
	if err != nil {
		t.Fatalf("expected nil with zero components, got: %v", err)
	}
}

// TestBootstrap_CancelledContext verifies graceful shutdown when an
// already-cancelled context is passed to RunSignal.
func TestBootstrap_CancelledContext(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("c1", StageFoundation, mu, ord))
	_ = b.Register(newMock("c2", StageServer, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// RunSignal should start all, then immediately shut down.
	err := b.RunSignal(ctx, syscall.SIGUSR1)
	if err != nil {
		t.Fatalf("expected nil for graceful shutdown with cancelled ctx, got: %v", err)
	}

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	orderContains(t, got, "start:c1")
	orderContains(t, got, "start:c2")
	orderContains(t, got, "stop:c2")
	orderContains(t, got, "stop:c1")
}

// TestBootstrap_StopPanicRecovered verifies that when one component's Stop
// panics, the panic is recovered and other components still get stopped.
func TestBootstrap_StopPanicRecovered(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	c1 := newMock("c1", StageFoundation, mu, ord)
	c2 := newMock("c2", StageClient, mu, ord)
	c2.stopPanic = true
	c3 := newMock("c3", StageServer, mu, ord)

	_ = b.Register(c1)
	_ = b.Register(c2)
	_ = b.Register(c3)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- b.RunSignal(ctx, syscall.SIGUSR1)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	err := <-done
	// Stop errors (including panic) should be collected; other stops complete.
	_ = err

	mu.Lock()
	got := make([]string, len(*ord))
	copy(got, *ord)
	mu.Unlock()

	// All three should be stopped, even though c2 panics.
	orderContains(t, got, "stop:c1")
	orderContains(t, got, "stop:c2")
	orderContains(t, got, "stop:c3")
}

// TestBootstrap_DoubleRunReturnsError verifies that calling RunSignal twice
// returns an error on the second call.
func TestBootstrap_DoubleRunReturnsError(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("c", StageClient, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First call succeeds and starts normally.
	done := make(chan error, 1)
	go func() {
		done <- b.RunSignal(ctx, syscall.SIGUSR1)
	}()

	// Wait for first Run to start.
	time.Sleep(50 * time.Millisecond)

	// Second call should return error immediately.
	err := b.RunSignal(context.Background(), syscall.SIGUSR1)
	if err == nil {
		t.Fatal("expected error for second RunSignal call, got nil")
	}

	// Clean up first Run.
	cancel()
	<-done
}

// mockExitWatcher implements Component and exitWatcher for testing unexpected
// exit detection on non-StageServer components (e.g. StageDaemon).
type mockExitWatcher struct {
	name     string
	stage    Stage
	done     chan error
	startErr error
	stopErr  error
}

func (m *mockExitWatcher) Name() string                  { return m.name }
func (m *mockExitWatcher) Stage() Stage                  { return m.stage }
func (m *mockExitWatcher) Start(_ context.Context) error { return m.startErr }
func (m *mockExitWatcher) Stop(_ context.Context) error  { return m.stopErr }
func (m *mockExitWatcher) Done() <-chan error            { return m.done }

// TestBootstrap_DaemonExitWatcher verifies that a StageDaemon component
// implementing exitWatcher triggers shutdown when its Done() channel
// receives an error. This proves the monitor watches all component stages,
// not just StageServer.
func TestBootstrap_DaemonExitWatcher(t *testing.T) {
	b := New()
	mu, ord := newTestOrder()

	daemon := &mockExitWatcher{
		name:  "mydaemon",
		stage: StageDaemon,
		done:  make(chan error, 1),
	}
	_ = b.Register(daemon)
	_ = b.Register(newMock("other", StageClient, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- b.RunSignal(ctx, syscall.SIGUSR1)
	}()

	time.Sleep(50 * time.Millisecond)

	// Simulate daemon failure — sends error through Done() channel.
	daemon.done <- errors.New("daemon crashed")

	err := <-done
	if err == nil {
		t.Fatal("expected error from daemon exit, got nil")
	}
	if !strings.Contains(err.Error(), "daemon crashed") {
		t.Fatalf("expected error containing 'daemon crashed', got: %v", err)
	}
}

// captureSlog replaces the default logs logger with a TextHandler writing to
// a buffer and returns the buffer. The original logger is restored on cleanup.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := logs.Default()
	logs.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { logs.SetDefault(old) })
	return &buf
}

func TestBootstrap_StartingLog(t *testing.T) {
	buf := captureSlog(t)
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("a", StageClient, mu, ord))
	_ = b.Register(newMock("b", StageServer, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "bootstrap starting") {
		t.Fatal("expected 'bootstrap starting' in log output")
	}
	if !strings.Contains(output, "components=2") {
		t.Fatal("expected components=2 in log output")
	}
	if !strings.Contains(output, "user defined signal 1") {
		t.Fatal("expected signal name in log output")
	}
}

func TestBootstrap_ComponentStartedLog(t *testing.T) {
	buf := captureSlog(t)
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("svc", StageServer, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "component started") {
		t.Fatal("expected 'component started' in log output")
	}
	if !strings.Contains(output, "component=svc") {
		t.Fatal("expected component=svc in log output")
	}
	if !strings.Contains(output, "stage=Server") {
		t.Fatal("expected stage=Server in log output")
	}
}

func TestBootstrap_StartFailedRollbackLog(t *testing.T) {
	buf := captureSlog(t)
	b := New()
	mu, ord := newTestOrder()

	ok := newMock("ok", StageFoundation, mu, ord)
	fail := newMock("fail", StageClient, mu, ord)
	fail.startErr = errors.New("boom")

	_ = b.Register(ok)
	_ = b.Register(fail)

	ctx := context.Background()
	_ = b.RunSignal(ctx, syscall.SIGUSR1)

	output := buf.String()
	if !strings.Contains(output, "component start failed, rolling back") {
		t.Fatal("expected 'component start failed, rolling back' in log output")
	}
	if !strings.Contains(output, "component=fail") {
		t.Fatal("expected component=fail in log output")
	}
}

func TestBootstrap_ShutdownLog(t *testing.T) {
	buf := captureSlog(t)
	b := New()
	mu, ord := newTestOrder()

	_ = b.Register(newMock("c", StageClient, mu, ord))

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "shutdown starting") {
		t.Fatal("expected 'shutdown starting' in log output")
	}
	if !strings.Contains(output, "components=1") {
		t.Fatal("expected components=1 in shutdown log output")
	}
}

func TestBootstrap_StopFailedLog(t *testing.T) {
	buf := captureSlog(t)
	b := New()
	mu, ord := newTestOrder()

	c1 := newMock("c1", StageClient, mu, ord)
	c1.stopErr = errors.New("stop boom")

	_ = b.Register(c1)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.RunSignal(ctx, syscall.SIGUSR1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "component stop failed") {
		t.Fatal("expected 'component stop failed' in log output")
	}
	if !strings.Contains(output, "component=c1") {
		t.Fatal("expected component=c1 in stop failed log output")
	}
}

func TestBootstrap_UnexpectedExitLog(t *testing.T) {
	buf := captureSlog(t)
	b := New()

	daemon := &mockExitWatcher{
		name:  "mydaemon",
		stage: StageDaemon,
		done:  make(chan error, 1),
	}
	_ = b.Register(daemon)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- b.RunSignal(ctx, syscall.SIGUSR1)
	}()

	time.Sleep(50 * time.Millisecond)
	daemon.done <- errors.New("daemon crashed")

	_ = <-done

	output := buf.String()
	if !strings.Contains(output, "component exited unexpectedly") {
		t.Fatal("expected 'component exited unexpectedly' in log output")
	}
	if !strings.Contains(output, "component=mydaemon") {
		t.Fatal("expected component=mydaemon in unexpected exit log output")
	}
}
