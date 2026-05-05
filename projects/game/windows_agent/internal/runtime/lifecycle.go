package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dominion/projects/game/windows_agent/internal/capture"
	"dominion/projects/game/windows_agent/internal/encoder"
	"dominion/projects/game/windows_agent/internal/input"
	"dominion/projects/game/windows_agent/internal/media"
	"dominion/projects/game/windows_agent/internal/transport"
	"dominion/projects/game/windows_agent/internal/window"
)

// NewRuntime creates a disconnected runtime with default subsystem adapters.
func NewRuntime(ffmpegPath, helperPath string) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{
		connState:   ConnDisconnected,
		streamState: StreamIdle,
		transport:   transport.NewClient(),
		windowMgr:   defaultWindowManager{},
		captureCfg:  capture.DefaultCaptureConfig(),
		encoder:     encoder.NewEncoder(ffmpegPath),
		inputMgr:    input.NewManager(),
		parseMedia:  media.ParseStreaming,
		ffmpegPath:  ffmpegPath,
		helperPath:  helperPath,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// ConnectionState returns the current connection state.
func (r *Runtime) ConnectionState() ConnectionState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.connState
}

// StreamingState returns the current streaming state.
func (r *Runtime) StreamingState() StreamingState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.streamState
}

// Connect establishes the WebSocket connection, sends hello, and enters ConnConnected.
func (r *Runtime) Connect(ctx context.Context, connectURL string) error {
	sessionID, err := ParseSessionURL(connectURL)
	if err != nil {
		return err
	}

	r.mu.Lock()
	if r.connState != ConnDisconnected {
		r.mu.Unlock()
		return fmt.Errorf("already connected or connecting")
	}
	r.connState = ConnConnecting
	r.session = &Session{ID: sessionID, ConnectURL: connectURL, Role: sessionRoleWindowsAgent}
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.startTime = time.Now()
	r.lastConnError = nil
	r.mu.Unlock()

	if err := r.transport.Connect(ctx, connectURL); err != nil {
		r.setConnError(err)
		return err
	}
	if err := r.SendHello(ctx); err != nil {
		r.setConnError(err)
		return err
	}

	r.mu.Lock()
	r.connState = ConnConnected
	r.mu.Unlock()

	r.startReadLoopConsumer()
	return nil
}

// SendHello sends the gateway hello message for the current session.
func (r *Runtime) SendHello(ctx context.Context) error {
	session := r.currentSession()
	if session == nil {
		return fmt.Errorf("session is not initialized")
	}
	return r.transport.SendHello(ctx, session.ID)
}

// BindWindow validates and stores a target HWND.
func (r *Runtime) BindWindow(hwnd uintptr) error {
	if err := r.ensureConnState(ConnConnected); err != nil {
		return err
	}
	if !r.windowMgr.IsWindowValid(hwnd) {
		return fmt.Errorf("window handle is invalid: %d", hwnd)
	}

	windows, err := r.windowMgr.EnumerateWindows()
	if err != nil {
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
	r.mu.Unlock()
	return nil
}

// ClearWindow unbinds the current window after stopping capture when needed.
func (r *Runtime) ClearWindow() error {
	r.mu.RLock()
	streamState := r.streamState
	r.mu.RUnlock()

	if streamState == StreamStreaming {
		if err := r.StopCapture(); err != nil {
			return fmt.Errorf("stop capture before clear window: %w", err)
		}
	}

	r.mu.Lock()
	r.boundWindow = nil
	r.mu.Unlock()
	return nil
}

// StartCapture starts ffmpeg capture and media forwarding.
func (r *Runtime) StartCapture(ctx context.Context) error {
	if err := r.ensureConnState(ConnConnected); err != nil {
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

	r.mu.Lock()
	if r.streamState != StreamIdle && r.streamState != StreamError {
		r.mu.Unlock()
		return fmt.Errorf("streaming already started")
	}
	r.streamState = StreamStarting
	r.lastStreamError = nil
	r.mu.Unlock()

	config := encoder.DefaultConfig()
	config.HWND = boundWindow.HWND
	config.FrameRate = captureCfg.FrameRate
	config.MaxWidth = captureCfg.MaxWidth
	config.MaxHeight = captureCfg.MaxHeight

	if err := r.encoder.Start(ctx, config); err != nil {
		r.setStreamError(err)
		r.stopCaptureSubsystems()
		return err
	}
	if err := r.inputMgr.Start(r.helperPath); err != nil {
		r.setStreamError(err)
		r.stopCaptureSubsystems()
		return err
	}
	if err := r.startMediaFlow(); err != nil {
		r.setStreamError(err)
		r.stopCaptureSubsystems()
		return err
	}

	r.mu.Lock()
	r.streamState = StreamStreaming
	r.mu.Unlock()
	return nil
}

// StopCapture stops media streaming without disconnecting from the gateway.
func (r *Runtime) StopCapture() error {
	r.mu.Lock()
	if r.streamState != StreamStreaming && r.streamState != StreamError {
		r.mu.Unlock()
		return fmt.Errorf("not streaming")
	}
	r.streamState = StreamStopping
	r.mu.Unlock()

	var err error
	if r.encoder != nil {
		err = r.encoder.Stop()
	}
	if r.inputMgr != nil {
		err = errors.Join(err, r.inputMgr.Stop())
	}

	r.mu.Lock()
	r.streamState = StreamIdle
	r.lastStreamError = err
	r.mu.Unlock()
	return err
}

// Disconnect cleanly shuts down all subsystems and enters ConnDisconnected.
func (r *Runtime) Disconnect() error {
	return r.cleanup()
}

func (r *Runtime) ensureConnState(want ConnectionState) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.connState != want {
		return fmt.Errorf("requires %s, but current connection state is %s", want, r.connState)
	}
	return nil
}

func (r *Runtime) setConnError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastConnError = err
	r.connState = ConnDisconnected
}

func (r *Runtime) setStreamError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastStreamError = err
	r.streamState = StreamError
}

func (r *Runtime) currentSession() *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.session
}

// stopCaptureSubsystems stops encoder and input without changing state.
func (r *Runtime) stopCaptureSubsystems() {
	if r.encoder != nil {
		r.encoder.Stop()
	}
	if r.inputMgr != nil {
		r.inputMgr.Stop()
	}
}
