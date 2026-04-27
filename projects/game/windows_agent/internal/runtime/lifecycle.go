package runtime

import (
	"context"
	"fmt"
	"time"

	"dominion/projects/game/windows_agent/internal/capture"
	"dominion/projects/game/windows_agent/internal/encoder"
	"dominion/projects/game/windows_agent/internal/media"
	"dominion/projects/game/windows_agent/internal/transport"
	"dominion/projects/game/windows_agent/internal/window"
)

// NewRuntime creates a disconnected runtime with default subsystem adapters.
func NewRuntime() *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{
		state:      StateDisconnected,
		transport:  transport.NewClient(),
		windowMgr:  defaultWindowManager{},
		captureCfg: capture.DefaultCaptureConfig(),
		parseMedia: media.Parse,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// State returns the current runtime lifecycle state.
func (r *Runtime) State() AgentState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// Connect establishes the WebSocket connection, sends hello, and enters StateConnected.
func (r *Runtime) Connect(ctx context.Context, connectURL string) error {
	sessionID, err := ParseSessionURL(connectURL)
	if err != nil {
		return err
	}
	if err := r.transition(StateDisconnected, StateConnecting); err != nil {
		return err
	}

	r.mu.Lock()
	r.session = &Session{ID: sessionID, ConnectURL: connectURL, Role: sessionRoleWindowsAgent}
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.startTime = time.Now()
	r.mu.Unlock()

	if err := r.transport.Connect(ctx, connectURL); err != nil {
		r.setError(err)
		return err
	}
	if err := r.SendHello(ctx); err != nil {
		r.setError(err)
		return err
	}
	return r.transition(StateConnecting, StateConnected)
}

// SendHello sends the gateway hello message for the current session.
func (r *Runtime) SendHello(ctx context.Context) error {
	session := r.currentSession()
	if session == nil {
		return fmt.Errorf("session is not initialized")
	}
	return r.transport.SendHello(ctx, session.ID)
}

// BindWindow validates and stores a target HWND, then enters StateBound.
func (r *Runtime) BindWindow(hwnd uintptr) error {
	if err := r.ensureState(StateConnected); err != nil {
		return err
	}
	if !r.windowMgr.IsWindowValid(hwnd) {
		return fmt.Errorf("window handle is invalid: %d", hwnd)
	}

	windows, err := r.windowMgr.EnumerateWindows()
	if err != nil {
		r.setError(err)
		return err
	}
	info := window.WindowInfo{HWND: hwnd}
	for _, candidate := range windows {
		if candidate.HWND == hwnd {
			info = candidate
			break
		}
	}

	r.mu.Lock()
	r.boundWindow = &info
	r.captureCfg = capture.CaptureConfig{
		Mode:      capture.SelectStrategy(&info),
		HWND:      info.HWND,
		Title:     info.Title,
		Rect:      capture.Rect{Left: info.Rect.Left, Top: info.Rect.Top, Right: info.Rect.Right, Bottom: info.Rect.Bottom},
		FrameRate: r.captureCfg.FrameRate,
		MaxWidth:  r.captureCfg.MaxWidth,
		MaxHeight: r.captureCfg.MaxHeight,
	}
	r.state = StateBound
	r.mu.Unlock()
	return nil
}

// StartCapture starts ffmpeg capture and media forwarding, then enters StateStreaming.
func (r *Runtime) StartCapture(ctx context.Context) error {
	if err := r.ensureState(StateBound); err != nil {
		return err
	}
	if r.encoder == nil {
		return fmt.Errorf("encoder is not configured")
	}

	r.mu.RLock()
	boundWindow := r.boundWindow
	captureCfg := r.captureCfg
	r.mu.RUnlock()
	if boundWindow == nil {
		return fmt.Errorf("window is not bound")
	}
	config := encoder.DefaultConfig()
	config.HWND = boundWindow.HWND
	config.FrameRate = captureCfg.FrameRate
	config.MaxWidth = captureCfg.MaxWidth
	config.MaxHeight = captureCfg.MaxHeight

	if err := r.encoder.Start(ctx, config); err != nil {
		r.setError(err)
		return err
	}
	if err := r.startMediaFlow(); err != nil {
		r.setError(err)
		return err
	}
	return r.transition(StateBound, StateStreaming)
}

// StopCapture stops media streaming without disconnecting from the gateway.
func (r *Runtime) StopCapture() error {
	if err := r.ensureState(StateStreaming); err != nil {
		return err
	}
	var err error
	if r.encoder != nil {
		err = r.encoder.Stop()
	}
	if transErr := r.transition(StateStreaming, StateBound); transErr != nil && err == nil {
		err = transErr
	}
	if err != nil {
		r.setError(err)
	}
	return err
}

// Disconnect cleanly shuts down all subsystems and enters StateDisconnected.
func (r *Runtime) Disconnect() error {
	return r.cleanup()
}

func (r *Runtime) transition(from AgentState, to AgentState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != from {
		return fmt.Errorf("invalid state transition: %d -> %d", r.state, to)
	}
	r.state = to
	return nil
}

func (r *Runtime) ensureState(want AgentState) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state != want {
		return fmt.Errorf("invalid state: got %d, want %d", r.state, want)
	}
	return nil
}

func (r *Runtime) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastError = err
	r.state = StateError
}

func (r *Runtime) currentSession() *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.session
}
