package app

import (
	"context"
	"sync"

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
}

// NewApp creates an App with a default Runtime and Wails event emitter.
func NewApp(paths ...string) *App {
	var ffmpegPath, helperPath string
	if len(paths) > 0 {
		ffmpegPath = paths[0]
	}
	if len(paths) > 1 {
		helperPath = paths[1]
	}
	return &App{
		rt:       agentruntime.NewRuntime(ffmpegPath, helperPath),
		sc:       sessionclient.NewClient(nil),
		status:   AgentStatus{State: "Disconnected", SessionServiceState: "unknown"},
		emitFunc: wailsrt.EventsEmit,
	}
}

// WailsInit stores the Wails context for event emission.
func (a *App) WailsInit(ctx context.Context) error {
	a.ctx = ctx
	a.log("info", "app started", nil)
	return nil
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
