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

	"dominion/common/gopkg/logs/event"
)

// captureStdout returns a function that redirects os.Stdout to a pipe and
// provides a reader. Call the returned stop function to restore os.Stdout
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

// resetReporter clears the active reporter for a clean test state.
func resetReporter() {
	reporterMu.Lock()
	activeReporter = nil
	reporterMu.Unlock()
}

func TestDefault(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	logger := Default()
	if logger == nil {
		t.Fatal("Default() returned nil")
	}

	logger.Info("hello")
	output := stop()

	if !strings.Contains(output, "msg=") {
		t.Errorf("expected text handler output containing 'msg=', got: %s", output)
	}
}

func TestFromContext_NoLogger(t *testing.T) {
	ctx := context.Background()
	logger := FromContext(ctx)
	if logger == nil {
		t.Fatal("FromContext(empty ctx) returned nil")
	}
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

func TestInfo(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	Info(context.Background(), "info-message", event.String("key", "value"))

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

func TestError(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	err := errors.New("something went wrong")
	Error(context.Background(), "failed", event.String("error", err.Error()), event.String("user", "alice"))

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
	if !strings.Contains(output, "user=alice") {
		t.Errorf("expected output containing 'user=alice', got: %s", output)
	}
}

func TestError_ErrNil(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	// Err(nil) returns zero event.Event — should be skipped, not produce "error=nil".
	Error(context.Background(), "op-failed", event.Err(nil), event.String("user", "alice"))

	output := stop()
	if strings.Contains(output, "error=nil") {
		t.Errorf("expected nil error to be filtered, got: %s", output)
	}
	if !strings.Contains(output, "user=alice") {
		t.Errorf("expected output containing 'user=alice', got: %s", output)
	}
}

func TestDebug(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	Debug(context.Background(), "debug-message", event.String("key", "value"))

	output := stop()
	if strings.Contains(output, "debug-message") {
		t.Errorf("expected debug message to be suppressed, got: %s", output)
	}
}

func TestWarn(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	Warn(context.Background(), "warn-message", event.String("key", "value"))

	output := stop()
	if !strings.Contains(output, "msg=") {
		t.Errorf("expected text output containing 'msg=', got: %s", output)
	}
	if !strings.Contains(output, "warn-message") {
		t.Errorf("expected output containing 'warn-message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected output containing 'key=value', got: %s", output)
	}
}

func TestWith(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	ctx := With(context.Background(), event.String("request_id", "abc123"))
	logger := FromContext(ctx)
	logger.Info("with-test")

	output := stop()
	if !strings.Contains(output, "request_id=abc123") {
		t.Errorf("expected output containing 'request_id=abc123', got: %s", output)
	}
}

func TestWith_Multiple(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	ctx := With(context.Background(), event.String("first", "1"))
	ctx = With(ctx, event.String("second", "2"))
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

func TestWith_NilEventSkipped(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	ctx := With(context.Background(), event.Err(nil), event.String("key", "val"))
	logger := FromContext(ctx)
	logger.Info("msg")

	output := stop()
	if strings.Contains(output, "error=nil") {
		t.Errorf("expected nil event to be filtered, got: %s", output)
	}
	if !strings.Contains(output, "key=val") {
		t.Errorf("expected output containing 'key=val', got: %s", output)
	}
}

func TestLazyInit(t *testing.T) {
	resetInit()

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

func TestWithLogger(t *testing.T) {
	resetInit()

	custom := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx := WithLogger(context.Background(), custom)

	logger := FromContext(ctx)
	if logger != custom {
		t.Error("WithLogger should store the custom logger in context")
	}
}

func TestWithLogger_NilLogger(t *testing.T) {
	resetInit()

	ctx := WithLogger(context.Background(), nil)

	logger := FromContext(ctx)
	if logger != Default() {
		t.Error("WithLogger(nil) should cause FromContext to return Default()")
	}
}

func TestInstallReporter(t *testing.T) {
	resetInit()
	resetReporter()

	var buf bytes.Buffer
	reporter := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	uninstall := InstallReporter(reporter)
	defer uninstall()

	Info(context.Background(), "reporter-msg", event.String("k", "v"))

	output := buf.String()
	if !strings.Contains(output, "reporter-msg") {
		t.Errorf("expected reporter output containing 'reporter-msg', got: %s", output)
	}
	if !strings.Contains(output, "k=v") {
		t.Errorf("expected reporter output containing 'k=v', got: %s", output)
	}
}

func TestUninstallReporter(t *testing.T) {
	resetInit()
	resetReporter()

	var buf bytes.Buffer
	reporter := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	uninstall := InstallReporter(reporter)
	uninstall()

	stop := captureStdout(t)
	defer stop()

	Info(context.Background(), "console-msg")
	output := stop()

	if !strings.Contains(output, "console-msg") {
		t.Errorf("expected console output containing 'console-msg' after uninstall, got: %s", output)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty reporter buffer after uninstall, got: %s", buf.String())
	}
}

func TestUninstallOnlyOwn(t *testing.T) {
	resetInit()
	resetReporter()

	var bufA, bufB bytes.Buffer
	reporterA := slog.New(slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reporterB := slog.New(slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelInfo}))

	unA := InstallReporter(reporterA)
	unB := InstallReporter(reporterB)
	unA() // uninstall A — B should remain active

	Info(context.Background(), "msg-after-unA")

	if bufA.Len() != 0 {
		t.Errorf("reporter A should be inactive after uninstall, got: %s", bufA.String())
	}
	if !strings.Contains(bufB.String(), "msg-after-unA") {
		t.Errorf("reporter B should be active after uninstalling A, got: %s", bufB.String())
	}
	_ = unB
}

func TestInstallReporterNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("InstallReporter(nil) should panic")
		}
	}()
	InstallReporter(nil)
}

func TestSetDefault(t *testing.T) {
	resetInit()
	resetReporter()

	var buf bytes.Buffer
	mockLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	SetDefault(mockLogger)

	Default().Info("bypass")

	output := buf.String()
	if !strings.Contains(output, "bypass") {
		t.Errorf("expected output 'bypass' after SetDefault, got: %s", output)
	}
}
