package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/windows_agent/internal/encoder"
	"dominion/projects/game/windows_agent/internal/input"
	"dominion/projects/game/windows_agent/internal/media"
	"dominion/projects/game/windows_agent/internal/transport"
	"dominion/projects/game/windows_agent/internal/window"
)

func TestStateTransitions(t *testing.T) {
	// given
	r := newTestRuntime()
	r.encoder = &fakeEncoder{stdout: bytes.NewReader(fmp4Stream())}

	// when
	if err := r.Connect(context.Background(), "wss://example.test/v1/sessions/session-1/game/connect?token=t"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	if r.ConnectionState() != ConnConnected {
		t.Fatalf("ConnectionState() = %d, want %d", r.ConnectionState(), ConnConnected)
	}
	if err := r.BindWindow(100); err != nil {
		t.Fatalf("BindWindow() unexpected error: %v", err)
	}
	if r.StreamingState() != StreamIdle {
		t.Fatalf("StreamingState() = %d, want %d", r.StreamingState(), StreamIdle)
	}
	if err := r.StartCapture(context.Background()); err != nil {
		t.Fatalf("StartCapture() unexpected error: %v", err)
	}

	// then
	if r.StreamingState() != StreamStreaming {
		t.Fatalf("StreamingState() = %d, want %d", r.StreamingState(), StreamStreaming)
	}
}

func TestStreamingToDisconnected(t *testing.T) {
	// given
	r := newTestRuntime()
	r.encoder = &fakeEncoder{stdout: bytes.NewReader(fmp4Stream())}
	if err := r.Connect(context.Background(), "wss://example.test/v1/sessions/session-1/game/connect"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	if err := r.BindWindow(100); err != nil {
		t.Fatalf("BindWindow() unexpected error: %v", err)
	}
	if err := r.StartCapture(context.Background()); err != nil {
		t.Fatalf("StartCapture() unexpected error: %v", err)
	}

	// when
	if err := r.Disconnect(); err != nil {
		t.Fatalf("Disconnect() unexpected error: %v", err)
	}

	// then
	if r.ConnectionState() != ConnDisconnected {
		t.Fatalf("ConnectionState() = %d, want %d", r.ConnectionState(), ConnDisconnected)
	}
	if r.StreamingState() != StreamIdle {
		t.Fatalf("StreamingState() = %d, want %d", r.StreamingState(), StreamIdle)
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Runtime) error
	}{
		{name: "bind before connect", run: func(r *Runtime) error { return r.BindWindow(100) }},
		{name: "start before bind", run: func(r *Runtime) error { return r.StartCapture(context.Background()) }},
		{name: "stop before streaming", run: func(r *Runtime) error { return r.StopCapture() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			r := newTestRuntime()

			// when
			err := tt.run(r)

			// then
			if err == nil {
				t.Fatalf("invalid transition expected error")
			}
		})
	}
}

func TestParseSessionURL(t *testing.T) {
	tests := []struct {
		name       string
		connectURL string
		want       string
		wantErr    bool
	}{
		{name: "https URL", connectURL: "https://gateway.test/v1/sessions/abc-123/game/connect", want: "abc-123"},
		{name: "wss URL with token", connectURL: "wss://gateway.test/v1/sessions/s1/game/connect?token=secret", want: "s1"},
		{name: "invalid path", connectURL: "wss://gateway.test/v1/other/s1/game/connect", wantErr: true},
		{name: "missing session", connectURL: "wss://gateway.test/v1/sessions//game/connect", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := ParseSessionURL(tt.connectURL)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ParseSessionURL(%q) expected error", tt.connectURL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseSessionURL(%q) unexpected error: %v", tt.connectURL, err)
			}
			if got != tt.want {
				t.Fatalf("ParseSessionURL(%q) = %q, want %q", tt.connectURL, got, tt.want)
			}
		})
	}
}

func TestCleanupOrder(t *testing.T) {
	// given
	order := newOrderRecorder()
	r := newTestRuntime()
	r.encoder = &fakeEncoder{order: order}
	r.inputMgr = &fakeInput{order: order}
	r.transport = &fakeTransport{order: order}
	r.connState = ConnConnected
	r.streamState = StreamStreaming

	// when
	if err := r.Disconnect(); err != nil {
		t.Fatalf("Disconnect() unexpected error: %v", err)
	}

	// then
	want := []string{"encoder.stop", "input.release_all", "input.stop", "transport.close"}
	if got := order.events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup order = %v, want %v", got, want)
	}
}

func TestMediaFlow(t *testing.T) {
	// given
	r := newTestRuntime()
	ft := &fakeTransport{}
	r.transport = ft
	r.session = &Session{ID: "session-1"}
	r.encoder = &fakeEncoder{stdout: bytes.NewReader(fmp4Stream())}

	// when
	if err := r.startMediaFlow(); err != nil {
		t.Fatalf("startMediaFlow() unexpected error: %v", err)
	}
	select {
	case err := <-r.mediaDone:
		if err != nil {
			t.Fatalf("media flow unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("media flow did not complete")
	}

	// then
	want := []string{"media_init", "media_segment:seg-0"}
	if got := ft.events; !reflect.DeepEqual(got, want) {
		t.Fatalf("transport events = %v, want %v", got, want)
	}
}

func TestControlFlow(t *testing.T) {
	// given
	r := newTestRuntime()
	ft := &fakeTransport{}
	fi := &fakeInput{}
	r.transport = ft
	r.inputMgr = fi
	r.session = &Session{ID: "session-1"}
	r.boundWindow = &window.WindowInfo{HWND: 100}
	req := &gw.GameControlRequest{
		OperationId: "op-1",
		Action: &gw.GameControlRequest_MouseClick{
			MouseClick: &gw.GameMouseClick{
				Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				X:      10,
				Y:      20,
			},
		},
	}

	// when
	if err := r.handleControlRequest(req); err != nil {
		t.Fatalf("handleControlRequest() unexpected error: %v", err)
	}

	// then
	wantEvents := []string{"control_ack:op-1", "control_result:op-1:GAME_CONTROL_RESULT_STATUS_SUCCEEDED"}
	if got := ft.events; !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("transport events = %v, want %v", got, wantEvents)
	}
	if fi.lastCommand.Action != input.ActionMouseClick || fi.lastCommand.HWND != 100 {
		t.Fatalf("executed command = %+v, want click on hwnd 100", fi.lastCommand)
	}
}

func TestReadLoopRouting(t *testing.T) {
	tests := []struct {
		name       string
		msg        transport.InboundMessage
		setup      func(*Runtime)
		wantEvents []string
		wantConn   ConnectionState
	}{
		{
			name: "control request sends ack and result",
			msg: transport.InboundMessage{ControlRequest: &gw.GameControlRequest{
				OperationId: "op-1",
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      10,
						Y:      20,
					},
				},
			}},
			setup: func(r *Runtime) {
				r.session = &Session{ID: "session-1"}
				r.boundWindow = &window.WindowInfo{HWND: 100}
			},
			wantEvents: []string{"control_ack:op-1", "control_result:op-1:GAME_CONTROL_RESULT_STATUS_SUCCEEDED"},
			wantConn:   ConnDisconnected,
		},
		{
			name:       "ping sends pong",
			msg:        transport.InboundMessage{Ping: &gw.GamePing{Nonce: "nonce-1"}},
			setup:      func(r *Runtime) { r.session = &Session{ID: "session-1"} },
			wantEvents: []string{"pong:nonce-1"},
			wantConn:   ConnDisconnected,
		},
		{
			name:     "gateway error sets runtime error state",
			msg:      transport.InboundMessage{Error: &gw.GameError{Code: "gateway_error", Message: "boom"}},
			wantConn: ConnDisconnected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			r := newTestRuntime()
			ft := &fakeTransport{readLoopMsgs: []transport.InboundMessage{tt.msg}}
			r.transport = ft
			if tt.setup != nil {
				tt.setup(r)
			}

			// when
			r.startReadLoopConsumer()

			// then
			waitForReadLoop(t, r, ft, tt.wantEvents, tt.wantConn)
		})
	}
}

func TestClearWindow(t *testing.T) {
	tests := []struct {
		name             string
		streaming        bool
		wantEncoderStops int
	}{
		{name: "bound window clears to connected"},
		{name: "streaming window stops capture before clearing", streaming: true, wantEncoderStops: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			r := newTestRuntime()
			fe := &fakeEncoder{stdout: bytes.NewReader(fmp4Stream())}
			r.encoder = fe
			if err := r.Connect(context.Background(), "wss://example.test/v1/sessions/session-1/game/connect"); err != nil {
				t.Fatalf("Connect() unexpected error: %v", err)
			}
			if err := r.BindWindow(100); err != nil {
				t.Fatalf("BindWindow() unexpected error: %v", err)
			}
			if tt.streaming {
				if err := r.StartCapture(context.Background()); err != nil {
					t.Fatalf("StartCapture() unexpected error: %v", err)
				}
			}

			// when
			err := r.ClearWindow()

			// then
			if err != nil {
				t.Fatalf("ClearWindow() unexpected error: %v", err)
			}
			if r.ConnectionState() != ConnConnected {
				t.Fatalf("ConnectionState() = %d, want %d", r.ConnectionState(), ConnConnected)
			}
			if r.boundWindow != nil {
				t.Fatalf("boundWindow = %+v, want nil", r.boundWindow)
			}
			if fe.stopCount != tt.wantEncoderStops {
				t.Fatalf("encoder stop count = %d, want %d", fe.stopCount, tt.wantEncoderStops)
			}
		})
	}
}

func TestNewRuntimeInitialization(t *testing.T) {
	// given
	ffmpegPath := "resources/bin/ffmpeg.exe"
	helperPath := "resources/bin/input-helper.exe"

	// when
	r := NewRuntime(ffmpegPath, helperPath)

	// then
	if r.encoder == nil {
		t.Fatalf("encoder is nil")
	}
	if r.inputMgr == nil {
		t.Fatalf("inputMgr is nil")
	}
	if r.ffmpegPath != ffmpegPath || r.helperPath != helperPath {
		t.Fatalf("paths = (%q, %q), want (%q, %q)", r.ffmpegPath, r.helperPath, ffmpegPath, helperPath)
	}
}

func TestInputHelperLifecycle(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Runtime) error
	}{
		{name: "stop capture stops helper", run: func(r *Runtime) error { return r.StopCapture() }},
		{name: "disconnect stops helper", run: func(r *Runtime) error { return r.Disconnect() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			r := newTestRuntime()
			r.helperPath = "resources/bin/input-helper.exe"
			fi := &fakeInput{}
			r.inputMgr = fi
			r.encoder = &fakeEncoder{stdout: bytes.NewReader(fmp4Stream())}
			if err := r.Connect(context.Background(), "wss://example.test/v1/sessions/session-1/game/connect"); err != nil {
				t.Fatalf("Connect() unexpected error: %v", err)
			}
			if err := r.BindWindow(100); err != nil {
				t.Fatalf("BindWindow() unexpected error: %v", err)
			}
			if err := r.StartCapture(context.Background()); err != nil {
				t.Fatalf("StartCapture() unexpected error: %v", err)
			}

			// when
			err := tt.run(r)

			// then
			if err != nil {
				t.Fatalf("lifecycle action unexpected error: %v", err)
			}
			if fi.startCount != 1 {
				t.Fatalf("input start count = %d, want 1", fi.startCount)
			}
			if fi.stopCount != 1 {
				t.Fatalf("input stop count = %d, want 1", fi.stopCount)
			}
			if fi.lastStartPath != r.helperPath {
				t.Fatalf("input start path = %q, want %q", fi.lastStartPath, r.helperPath)
			}
		})
	}
}

func newTestRuntime() *Runtime {
	r := NewRuntime("", "")
	r.transport = &fakeTransport{}
	r.windowMgr = &fakeWindowManager{windows: []window.WindowInfo{{HWND: 100, Title: "game", Rect: window.Rect{Right: 800, Bottom: 600}}}}
	r.inputMgr = &fakeInput{}
	return r
}

func waitForReadLoop(t *testing.T, r *Runtime, ft *fakeTransport, wantEvents []string, wantConn ConnectionState) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("read loop events = %v connState = %d, want events %v connState %d", ft.eventSnapshot(), r.ConnectionState(), wantEvents, wantConn)
		case <-tick.C:
			if reflect.DeepEqual(ft.eventSnapshot(), wantEvents) && r.ConnectionState() == wantConn {
				return
			}
		}
	}
}

type orderRecorder struct {
	mu  sync.Mutex
	log []string
}

func newOrderRecorder() *orderRecorder {
	return new(orderRecorder)
}

func (r *orderRecorder) add(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, event)
}

func (r *orderRecorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

type fakeTransport struct {
	order        *orderRecorder
	events       []string
	readLoopMsgs []transport.InboundMessage
	mu           sync.Mutex
}

func (f *fakeTransport) Connect(context.Context, string) error { return nil }
func (f *fakeTransport) Close() error {
	if f.order != nil {
		f.order.add("transport.close")
	}
	return nil
}
func (f *fakeTransport) SendHello(context.Context, string) error { return nil }
func (f *fakeTransport) SendMediaInit(context.Context, string, string, []byte) error {
	f.addEvent("media_init")
	return nil
}
func (f *fakeTransport) SendMediaSegment(_ context.Context, _ string, segmentID string, _ []byte, _ bool) error {
	f.addEvent("media_segment:" + segmentID)
	return nil
}
func (f *fakeTransport) SendControlAck(_ context.Context, _ string, operationID string) error {
	f.addEvent("control_ack:" + operationID)
	return nil
}
func (f *fakeTransport) SendControlResult(_ context.Context, _ string, operationID string, status gw.GameControlResultStatus) error {
	f.addEvent(fmt.Sprintf("control_result:%s:%s", operationID, status.String()))
	return nil
}
func (f *fakeTransport) SendPong(_ context.Context, _ string, nonce string) error {
	f.addEvent("pong:" + nonce)
	return nil
}
func (f *fakeTransport) ReadLoop(context.Context) (<-chan transport.InboundMessage, error) {
	ch := make(chan transport.InboundMessage)
	go func() {
		defer close(ch)
		for _, msg := range f.readLoopMsgs {
			ch <- msg
		}
	}()
	return ch, nil
}

func (f *fakeTransport) addEvent(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeTransport) eventSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

type fakeWindowManager struct {
	windows []window.WindowInfo
}

func (f *fakeWindowManager) EnumerateWindows() ([]window.WindowInfo, error) {
	return f.windows, nil
}

func (f *fakeWindowManager) IsWindowValid(hwnd uintptr) bool {
	for _, info := range f.windows {
		if info.HWND == hwnd {
			return true
		}
	}
	return false
}

type fakeInput struct {
	order         *orderRecorder
	lastCommand   input.Command
	lastStartPath string
	startCount    int
	stopCount     int
}

func (f *fakeInput) Start(helperPath string) error {
	f.lastStartPath = helperPath
	f.startCount++
	return nil
}
func (f *fakeInput) Stop() error {
	f.stopCount++
	if f.order != nil {
		f.order.add("input.stop")
	}
	return nil
}
func (f *fakeInput) ExecuteCommand(_ context.Context, cmd input.Command) (input.Response, error) {
	f.lastCommand = cmd
	return input.Response{Status: input.StatusOK}, nil
}
func (f *fakeInput) ReleaseAll() error {
	if f.order != nil {
		f.order.add("input.release_all")
	}
	return nil
}

type fakeEncoder struct {
	order     *orderRecorder
	stdout    io.Reader
	stopCount int
}

func (f *fakeEncoder) Start(context.Context, encoder.EncoderConfig) error { return nil }
func (f *fakeEncoder) StdoutPipe() io.Reader                              { return f.stdout }
func (f *fakeEncoder) Stop() error {
	f.stopCount++
	if f.order != nil {
		f.order.add("encoder.stop")
	}
	return nil
}
func (f *fakeEncoder) Wait() error { return nil }

func fmp4Stream() []byte {
	var data []byte
	data = append(data, mp4Box("ftyp", []byte("init"))...)
	data = append(data, mp4Box("moov", []byte("movie"))...)
	data = append(data, mp4Box("moof", []byte("frag"))...)
	data = append(data, mp4Box("mdat", []byte("media"))...)
	return data
}

func mp4Box(kind string, body []byte) []byte {
	size := uint32(8 + len(body))
	return append([]byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size), kind[0], kind[1], kind[2], kind[3]}, body...)
}

var _ TransportClient = (*fakeTransport)(nil)
var _ WindowEnumerator = (*fakeWindowManager)(nil)
var _ InputExecutor = (*fakeInput)(nil)
var _ MediaEncoder = (*fakeEncoder)(nil)
var _ MediaParser = media.ParseStreaming
