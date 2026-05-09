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

	// isInDeployMode defaults to otel.IsLoggerProviderSet(), which returns
	// false when OTel is not initialised. No explicit setup needed.
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

	oldIsDeploy := isInDeployMode
	oldDeploy := newDeployHandler
	t.Cleanup(func() {
		isInDeployMode = oldIsDeploy
		newDeployHandler = oldDeploy
	})

	// Route log writes to the deploy handler.
	isInDeployMode = func() bool { return true }

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

func TestDebugContext(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	DebugContext(context.Background(), "debug-message", "key", "value")

	output := stop()
	if strings.Contains(output, "debug-message") {
		t.Errorf("expected debug message to be suppressed, got: %s", output)
	}
}

func TestWarnContext(t *testing.T) {
	resetInit()

	stop := captureStdout(t)
	defer stop()

	WarnContext(context.Background(), "warn-message", "key", "value")

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

func Test_DynamicSwitch(t *testing.T) {
	resetInit()

	oldIsDeploy := isInDeployMode
	oldDeploy := newDeployHandler
	oldConsole := newConsoleHandler
	t.Cleanup(func() {
		isInDeployMode = oldIsDeploy
		newDeployHandler = oldDeploy
		newConsoleHandler = oldConsole
	})

	var consoleBuf, deployBuf bytes.Buffer

	newConsoleHandler = func() slog.Handler {
		return slog.NewTextHandler(&consoleBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	newDeployHandler = func(name string) slog.Handler {
		return slog.NewTextHandler(&deployBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	// given: OTel is not ready
	isInDeployMode = func() bool { return false }

	resetInit()
	logger := Default()

	// when: log in console mode
	logger.Info("console-msg")

	// then: console handler receives the message, deploy handler does not
	if !strings.Contains(consoleBuf.String(), "console-msg") {
		t.Errorf("console message missing, got: %s", consoleBuf.String())
	}
	if strings.Contains(deployBuf.String(), "console-msg") {
		t.Error("deploy handler should not receive messages in console mode")
	}

	// given: OTel becomes ready
	consoleBuf.Reset()
	deployBuf.Reset()
	isInDeployMode = func() bool { return true }

	// when: log after the switch
	logger.Info("deploy-msg")

	// then: deploy handler receives the message, console handler does not
	if strings.Contains(consoleBuf.String(), "deploy-msg") {
		t.Errorf("console handler should not receive messages in deploy mode, got: %s", consoleBuf.String())
	}
	if !strings.Contains(deployBuf.String(), "deploy-msg") {
		t.Errorf("deploy handler should receive messages after switch, got: %s", deployBuf.String())
	}
}

func Test_DynamicSwitch_WithAttrs(t *testing.T) {
	resetInit()

	oldIsDeploy := isInDeployMode
	oldDeploy := newDeployHandler
	oldConsole := newConsoleHandler
	t.Cleanup(func() {
		isInDeployMode = oldIsDeploy
		newDeployHandler = oldDeploy
		newConsoleHandler = oldConsole
	})

	var consoleBuf, deployBuf bytes.Buffer

	newConsoleHandler = func() slog.Handler {
		return slog.NewTextHandler(&consoleBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	newDeployHandler = func(name string) slog.Handler {
		return slog.NewTextHandler(&deployBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	// given: WithAttrs is called when OTel is not ready
	isInDeployMode = func() bool { return false }

	resetInit()
	logger := Default()
	enriched := logger.With("key", "value")

	// given: OTel becomes ready after WithAttrs
	isInDeployMode = func() bool { return true }

	// when: log through the enriched logger
	enriched.Info("with-attrs-test")

	// then: attrs are present in the deploy handler output
	output := deployBuf.String()
	if !strings.Contains(output, "key=value") {
		t.Errorf("attr 'key=value' should be present in deploy output, got: %s", output)
	}
	if !strings.Contains(output, "with-attrs-test") {
		t.Errorf("message missing in deploy output: %s", output)
	}
	// Attrs should not appear in console output after the switch
	if strings.Contains(consoleBuf.String(), "with-attrs-test") {
		t.Errorf("console should not receive messages after switch, got: %s", consoleBuf.String())
	}
}

func Test_DynamicSwitch_WithGroup(t *testing.T) {
	resetInit()

	oldIsDeploy := isInDeployMode
	oldDeploy := newDeployHandler
	oldConsole := newConsoleHandler
	t.Cleanup(func() {
		isInDeployMode = oldIsDeploy
		newDeployHandler = oldDeploy
		newConsoleHandler = oldConsole
	})

	var consoleBuf, deployBuf bytes.Buffer

	newConsoleHandler = func() slog.Handler {
		return slog.NewTextHandler(&consoleBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	newDeployHandler = func(name string) slog.Handler {
		return slog.NewTextHandler(&deployBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	// given: WithGroup is called when OTel is not ready
	isInDeployMode = func() bool { return false }

	resetInit()
	logger := Default()
	grouped := logger.WithGroup("grp")

	// given: OTel becomes ready after WithGroup
	isInDeployMode = func() bool { return true }

	// when: log through the grouped logger
	grouped.Info("with-group-test")

	// then: message reaches the deploy handler
	output := deployBuf.String()
	if !strings.Contains(output, "with-group-test") {
		t.Errorf("message missing in deploy output: %s", output)
	}
	// Console handler should not receive the message
	if strings.Contains(consoleBuf.String(), "with-group-test") {
		t.Errorf("console should not receive messages after switch, got: %s", consoleBuf.String())
	}
}

func Test_SetDefault_BypassesDynamicSwitch(t *testing.T) {
	resetInit()

	oldIsDeploy := isInDeployMode
	t.Cleanup(func() { isInDeployMode = oldIsDeploy })

	var buf bytes.Buffer
	mockLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	SetDefault(mockLogger)

	// given: deploy mode toggled — SetDefault bypasses the dynamic handler
	isInDeployMode = func() bool { return false }

	// when: log with deploy mode off
	Default().Info("bypass-1")

	output := buf.String()
	if !strings.Contains(output, "bypass-1") {
		t.Errorf("expected output 'bypass-1' in deploy-off mode, got: %s", output)
	}

	// when: log with deploy mode on
	buf.Reset()
	isInDeployMode = func() bool { return true }
	Default().Info("bypass-2")

	output = buf.String()
	if !strings.Contains(output, "bypass-2") {
		t.Errorf("expected output 'bypass-2' in deploy-on mode, got: %s", output)
	}
}
