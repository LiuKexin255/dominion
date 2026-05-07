package logs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"dominion/common/gopkg/otel"
)

// captureStdout returns a function that redirects os.Stdout to a pipe and
// provides a reader.  Call the returned stop function to restore os.Stdout
// and read all captured output.
func captureStdout(t testing.TB) (stop func() string) {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	return func() string {
		w.Close()
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("read pipe: %v", err)
		}
		os.Stdout = old
		return buf.String()
	}
}

// resetInit resets the package-level init state so that each test can trigger
// a fresh lazy initialisation.
func resetInit() {
	initOnce = new(sync.Once)
	defaultLogger = nil
}

func TestDefault_NonDeploy(t *testing.T) {
	resetInit()

	// Simulate non-deploy: no LoggerProviderSet.
	t.Cleanup(func() { otel.LoggerProviderSet = false })
	otel.LoggerProviderSet = false

	stop := captureStdout(t)
	logger := Default()

	if logger == nil {
		stop()
		t.Fatal("Default() returned nil")
	}

	logger.Info("hello")
	output := stop()

	// In non-deploy mode we use a TextHandler; output should contain the
	// message key ("msg=") rather than OTLP JSON.
	if !strings.Contains(output, "msg=") {
		t.Errorf("expected text handler output containing 'msg=', got: %s", output)
	}
}

func TestDefault_Deploy(t *testing.T) {
	resetInit()

	oldDeploy := newDeployHandler
	t.Cleanup(func() {
		newDeployHandler = oldDeploy
		otel.LoggerProviderSet = false
	})
	otel.LoggerProviderSet = true

	var deployCalled bool
	var buf bytes.Buffer
	newDeployHandler = func(name string) slog.Handler {
		deployCalled = true
		if name != "dominion/common/gopkg/logs" {
			t.Errorf("newDeployHandler name = %q, want 'dominion/common/gopkg/logs'", name)
		}
		return slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	logger := Default()
	if logger == nil {
		t.Fatal("Default() returned nil")
	}

	if !deployCalled {
		t.Error("deploy handler was not called")
	}

	logger.Info("deploy-test")
	output := buf.String()
	if !strings.Contains(output, "msg=") {
		t.Errorf("expected output containing 'msg=', got: %s", output)
	}
	if !strings.Contains(output, "deploy-test") {
		t.Errorf("expected output containing 'deploy-test', got: %s", output)
	}
}

func TestFromContext_NoLogger(t *testing.T) {
	ctx := context.Background()
	logger := FromContext(ctx)
	if logger == nil {
		t.Fatal("FromContext(empty ctx) returned nil")
	}
	// Should be the same instance as Default().
	if logger != Default() {
		t.Error("FromContext(empty ctx) should return Default()")
	}
}

func TestFromContext_WithLogger(t *testing.T) {
	custom := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx := context.WithValue(context.Background(), loggerKey{}, custom)

	logger := FromContext(ctx)
	if logger != custom {
		t.Error("FromContext should return the logger stored in context")
	}
}

func TestInfoContext(t *testing.T) {
	resetInit()

	t.Cleanup(func() { otel.LoggerProviderSet = false })
	otel.LoggerProviderSet = false

	stop := captureStdout(t)
	defer stop() // Ensure stdout is restored even on failure.

	InfoContext(context.Background(), "info-message", "key", "value")

	output := stop()
	if !strings.Contains(output, "msg=") {
		t.Errorf("expected text output containing 'msg=', got: %s", output)
	}
	if !strings.Contains(output, "info-message") {
		t.Errorf("expected output containing 'info-message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected output containing 'key=value', got: %s", output)
	}
}

func TestErrorContext(t *testing.T) {
	resetInit()

	t.Cleanup(func() { otel.LoggerProviderSet = false })
	otel.LoggerProviderSet = false

	stop := captureStdout(t)
	defer stop()

	err := errors.New("something went wrong")
	ErrorContext(context.Background(), "failed", "error", err, "user", "alice")

	output := stop()
	if !strings.Contains(output, "msg=") {
		t.Errorf("expected text output containing 'msg=', got: %s", output)
	}
	if !strings.Contains(output, "failed") {
		t.Errorf("expected output containing 'failed', got: %s", output)
	}
	if !strings.Contains(output, "error=") {
		t.Errorf("expected output containing 'error=', got: %s", output)
	}
	if !strings.Contains(output, "something went wrong") {
		t.Errorf("expected output containing 'something went wrong', got: %s", output)
	}
	if !strings.Contains(output, "user=alice") {
		t.Errorf("expected output containing 'user=alice', got: %s", output)
	}
}

func TestWith(t *testing.T) {
	resetInit()

	t.Cleanup(func() { otel.LoggerProviderSet = false })
	otel.LoggerProviderSet = false

	stop := captureStdout(t)
	defer stop()

	ctx := With(context.Background(), "request_id", "abc123")
	logger := FromContext(ctx)
	logger.Info("with-test")

	output := stop()
	if !strings.Contains(output, "request_id=abc123") {
		t.Errorf("expected output containing 'request_id=abc123', got: %s", output)
	}
}

func TestWith_Multiple(t *testing.T) {
	resetInit()

	t.Cleanup(func() { otel.LoggerProviderSet = false })
	otel.LoggerProviderSet = false

	stop := captureStdout(t)
	defer stop()

	ctx := With(context.Background(), "first", "1")
	ctx = With(ctx, "second", "2")
	logger := FromContext(ctx)
	logger.Info("accumulated")

	output := stop()
	if !strings.Contains(output, "first=1") {
		t.Errorf("expected output containing 'first=1', got: %s", output)
	}
	if !strings.Contains(output, "second=2") {
		t.Errorf("expected output containing 'second=2', got: %s", output)
	}
}

func TestLazyInit(t *testing.T) {
	resetInit()

	t.Cleanup(func() { otel.LoggerProviderSet = false })
	otel.LoggerProviderSet = false

	// Call Default() multiple times — should return the same logger.
	l1 := Default()
	l2 := Default()
	l3 := Default()

	if l1 != l2 {
		t.Error("Default() returned different loggers across calls")
	}
	if l1 != l3 {
		t.Error("Default() returned different loggers across calls")
	}
}
