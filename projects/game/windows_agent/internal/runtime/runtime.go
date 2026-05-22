package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	runtimepb "dominion/projects/game/runtime"
	"dominion/projects/game/windows_agent/internal/capture"
	"dominion/projects/game/windows_agent/internal/encoder"
	"dominion/projects/game/windows_agent/internal/input"
	"dominion/projects/game/windows_agent/internal/media"
	"dominion/projects/game/windows_agent/internal/transport"
	"dominion/projects/game/windows_agent/internal/window"
)

// ConnectionState represents the WebSocket connection lifecycle.
type ConnectionState int

const (
	// ConnDisconnected means no active gateway connection.
	ConnDisconnected ConnectionState = iota
	// ConnConnecting means the gateway WebSocket connection is being established.
	ConnConnecting
	// ConnConnected means the gateway connection and hello handshake are complete.
	ConnConnected
)

func (s ConnectionState) String() string {
	switch s {
	case ConnDisconnected:
		return "disconnected"
	case ConnConnecting:
		return "connecting"
	case ConnConnected:
		return "connected"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// StreamingState represents the media capture lifecycle.
type StreamingState int

const (
	// StreamIdle means no active capture.
	StreamIdle StreamingState = iota
	// StreamStarting means capture is being initialized.
	StreamStarting
	// StreamStreaming means ffmpeg capture and media forwarding are active.
	StreamStreaming
	// StreamStopping means capture is being stopped.
	StreamStopping
	// StreamError means capture failed.
	StreamError
)

func (s StreamingState) String() string {
	switch s {
	case StreamIdle:
		return "idle"
	case StreamStarting:
		return "starting"
	case StreamStreaming:
		return "streaming"
	case StreamStopping:
		return "stopping"
	case StreamError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// TransportClient sends and receives gateway WebSocket messages.
type TransportClient interface {
	Connect(ctx context.Context, connectURL string) error
	Close() error
	SendHello(ctx context.Context, sessionID string) error
	SendMediaInit(ctx context.Context, sessionID, streamID, initID, mimeType, codec string, segment []byte) error
	SendMediaSegment(ctx context.Context, sessionID, streamID, initID string, sequence uint64, segment []byte, randomAccess *bool, durationMS int32, discontinuity bool) error
	SendControlAck(ctx context.Context, sessionID, operationID string) error
	SendControlResult(ctx context.Context, sessionID, operationID string, status runtimepb.GameControlResultStatus) error
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

// MediaParser reads an fMP4 stream and delivers init and media segments via
// callbacks as they are parsed, without waiting for EOF.
type MediaParser func(r io.Reader, onInit func(*media.InitSegment) error, onMedia func(*media.MediaSegment) error) error

// Runtime coordinates transport, window binding, capture, encoding, media, and input.
type Runtime struct {
	connState   ConnectionState
	streamState StreamingState
	session     *Session
	transport   TransportClient
	windowMgr   WindowEnumerator
	captureCfg  capture.CaptureConfig
	encoder     MediaEncoder
	inputMgr    InputExecutor
	parseMedia  MediaParser
	ffmpegPath  string
	helperPath  string

	boundWindow *window.WindowInfo
	mediaDone   chan error

	streamID string // generated at StartCapture, reset on StopCapture
	initID   string // from latest ParseMediaInit
	sequence uint64 // per-stream sequence counter (starts at 1)

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	lastConnError   error
	lastStreamError error
	segCount        int64
	startTime       time.Time
}

type defaultWindowManager struct{}

// SetEncoder replaces the runtime media encoder, primarily for tests.
func (r *Runtime) SetEncoder(e MediaEncoder) {
	r.encoder = e
}

// SetInputMgr replaces the runtime input manager, primarily for tests.
func (r *Runtime) SetInputMgr(exec InputExecutor) {
	r.inputMgr = exec
}

func (defaultWindowManager) EnumerateWindows() ([]window.WindowInfo, error) {
	return window.EnumerateWindows()
}

func (defaultWindowManager) IsWindowValid(hwnd uintptr) bool {
	return window.IsWindowValid(hwnd)
}
