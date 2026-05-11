package otel

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"dominion/common/gopkg/bootstrap"
)

// stubInit replaces initFn for the duration of the test and restores it on cleanup.
func stubInit(t *testing.T, fn func(ctx context.Context, opts ...Option) (Shutdown, error)) {
	t.Helper()
	original := initFn
	initFn = fn
	t.Cleanup(func() { initFn = original })
}

// captureSlog redirects the default slog output to a buffer for the duration
// of the test and restores it on cleanup.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestComponent_StartCallsInit(t *testing.T) {
	initCalled := false
	stubInit(t, func(_ context.Context, _ ...Option) (Shutdown, error) {
		initCalled = true
		return func(_ context.Context) error { return nil }, nil
	})

	c := Component()
	err := c.Start(context.Background())

	if !initCalled {
		t.Fatal("initFn was not called")
	}
	if err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
}

func TestComponent_StopCallsShutdown(t *testing.T) {
	shutdownCalled := false
	stubInit(t, func(_ context.Context, _ ...Option) (Shutdown, error) {
		return func(_ context.Context) error {
			shutdownCalled = true
			return nil
		}, nil
	})

	c := Component()
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
	if !shutdownCalled {
		t.Fatal("shutdown function was not called")
	}
}

func TestComponent_StageIsFoundation(t *testing.T) {
	c := Component()

	if got := c.Stage(); got != bootstrap.StageFoundation {
		t.Fatalf("Stage() = %v, want %v", got, bootstrap.StageFoundation)
	}
}

func TestComponent_StartFailureReturnsError(t *testing.T) {
	wantErr := errors.New("init failed")
	stubInit(t, func(_ context.Context, _ ...Option) (Shutdown, error) {
		return nil, wantErr
	})

	c := Component()
	err := c.Start(context.Background())

	if err == nil {
		t.Fatal("Start expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Start error = %v, want %v", err, wantErr)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after failed Start returned unexpected error: %v", err)
	}
}

func TestComponent_StartSuccessLog(t *testing.T) {
	buf := captureSlog(t)
	stubInit(t, func(_ context.Context, _ ...Option) (Shutdown, error) {
		return func(_ context.Context) error { return nil }, nil
	})

	c := Component()
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "otel started") {
		t.Fatal("expected 'otel started' in log output")
	}
	if !strings.Contains(output, "component=otel") {
		t.Fatal("expected component=otel in log output")
	}
}

func TestComponent_StartFailureLog(t *testing.T) {
	buf := captureSlog(t)
	stubInit(t, func(_ context.Context, _ ...Option) (Shutdown, error) {
		return nil, errors.New("init failed")
	})

	c := Component()
	_ = c.Start(context.Background())

	output := buf.String()
	if !strings.Contains(output, "otel start failed") {
		t.Fatal("expected 'otel start failed' in log output")
	}
	if !strings.Contains(output, "component=otel") {
		t.Fatal("expected component=otel in log output")
	}
}
