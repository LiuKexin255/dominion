package runtime

import (
	"context"
	"io"
	"sync"
	"time"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/windows_agent/internal/capture"
	"dominion/projects/game/windows_agent/internal/encoder"
	"dominion/projects/game/windows_agent/internal/input"
	"dominion/projects/game/windows_agent/internal/media"
	"dominion/projects/game/windows_agent/internal/transport"
	"dominion/projects/game/windows_agent/internal/window"
)

// AgentState is the runtime lifecycle state for the Windows agent.
type AgentState int

const (
	// StateDisconnected means no active gateway or subsystem session exists.
	StateDisconnected AgentState = iota
	// StateConnecting means the gateway WebSocket connection is being established.
	StateConnecting
	// StateConnected means the gateway connection and hello handshake are complete.
	StateConnected
	// StateBound means a target window has been selected for capture and input.
	StateBound
	// StateStreaming means ffmpeg capture and media forwarding are active.
	StateStreaming
	// StateError means a subsystem failed and the runtime needs cleanup.
	StateError
)

// TransportClient sends and receives gateway WebSocket messages.
type TransportClient interface {
	Connect(ctx context.Context, connectURL string) error
	Close() error
	SendHello(ctx context.Context, sessionID string) error
	SendMediaInit(ctx context.Context, sessionID, mimeType string, segment []byte) error
	SendMediaSegment(ctx context.Context, sessionID, segmentID string, segment []byte, keyframe bool) error
	SendControlAck(ctx context.Context, sessionID, operationID string) error
	SendControlResult(ctx context.Context, sessionID, operationID string, status gw.GameControlResultStatus) error
	SendPong(ctx context.Context, sessionID, nonce string) error
	ReadLoop(ctx context.Context) (<-chan transport.InboundMessage, error)
}

// WindowEnumerator enumerates and validates top-level windows.
type WindowEnumerator interface {
	EnumerateWindows() ([]window.WindowInfo, error)
	IsWindowValid(hwnd uintptr) bool
}

// InputExecutor manages input-helper IPC.
type InputExecutor interface {
	Start(helperPath string) error
	Stop() error
	ExecuteCommand(ctx context.Context, cmd input.Command) (input.Response, error)
	ReleaseAll() error
}

// MediaEncoder manages one ffmpeg process used for media capture.
type MediaEncoder interface {
	Start(ctx context.Context, config encoder.EncoderConfig) error
	StdoutPipe() io.Reader
	Stop() error
	Wait() error
}

// MediaParser parses fragmented MP4 data from ffmpeg stdout.
type MediaParser func(io.Reader) (*media.ParseResult, error)

// Runtime coordinates transport, window binding, capture, encoding, media, and input.
type Runtime struct {
	state      AgentState
	session    *Session
	transport  TransportClient
	windowMgr  WindowEnumerator
	captureCfg capture.CaptureConfig
	encoder    MediaEncoder
	inputMgr   InputExecutor
	parseMedia MediaParser

	boundWindow *window.WindowInfo
	mediaDone   chan error

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	lastError error
	segCount  int64
	startTime time.Time
}

type defaultWindowManager struct{}

func (defaultWindowManager) EnumerateWindows() ([]window.WindowInfo, error) {
	return window.EnumerateWindows()
}

func (defaultWindowManager) IsWindowValid(hwnd uintptr) bool {
	return window.IsWindowValid(hwnd)
}
