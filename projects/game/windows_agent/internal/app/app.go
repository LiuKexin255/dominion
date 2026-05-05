package app

import (
	"context"
	"sync"

	agentlog "dominion/projects/game/windows_agent/internal/log"
	agentruntime "dominion/projects/game/windows_agent/internal/runtime"
	"dominion/projects/game/windows_agent/internal/sessionclient"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// runtimeService defines the Runtime operations needed by the App layer.
type runtimeService interface {
	Connect(ctx context.Context, connectURL string) error
	Disconnect() error
	BindWindow(hwnd uintptr) error
	StartCapture(ctx context.Context) error
	StopCapture() error
	ClearWindow() error
}

// App is the Wails application glue layer that exposes runtime operations to the frontend.
// Exported methods are auto-bound by Wails and callable from JavaScript via
// window.go.main.App.MethodName().
type App struct {
	ctx context.Context
	rt  runtimeService
	sc  *sessionclient.Client

	currentSession *Session
	sessionMu      sync.RWMutex

	mu     sync.RWMutex
	status AgentStatus

	emitFunc func(ctx context.Context, name string, data ...interface{})

	initErrors     []string
	deferredEvents []deferredEvent
}

type deferredEvent struct {
	name string
	data interface{}
}

type appConfig struct {
	ffmpegPath string
	helperPath string
	initErrors []string
}

// AppOption configures App construction.
type AppOption func(*appConfig)

// WithFFmpegPath sets the ffmpeg executable path used by the runtime.
func WithFFmpegPath(path string) AppOption {
	return func(cfg *appConfig) {
		cfg.ffmpegPath = path
	}
}

// WithHelperPath sets the input helper executable path used by the runtime.
func WithHelperPath(path string) AppOption {
	return func(cfg *appConfig) {
		cfg.helperPath = path
	}
}

// WithInitErrors sets initialization errors to report after frontend subscription.
func WithInitErrors(errors []string) AppOption {
	return func(cfg *appConfig) {
		cfg.initErrors = append([]string(nil), errors...)
	}
}

// NewApp creates an App with a default Runtime and Wails event emitter.
func NewApp(options ...AppOption) *App {
	cfg := appConfig{}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}
	return &App{
		rt:         agentruntime.NewRuntime(cfg.ffmpegPath, cfg.helperPath),
		sc:         sessionclient.NewClient(nil),
		status:     AgentStatus{State: "Disconnected", SessionServiceState: "unknown"},
		emitFunc:   wailsrt.EventsEmit,
		initErrors: append([]string(nil), cfg.initErrors...),
	}
}

// WailsInit stores the Wails context and initializes the global window logger.
func (a *App) WailsInit(ctx context.Context) error {
	a.ctx = ctx

	emitFn := func(name string, data interface{}) {
		if a.ctx != nil && a.emitFunc != nil {
			a.emitFunc(a.ctx, name, data)
		}
	}
	agentlog.SetGlobal(agentlog.NewLogger(emitFn))

	a.log("info", "app started", nil)
	for _, errMsg := range a.initErrors {
		a.deferredEvents = append(a.deferredEvents,
			deferredEvent{name: EventLogEntry, data: a.logEntry("error", errMsg, nil)},
		)
	}
	return nil
}

// FlushInitErrors emits deferred initialization errors after the frontend subscribes.
func (a *App) FlushInitErrors() {
	for _, event := range a.deferredEvents {
		a.emitEvent(event.name, event.data)
	}
	a.deferredEvents = nil
}

// WailsShutdown disconnects the runtime and cleans up all resources.
func (a *App) WailsShutdown() {
	a.log("info", "app shutting down", nil)
	_ = a.rt.Disconnect()
}

// setStatus updates the status under the write lock.
func (a *App) setStatus(fn func(*AgentStatus)) {
	a.mu.Lock()
	fn(&a.status)
	a.mu.Unlock()
}

func (a *App) setSessionServiceState(state, errMsg string) {
	a.setStatus(func(s *AgentStatus) {
		s.SessionServiceState = state
		s.SessionServiceError = errMsg
	})
	a.emitStatusChanged()
}
