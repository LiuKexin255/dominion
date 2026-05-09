// Package bootstrap provides a structured lifecycle manager for application components.
//
// Bootstrap orchestrates component startup (in Stage+Name order), monitors
// running servers, and handles graceful shutdown with proper ordering and
// error aggregation.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
)

// Bootstrap manages a set of application components through their lifecycle.
type Bootstrap struct {
	components []Component
	cfg        config

	mu      sync.Mutex
	running bool

	stopOnce sync.Once
}

// New creates a Bootstrap with the given options applied to its configuration.
func New(opts ...Option) *Bootstrap {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}
	return &Bootstrap{
		cfg: *cfg,
	}
}

// Register adds a component to the bootstrap.
//
// Returns an error if a component with the same Name() is already registered,
// or if Run has already been called.
func (b *Bootstrap) Register(c Component) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return errors.New("bootstrap: cannot register component after Run has been called")
	}

	for _, existing := range b.components {
		if existing.Name() == c.Name() {
			return fmt.Errorf("bootstrap: duplicate component name %q", c.Name())
		}
	}

	b.components = append(b.components, c)
	return nil
}

// Run starts all registered components, waits for a shutdown signal, and
// gracefully stops them. It uses os.Interrupt and syscall.SIGTERM as default
// shutdown signals.
func (b *Bootstrap) Run(ctx context.Context) error {
	return b.RunSignal(ctx, os.Interrupt, syscall.SIGTERM)
}

// exitWatcher is optionally implemented by components to signal when the
// component has exited unexpectedly.
type exitWatcher interface {
	Done() <-chan error
}

// RunSignal starts all registered components, then waits for a shutdown
// trigger (signal, context cancellation, or server exit). On shutdown it
// stops all components in reverse start order with a unified deadline.
func (b *Bootstrap) RunSignal(ctx context.Context, signals ...os.Signal) error {
	// 1. Mark running state to prevent further Register calls.
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return errors.New("bootstrap: Run has already been called")
	}
	b.running = true
	// Snapshot components under the lock.
	sorted := make([]Component, len(b.components))
	copy(sorted, b.components)
	b.mu.Unlock()

	// 2. Sort components: Stage asc, then Name asc.
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Stage() != sorted[j].Stage() {
			return sorted[i].Stage() < sorted[j].Stage()
		}
		return sorted[i].Name() < sorted[j].Name()
	})

	// 3. Start each component sequentially. Track which ones started successfully.
	slog.InfoContext(ctx, "bootstrap starting", "components", len(sorted), "signals", fmtSignalNames(signals))
	var started []Component
	for _, c := range sorted {
		if err := c.Start(ctx); err != nil {
			slog.ErrorContext(ctx, "component start failed, rolling back", "component", c.Name(), "stage", c.Stage().String(), "error", err)
			// 4. Rollback: stop all successfully-started components in reverse order.
			rollbackErr := b.shutdown(started)
			startErr := fmt.Errorf("bootstrap: component %q failed to start: %w", c.Name(), err)
			return errors.Join(startErr, rollbackErr)
		}
		slog.InfoContext(ctx, "component started", "component", c.Name(), "stage", c.Stage().String())
		started = append(started, c)
	}

	// 5. Create signal.NotifyContext to listen for shutdown signals.
	signalCtx, signalStop := signal.NotifyContext(ctx, signals...)
	defer signalStop()

	// 6. Start monitoring goroutine for server component exit.
	serverExit := make(chan error, 1)
	go b.monitorExitWatchers(started, serverExit)

	// 7. Wait for shutdown trigger.
	var shutdownCause error
	select {
	case <-signalCtx.Done():
		// Clean shutdown (signal or context cancellation).
	case err := <-serverExit:
		// Server exited unexpectedly.
		shutdownCause = err
	}

	// 8-12. Shutdown: use sync.Once to ensure shutdown only executes once.
	var shutdownErr error
	b.stopOnce.Do(func() {
		shutdownErr = b.shutdown(started)
	})

	// 13. Return appropriate error: clean signal shutdown returns nil.
	if shutdownCause != nil {
		return errors.Join(shutdownCause, shutdownErr)
	}
	return shutdownErr
}

// shutdown stops all started components in reverse order using the
// configured shutdown timeout as a unified deadline.
func (b *Bootstrap) shutdown(started []Component) error {
	slog.Info("shutdown starting", "components", len(started), "timeout", b.cfg.shutdownTimeout)
	stopCtx, cancel := context.WithTimeout(context.Background(), b.cfg.shutdownTimeout)
	defer cancel()

	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		c := started[i]
		func() {
			defer func() {
				if r := recover(); r != nil {
					errs = append(errs, fmt.Errorf("bootstrap: component %q Stop panicked: %v", c.Name(), r))
				}
			}()
			if err := c.Stop(stopCtx); err != nil {
				slog.Error("component stop failed", "component", c.Name(), "error", err)
				errs = append(errs, fmt.Errorf("bootstrap: component %q Stop: %w", c.Name(), err))
			}
		}()
	}
	return errors.Join(errs...)
}

// monitorExitWatchers watches all components that implement exitWatcher for
// unexpected exits.
func (b *Bootstrap) monitorExitWatchers(components []Component, exitCh chan<- error) {
	for _, c := range components {
		ew, ok := c.(exitWatcher)
		if !ok {
			continue
		}
		go func(name string, done <-chan error) {
			if err := <-done; err != nil {
				slog.Error("component exited unexpectedly", "component", name, "error", err)
				select {
				case exitCh <- fmt.Errorf("bootstrap: component %q exited: %w", name, err):
				default:
				}
			}
		}(c.Name(), ew.Done())
	}
}

// fmtSignalNames returns a comma-separated string of signal names for logging.
func fmtSignalNames(signals []os.Signal) string {
	names := make([]string, len(signals))
	for i, s := range signals {
		names[i] = s.String()
	}
	return strings.Join(names, ",")
}
