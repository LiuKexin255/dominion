package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dominion/common/gopkg/otel"
)

// stubOtelInit replaces otelInit for the duration of the test and restores it on cleanup.
func stubOtelInit(t *testing.T, fn func(ctx context.Context, opts ...otel.Option) (otel.Shutdown, error)) {
	t.Helper()
	original := otelInit
	otelInit = fn
	t.Cleanup(func() { otelInit = original })
}

func TestOTel_StartCallsInit(t *testing.T) {
	initCalled := false
	stubOtelInit(t, func(_ context.Context, _ ...otel.Option) (otel.Shutdown, error) {
		initCalled = true
		return func(_ context.Context) error { return nil }, nil
	})

	c := OTel()
	err := c.Start(context.Background())

	if !initCalled {
		t.Fatal("otelInit was not called")
	}
	if err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
	// Verify shutdown was saved by calling Stop.
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
}

func TestOTel_StopCallsShutdown(t *testing.T) {
	shutdownCalled := false
	stubOtelInit(t, func(_ context.Context, _ ...otel.Option) (otel.Shutdown, error) {
		return func(_ context.Context) error {
			shutdownCalled = true
			return nil
		}, nil
	})

	c := OTel()
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

func TestOTel_StageIsFoundation(t *testing.T) {
	c := OTel()

	if got := c.Stage(); got != StageFoundation {
		t.Fatalf("Stage() = %v, want %v", got, StageFoundation)
	}
}

func TestOTel_StartFailureReturnsError(t *testing.T) {
	wantErr := errors.New("init failed")
	stubOtelInit(t, func(_ context.Context, _ ...otel.Option) (otel.Shutdown, error) {
		return nil, wantErr
	})

	c := OTel()
	err := c.Start(context.Background())

	if err == nil {
		t.Fatal("Start expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Start error = %v, want %v", err, wantErr)
	}
	// Stop on failed start should return nil (shutdown is nil).
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after failed Start returned unexpected error: %v", err)
	}
}

func TestOTel_StartSuccessLog(t *testing.T) {
	buf := captureSlog(t)
	stubOtelInit(t, func(_ context.Context, _ ...otel.Option) (otel.Shutdown, error) {
		return func(_ context.Context) error { return nil }, nil
	})

	c := OTel()
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

func TestOTel_StartFailureLog(t *testing.T) {
	buf := captureSlog(t)
	stubOtelInit(t, func(_ context.Context, _ ...otel.Option) (otel.Shutdown, error) {
		return nil, errors.New("init failed")
	})

	c := OTel()
	_ = c.Start(context.Background())

	output := buf.String()
	if !strings.Contains(output, "otel start failed") {
		t.Fatal("expected 'otel start failed' in log output")
	}
	if !strings.Contains(output, "component=otel") {
		t.Fatal("expected component=otel in log output")
	}
}
