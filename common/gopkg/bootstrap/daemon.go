package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Worker is a single run instance managed by a Daemon.
//
// Start should block until the worker exits (for long-running workers) or
// return on completion (for one-shot workers). The context is cancelled
// when the daemon is shutting down.
//
// Stop is called to request the worker to exit and clean up resources.
// It must be safe to call multiple times.
type Worker interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// WorkerBuilder creates a new Worker instance for each daemon start/restart.
//
// Build is invoked before each worker Start call — once on initial launch
// and once per restart. It should create a fresh worker instance and may
// perform pre-start recovery logic.
type WorkerBuilder interface {
	Build(ctx context.Context) (Worker, error)
}

// WorkerBuilderFunc is a function adapter for WorkerBuilder.
type WorkerBuilderFunc func(ctx context.Context) (Worker, error)

// Build calls the underlying function.
func (f WorkerBuilderFunc) Build(ctx context.Context) (Worker, error) {
	return f(ctx)
}

// DaemonDecision determines how the daemon responds to a worker error.
type DaemonDecision int

const (
	// DaemonRestart triggers a restart after exponential backoff.
	DaemonRestart DaemonDecision = iota
	// DaemonStop lets the daemon exit cleanly without triggering global shutdown.
	DaemonStop
	// DaemonFatal reports the error via the exit watcher, triggering global shutdown.
	DaemonFatal
)

// DaemonOption configures daemon behaviour.
type DaemonOption func(*daemonConfig)

// daemonConfig holds restart policy configuration.
type daemonConfig struct {
	initialBackoff  time.Duration
	maxBackoff      time.Duration
	maxRestarts     int
	errorClassifier func(context.Context, error) DaemonDecision
}

// WithDaemonRestartBackoff sets the initial backoff duration between restarts.
// Default is 1 second. The backoff doubles after each restart up to maxBackoff.
func WithDaemonRestartBackoff(d time.Duration) DaemonOption {
	return func(c *daemonConfig) {
		c.initialBackoff = d
	}
}

// WithDaemonMaxRestartBackoff sets the maximum backoff duration between restarts.
// Default is 30 seconds.
func WithDaemonMaxRestartBackoff(d time.Duration) DaemonOption {
	return func(c *daemonConfig) {
		c.maxBackoff = d
	}
}

// WithDaemonMaxRestarts sets the maximum number of restart attempts.
// When exhausted, the daemon reports a fatal error. Default is 5.
// A negative value disables the restart limit.
func WithDaemonMaxRestarts(max int) DaemonOption {
	return func(c *daemonConfig) {
		c.maxRestarts = max
	}
}

// WithDaemonErrorClassifier sets a custom error classification function.
// The function receives the daemon's context and the error and returns
// a DaemonDecision.
func WithDaemonErrorClassifier(fn func(context.Context, error) DaemonDecision) DaemonOption {
	return func(c *daemonConfig) {
		c.errorClassifier = fn
	}
}

// daemonComponent adapts a WorkerBuilder into a bootstrap Component.
// It manages a supervisor goroutine that handles worker lifecycle:
// building, starting, restarting, and detecting fatal errors.
type daemonComponent struct {
	name    string
	builder WorkerBuilder
	cfg     daemonConfig

	done chan error // exit watcher channel (buffered, size 1)

	mu        sync.Mutex
	daemonCtx context.Context
	cancel    context.CancelFunc
	worker    Worker
	wg        sync.WaitGroup
	fatalOnce sync.Once
}

// Daemon creates a new daemon Component with the given name and WorkerBuilder.
//
// Start launches the supervisor goroutine and returns nil immediately.
// Stop cancels the daemon's context, calls the current worker's Stop, and
// waits for the supervisor to exit.
//
// The daemon implements the exitWatcher interface — fatal errors are
// reported via the Done() channel, which triggers global shutdown in
// bootstrap.
func Daemon(name string, builder WorkerBuilder, opts ...DaemonOption) Component {
	cfg := daemonConfig{
		initialBackoff: 1 * time.Second,
		maxBackoff:     30 * time.Second,
		maxRestarts:    5,
	}

	for _, o := range opts {
		o(&cfg)
	}

	return &daemonComponent{
		name:    name,
		builder: builder,
		cfg:     cfg,
		done:    make(chan error, 1),
	}
}

// Name returns the component name.
func (c *daemonComponent) Name() string {
	return c.name
}

// Stage returns StageDaemon.
func (c *daemonComponent) Stage() Stage {
	return StageDaemon
}

// Start initialises the daemon context and launches the supervisor goroutine.
// It returns nil immediately.
func (c *daemonComponent) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.daemonCtx != nil {
		return fmt.Errorf("bootstrap: daemon %q already started", c.name)
	}

	c.daemonCtx, c.cancel = context.WithCancel(ctx)
	c.wg.Add(1)
	go c.supervise()

	return nil
}

// Stop cancels the daemon's context, calls the current worker's Stop, and
// waits for the supervisor goroutine to finish. It is safe to call multiple
// times.
func (c *daemonComponent) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	worker := c.worker
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if worker != nil {
		_ = worker.Stop(ctx)
	}

	c.wg.Wait()
	return nil
}

// Done returns a channel that receives a fatal error if the daemon encounters
// an unrecoverable error. It satisfies the exitWatcher interface.
func (c *daemonComponent) Done() <-chan error {
	return c.done
}

// supervise is the main supervisor loop. It manages the worker lifecycle
// — building, starting, and restarting according to the configured policy.
func (c *daemonComponent) supervise() {
	defer c.wg.Done()

	restartCount := 0
	backoff := c.cfg.initialBackoff

	for {
		if err := c.daemonCtx.Err(); err != nil {
			return
		}

		worker, buildErr := c.buildWorker()
		if buildErr != nil {
			if !c.applyRestartPolicy(buildErr, &restartCount, &backoff) {
				return
			}
			continue
		}

		c.mu.Lock()
		c.worker = worker
		c.mu.Unlock()

		startErr := c.startWorker(worker)
		// Shutdown context: worker exit is not an error to restart on.
		if c.daemonCtx.Err() != nil {
			return
		}
		if !c.applyRestartPolicy(startErr, &restartCount, &backoff) {
			return
		}
	}
}

// applyRestartPolicy classifies err and applies the restart policy.
// Returns true if the daemon should restart (continue the supervise loop),
// false if it should exit (stop, fatal, or context cancelled during backoff).
func (c *daemonComponent) applyRestartPolicy(err error, restartCount *int, backoff *time.Duration) bool {
	decision := c.classify(err)
	switch decision {
	case DaemonRestart:
		*restartCount++
		if c.restartsExhausted(*restartCount) {
			c.reportFatal(err)
			return false
		}
		if !c.sleepBackoff(*backoff) {
			return false
		}
		*backoff = c.nextBackoff(*backoff)
		return true
	case DaemonStop:
		return false
	case DaemonFatal:
		c.reportFatal(err)
		return false
	default:
		return false
	}
}

// buildWorker calls builder.Build with panic recovery.
func (c *daemonComponent) buildWorker() (worker Worker, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("bootstrap: daemon %q Build panicked: %v", c.name, r)
		}
	}()
	return c.builder.Build(c.daemonCtx)
}

// startWorker calls worker.Start with panic recovery.
func (c *daemonComponent) startWorker(worker Worker) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("bootstrap: daemon %q Start panicked: %v", c.name, r)
		}
	}()
	return worker.Start(c.daemonCtx)
}

// classify applies the default or custom error classifier.
func (c *daemonComponent) classify(err error) DaemonDecision {
	if c.cfg.errorClassifier != nil {
		return c.cfg.errorClassifier(c.daemonCtx, err)
	}
	return defaultErrorClassifier(c.daemonCtx, err)
}

// restartsExhausted returns true when restart attempts have exceeded the
// configured maximum. A negative maxRestarts disables the limit.
func (c *daemonComponent) restartsExhausted(restartCount int) bool {
	if c.cfg.maxRestarts < 0 {
		return false
	}
	return restartCount > c.cfg.maxRestarts
}

// sleepBackoff sleeps for the given duration, respecting daemon context
// cancellation. Returns false if context was cancelled during sleep.
func (c *daemonComponent) sleepBackoff(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-c.daemonCtx.Done():
		return false
	}
}

// nextBackoff doubles the current backoff up to the configured maximum.
func (c *daemonComponent) nextBackoff(current time.Duration) time.Duration {
	return min(current*2, c.cfg.maxBackoff)
}

// reportFatal sends an error to the done channel exactly once.
func (c *daemonComponent) reportFatal(err error) {
	c.fatalOnce.Do(func() {
		c.done <- err
	})
}

// defaultErrorClassifier provides the built-in error classification.
//
// Rules:
//   - context.Canceled / DeadlineExceeded when daemon ctx is cancelled → DaemonStop
//   - context.Canceled / DeadlineExceeded when daemon ctx is NOT cancelled → DaemonRestart
//   - nil error → DaemonStop (worker finished naturally)
//   - all other errors → DaemonRestart
func defaultErrorClassifier(daemonCtx context.Context, err error) DaemonDecision {
	if err == nil {
		return DaemonStop
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if daemonCtx.Err() != nil {
			return DaemonStop
		}
		return DaemonRestart
	}

	return DaemonRestart
}
