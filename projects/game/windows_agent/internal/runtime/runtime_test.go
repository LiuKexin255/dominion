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
	if r.State() != StateConnected {
		t.Fatalf("State() = %d, want %d", r.State(), StateConnected)
	}
	if err := r.BindWindow(100); err != nil {
		t.Fatalf("BindWindow() unexpected error: %v", err)
	}
	if r.State() != StateBound {
		t.Fatalf("State() = %d, want %d", r.State(), StateBound)
	}
	if err := r.StartCapture(context.Background()); err != nil {
		t.Fatalf("StartCapture() unexpected error: %v", err)
	}

	// then
	if r.State() != StateStreaming {
		t.Fatalf("State() = %d, want %d", r.State(), StateStreaming)
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
	if r.State() != StateDisconnected {
		t.Fatalf("State() = %d, want %d", r.State(), StateDisconnected)
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
	r.state = StateStreaming

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
		Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
		Mouse: &gw.GameMouseAction{
			Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
			X:      10,
			Y:      20,
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

func newTestRuntime() *Runtime {
	r := NewRuntime()
	r.transport = &fakeTransport{}
	r.windowMgr = &fakeWindowManager{windows: []window.WindowInfo{{HWND: 100, Title: "game", Rect: window.Rect{Right: 800, Bottom: 600}}}}
	r.inputMgr = &fakeInput{}
	return r
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
	order  *orderRecorder
	events []string
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
	f.events = append(f.events, "media_init")
	return nil
}
func (f *fakeTransport) SendMediaSegment(_ context.Context, _ string, segmentID string, _ []byte, _ bool) error {
	f.events = append(f.events, "media_segment:"+segmentID)
	return nil
}
func (f *fakeTransport) SendControlAck(_ context.Context, _ string, operationID string) error {
	f.events = append(f.events, "control_ack:"+operationID)
	return nil
}
func (f *fakeTransport) SendControlResult(_ context.Context, _ string, operationID string, status gw.GameControlResultStatus) error {
	f.events = append(f.events, fmt.Sprintf("control_result:%s:%s", operationID, status.String()))
	return nil
}
func (f *fakeTransport) SendPong(context.Context, string, string) error { return nil }
func (f *fakeTransport) ReadLoop(context.Context) (<-chan transport.InboundMessage, error) {
	ch := make(chan transport.InboundMessage)
	close(ch)
	return ch, nil
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
	order       *orderRecorder
	lastCommand input.Command
}

func (f *fakeInput) Start(string) error { return nil }
func (f *fakeInput) Stop() error {
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
	order  *orderRecorder
	stdout io.Reader
}

func (f *fakeEncoder) Start(context.Context, encoder.EncoderConfig) error { return nil }
func (f *fakeEncoder) StdoutPipe() io.Reader                              { return f.stdout }
func (f *fakeEncoder) Stop() error {
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
var _ MediaParser = media.Parse
