package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dominion/projects/game/windows_agent/internal/sessionclient"
)

func newTestApp(rt runtimeService, rec *emitRecorder) *App {
	return &App{
		ctx:      context.Background(),
		rt:       rt,
		sc:       newSessionClient(func(*http.Request) (int, string) { return http.StatusOK, "{}" }),
		status:   AgentStatus{State: "Disconnected"},
		emitFunc: rec.emit,
	}
}

func TestConnect(t *testing.T) {
	tests := []struct {
		name       string
		connectErr error
		wantState  string
		wantErr    bool
	}{
		{name: "success", wantState: "Connected"},
		{name: "runtime error", connectErr: errors.New("connection refused"), wantState: "Error", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{
				connectFn: func(context.Context, string) error { return tt.connectErr },
			}, rec)

			// when
			err := a.Connect("wss://gateway.test/v1/sessions/s-1/game/connect?token=t")

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("Connect() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Connect() unexpected error: %v", err)
			}
			status := a.GetStatus()
			if status.State != tt.wantState {
				t.Fatalf("State = %q, want %q", status.State, tt.wantState)
			}
			if !tt.wantErr && status.SessionID != "s-1" {
				t.Fatalf("SessionID = %q, want %q", status.SessionID, "s-1")
			}
			if !tt.wantErr && status.ConnectedAt == "" {
				t.Fatalf("ConnectedAt is empty, expected ISO timestamp")
			}
			if rec.find(EventStatusChanged) == nil {
				t.Fatalf("expected %s event", EventStatusChanged)
			}
			if tt.wantErr && rec.find(EventErrorOccurred) == nil {
				t.Fatalf("expected %s event", EventErrorOccurred)
			}
		})
	}
}

func TestDisconnect(t *testing.T) {
	tests := []struct {
		name          string
		disconnectErr error
		wantErr       bool
	}{
		{name: "success"},
		{name: "runtime error", disconnectErr: errors.New("cleanup failed"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{
				disconnectFn: func() error { return tt.disconnectErr },
			}, rec)
			a.status = AgentStatus{
				State:       "Connected",
				SessionID:   "s-1",
				BoundWindow: &WindowDetail{WindowRef: WindowRef{HWND: 100, Title: "Game"}},
			}

			// when
			err := a.Disconnect()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("Disconnect() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Disconnect() unexpected error: %v", err)
			}
			status := a.GetStatus()
			if status.State != "Disconnected" {
				t.Fatalf("State = %q, want %q", status.State, "Disconnected")
			}
			if status.SessionID != "" {
				t.Fatalf("SessionID = %q, want empty", status.SessionID)
			}
			if status.BoundWindow != nil {
				t.Fatalf("BoundWindow = %+v, want nil", status.BoundWindow)
			}
			if status.ConnectedAt != "" {
				t.Fatalf("ConnectedAt = %q, want empty", status.ConnectedAt)
			}
			if rec.find(EventStatusChanged) == nil {
				t.Fatalf("expected %s event", EventStatusChanged)
			}
		})
	}
}

func TestGetStatus(t *testing.T) {
	// given
	want := AgentStatus{
		State:       "Connected",
		SessionID:   "s-42",
		ConnectedAt: "2025-01-01T00:00:00Z",
	}
	a := &App{
		status: want,
	}

	// when
	got := a.GetStatus()

	// then
	if got.State != want.State {
		t.Fatalf("State = %q, want %q", got.State, want.State)
	}
	if got.SessionID != want.SessionID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, want.SessionID)
	}
	if got.ConnectedAt != want.ConnectedAt {
		t.Fatalf("ConnectedAt = %q, want %q", got.ConnectedAt, want.ConnectedAt)
	}
}

func TestBindWindow(t *testing.T) {
	tests := []struct {
		name         string
		bindErr      error
		wantState    string
		wantErr      bool
		wantBoundHWD uintptr
	}{
		{name: "success", wantState: "Bound", wantBoundHWD: 100},
		{name: "runtime error", bindErr: errors.New("invalid hwnd"), wantState: "Error", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{
				bindWindowFn: func(uintptr) error { return tt.bindErr },
			}, rec)
			a.status = AgentStatus{State: "Connected"}

			// when
			err := a.BindWindow(tt.wantBoundHWD)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("BindWindow() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("BindWindow() unexpected error: %v", err)
			}
			status := a.GetStatus()
			if status.State != tt.wantState {
				t.Fatalf("State = %q, want %q", status.State, tt.wantState)
			}
			if !tt.wantErr {
				if status.BoundWindow == nil {
					t.Fatalf("BoundWindow is nil, expected window ref")
				}
				if status.BoundWindow.HWND != tt.wantBoundHWD {
					t.Fatalf("BoundWindow.HWND = %d, want %d", status.BoundWindow.HWND, tt.wantBoundHWD)
				}
			}
			if rec.find(EventStatusChanged) == nil {
				t.Fatalf("expected %s event", EventStatusChanged)
			}
		})
	}
}

func TestStatusTransitions(t *testing.T) {
	// given
	rec := newEmitRecorder()
	a := newTestApp(&mockRuntime{}, rec)

	// when: connect
	if err := a.Connect("wss://gateway.test/v1/sessions/s-99/game/connect?token=t"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}

	// then: state is Connected
	if s := a.GetStatus(); s.State != "Connected" {
		t.Fatalf("after Connect: State = %q, want %q", s.State, "Connected")
	}
	if s := a.GetStatus(); s.SessionID != "s-99" {
		t.Fatalf("after Connect: SessionID = %q, want %q", s.SessionID, "s-99")
	}

	// when: bind window
	if err := a.BindWindow(200); err != nil {
		t.Fatalf("BindWindow() unexpected error: %v", err)
	}

	// then: state is Bound with window ref
	if s := a.GetStatus(); s.State != "Bound" {
		t.Fatalf("after BindWindow: State = %q, want %q", s.State, "Bound")
	}
	if s := a.GetStatus(); s.BoundWindow == nil || s.BoundWindow.HWND != 200 {
		t.Fatalf("after BindWindow: BoundWindow.HWND = %d, want 200", s.BoundWindow.HWND)
	}

	// when: disconnect
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() unexpected error: %v", err)
	}

	// then: state is Disconnected with cleared fields
	s := a.GetStatus()
	if s.State != "Disconnected" {
		t.Fatalf("after Disconnect: State = %q, want %q", s.State, "Disconnected")
	}
	if s.SessionID != "" {
		t.Fatalf("after Disconnect: SessionID = %q, want empty", s.SessionID)
	}
	if s.BoundWindow != nil {
		t.Fatalf("after Disconnect: BoundWindow = %+v, want nil", s.BoundWindow)
	}
	if s.ConnectedAt != "" {
		t.Fatalf("after Disconnect: ConnectedAt = %q, want empty", s.ConnectedAt)
	}
}

func TestEventEmission(t *testing.T) {
	// given
	rec := newEmitRecorder()
	a := newTestApp(&mockRuntime{}, rec)

	// when: connect
	_ = a.Connect("wss://gateway.test/v1/sessions/s-1/game/connect")

	// then: status:changed event emitted with status data
	evt := rec.find(EventStatusChanged)
	if evt == nil {
		t.Fatalf("expected %s event after Connect", EventStatusChanged)
	}
	statusData, ok := evt.data.(AgentStatus)
	if !ok {
		t.Fatalf("event data type = %T, want AgentStatus", evt.data)
	}
	if statusData.State != "Connected" {
		t.Fatalf("event data State = %q, want %q", statusData.State, "Connected")
	}

	// when: connect error
	rec.reset()
	a2 := newTestApp(&mockRuntime{
		connectFn: func(context.Context, string) error { return errors.New("fail") },
	}, rec)
	_ = a2.Connect("wss://gateway.test/v1/sessions/s-2/game/connect")

	// then: error:occurred event emitted
	if rec.find(EventErrorOccurred) == nil {
		t.Fatalf("expected %s event after connect error", EventErrorOccurred)
	}
}

func TestEmitLog(t *testing.T) {
	// given
	rec := newEmitRecorder()
	a := &App{
		ctx:      context.Background(),
		emitFunc: rec.emit,
	}

	// when
	a.EmitLog("info", "agent started")

	// then
	evt := rec.find(EventLogEntry)
	if evt == nil {
		t.Fatalf("expected %s event", EventLogEntry)
	}
	data, ok := evt.data.(map[string]string)
	if !ok {
		t.Fatalf("event data type = %T, want map[string]string", evt.data)
	}
	if data["level"] != "info" {
		t.Fatalf("level = %q, want %q", data["level"], "info")
	}
	if data["message"] != "agent started" {
		t.Fatalf("message = %q, want %q", data["message"], "agent started")
	}
}

func TestListSessions(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantLen    int
		wantErr    bool
	}{
		{name: "success", statusCode: http.StatusOK, body: `{"sessions":[{"name":"sessions/s-1","type":"SESSION_TYPE_SAOLEI","status":"SESSION_STATUS_ACTIVE","gatewayId":"gw-1","agentConnectUrl":"wss://gateway.test/v1/sessions/s-1/game/connect?token=secret","reconnectGeneration":"0"}]}`, wantLen: 1},
		{name: "service error", statusCode: http.StatusInternalServerError, body: `failed`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{}, rec)
			a.sc = newSessionClient(func(r *http.Request) (int, string) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/sessions" {
					return http.StatusNotFound, r.URL.Path
				}
				return tt.statusCode, tt.body
			})

			// when
			got, err := a.ListSessions()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ListSessions() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ListSessions() unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("len(ListSessions()) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCreateSession(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "success", statusCode: http.StatusOK, body: `{"session":{"name":"sessions/s-1","type":"SESSION_TYPE_SAOLEI","gatewayId":"gw-1","agentConnectUrl":"wss://gateway.test/v1/sessions/s-1/game/connect?token=secret","reconnectGeneration":"0"}}`},
		{name: "service error", statusCode: http.StatusBadRequest, body: `bad type`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{}, rec)
			a.sc = newSessionClient(func(r *http.Request) (int, string) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions" {
					return http.StatusNotFound, r.URL.Path
				}
				return tt.statusCode, tt.body
			})

			// when
			got, err := a.CreateSession("desktop")

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("CreateSession() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CreateSession() unexpected error: %v", err)
			}
			if !tt.wantErr && (got == nil || got.Name != "sessions/s-1") {
				t.Fatalf("CreateSession() = %+v, want sessions/s-1", got)
			}
		})
	}
}

func TestConnectSession(t *testing.T) {
	tests := []struct {
		name          string
		connectErrs   []error
		reconnectCode int
		reconnectBody string
		wantCalls     int
		wantErr       bool
	}{
		{name: "success", wantCalls: 1},
		{name: "reconnect flow", connectErrs: []error{errors.New("dial failed"), nil}, reconnectCode: http.StatusOK, reconnectBody: `{"session":{"name":"sessions/s-1","type":"SESSION_TYPE_SAOLEI","gatewayId":"gw-2","agentConnectUrl":"wss://gateway-2.test/v1/sessions/s-1/game/connect?token=new","reconnectGeneration":"1"}}`, wantCalls: 2},
		{name: "reconnect error", connectErrs: []error{errors.New("dial failed")}, reconnectCode: http.StatusInternalServerError, reconnectBody: `failed`, wantCalls: 1, wantErr: true},
		{name: "retry error", connectErrs: []error{errors.New("dial failed"), errors.New("retry failed")}, reconnectCode: http.StatusOK, reconnectBody: `{"session":{"name":"sessions/s-1","type":"SESSION_TYPE_SAOLEI","gatewayId":"gw-2","agentConnectUrl":"wss://gateway-2.test/v1/sessions/s-1/game/connect?token=new","reconnectGeneration":"1"}}`, wantCalls: 2, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			connectCalls := 0
			a := newTestApp(&mockRuntime{
				connectFn: func(context.Context, string) error {
					var err error
					if connectCalls < len(tt.connectErrs) {
						err = tt.connectErrs[connectCalls]
					}
					connectCalls++
					return err
				},
			}, rec)
			a.sc = newSessionClient(func(r *http.Request) (int, string) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/s-1:reconnect" {
					return http.StatusNotFound, r.URL.Path
				}
				return tt.reconnectCode, tt.reconnectBody
			})
			session := Session{Name: "sessions/s-1", Type: "desktop", GatewayID: "gw-1", AgentConnectURL: "wss://gateway.test/v1/sessions/s-1/game/connect?token=old"}

			// when
			err := a.ConnectSession(session)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ConnectSession() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ConnectSession() unexpected error: %v", err)
			}
			if connectCalls != tt.wantCalls {
				t.Fatalf("connect calls = %d, want %d", connectCalls, tt.wantCalls)
			}
			if !tt.wantErr && a.GetStatus().SessionName != "sessions/s-1" {
				t.Fatalf("SessionName = %q, want sessions/s-1", a.GetStatus().SessionName)
			}
		})
	}
}

func TestDeleteSession(t *testing.T) {
	tests := []struct {
		name          string
		current       bool
		state         string
		deleteStatus  int
		wantStopCalls int
		wantDiscCalls int
		wantErr       bool
	}{
		{name: "delete other session", deleteStatus: http.StatusOK},
		{name: "delete current connected", current: true, state: "Connected", deleteStatus: http.StatusOK, wantDiscCalls: 1},
		{name: "delete current streaming", current: true, state: "Streaming", deleteStatus: http.StatusOK, wantStopCalls: 1, wantDiscCalls: 1},
		{name: "delete error", deleteStatus: http.StatusInternalServerError, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			stopCalls := 0
			disconnectCalls := 0
			a := newTestApp(&mockRuntime{
				stopCaptureFn: func() error { stopCalls++; return nil },
				disconnectFn:  func() error { disconnectCalls++; return nil },
			}, rec)
			a.status.State = tt.state
			if tt.current {
				a.currentSession = &Session{Name: "sessions/s-1"}
			}
			a.sc = newSessionClient(func(r *http.Request) (int, string) {
				if r.Method != http.MethodDelete || r.URL.Path != "/v1/sessions/s-1" {
					return http.StatusNotFound, r.URL.Path
				}
				return tt.deleteStatus, `deleted`
			})

			// when
			err := a.DeleteSession("sessions/s-1")

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("DeleteSession() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("DeleteSession() unexpected error: %v", err)
			}
			if stopCalls != tt.wantStopCalls {
				t.Fatalf("stop calls = %d, want %d", stopCalls, tt.wantStopCalls)
			}
			if disconnectCalls != tt.wantDiscCalls {
				t.Fatalf("disconnect calls = %d, want %d", disconnectCalls, tt.wantDiscCalls)
			}
		})
	}
}

func TestClearWindow(t *testing.T) {
	tests := []struct {
		name     string
		clearErr error
		wantErr  bool
	}{
		{name: "success"},
		{name: "runtime error", clearErr: errors.New("not bound"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{clearWindowFn: func() error { return tt.clearErr }}, rec)
			a.status = AgentStatus{State: "Bound", BoundWindow: &WindowDetail{WindowRef: WindowRef{HWND: 100}}}

			// when
			err := a.ClearWindow()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ClearWindow() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ClearWindow() unexpected error: %v", err)
			}
			if !tt.wantErr && a.GetStatus().BoundWindow != nil {
				t.Fatalf("BoundWindow = %+v, want nil", a.GetStatus().BoundWindow)
			}
		})
	}
}

func TestStartCapture(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		window     *WindowDetail
		startErr   error
		wantCalled bool
		wantErr    bool
	}{
		{name: "success", state: "Bound", window: &WindowDetail{WindowRef: WindowRef{HWND: 100}}, wantCalled: true},
		{name: "missing window", state: "Bound", wantErr: true},
		{name: "wrong state", state: "Disconnected", window: &WindowDetail{WindowRef: WindowRef{HWND: 100}}, wantErr: true},
		{name: "runtime error", state: "Bound", window: &WindowDetail{WindowRef: WindowRef{HWND: 100}}, startErr: errors.New("ffmpeg failed"), wantCalled: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			called := false
			a := newTestApp(&mockRuntime{startCaptureFn: func(context.Context) error { called = true; return tt.startErr }}, rec)
			a.status = AgentStatus{State: tt.state, BoundWindow: tt.window}

			// when
			err := a.StartCapture()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("StartCapture() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("StartCapture() unexpected error: %v", err)
			}
			if called != tt.wantCalled {
				t.Fatalf("runtime called = %t, want %t", called, tt.wantCalled)
			}
			if !tt.wantErr && a.GetStatus().State != "Streaming" {
				t.Fatalf("State = %q, want Streaming", a.GetStatus().State)
			}
		})
	}
}

func TestStopCapture(t *testing.T) {
	tests := []struct {
		name    string
		stopErr error
		wantErr bool
	}{
		{name: "success"},
		{name: "runtime error", stopErr: errors.New("stop failed"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rec := newEmitRecorder()
			a := newTestApp(&mockRuntime{stopCaptureFn: func() error { return tt.stopErr }}, rec)
			a.status = AgentStatus{State: "Streaming", StreamingStartedAt: "2026-05-01T00:00:00Z"}

			// when
			err := a.StopCapture()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("StopCapture() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("StopCapture() unexpected error: %v", err)
			}
			if !tt.wantErr && a.GetStatus().State != "Bound" {
				t.Fatalf("State = %q, want Bound", a.GetStatus().State)
			}
		})
	}
}

func TestTakeScreenshot(t *testing.T) {
	tests := []struct {
		name       string
		session    *Session
		statusCode int
		body       string
		wantURL    string
		wantErr    bool
	}{
		{name: "success", session: &Session{Name: "sessions/s-1", GatewayID: "gw-1"}, statusCode: http.StatusOK, body: `{"snapshot_id":"snap-1","mime_type":"image/png","image":"aW1n","capture_time":"now"}`, wantURL: "data:image/png;base64,aW1n"},
		{name: "no active session", wantErr: true},
		{name: "gateway error", session: &Session{Name: "sessions/s-1", GatewayID: "gw-1"}, statusCode: http.StatusBadGateway, body: `bad gateway`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/sessions/s-1/game/snapshot" {
					t.Fatalf("path = %q, want snapshot path", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			origClient := http.DefaultClient
			http.DefaultClient = server.Client()
			defer func() { http.DefaultClient = origClient }()
			a := newTestApp(&mockRuntime{}, newEmitRecorder())
			if tt.session != nil {
				session := *tt.session
				session.AgentConnectURL = server.URL + "/v1/sessions/s-1/game/connect?token=secret"
				a.currentSession = &session
			}

			// when
			got, err := a.TakeScreenshot()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("TakeScreenshot() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("TakeScreenshot() unexpected error: %v", err)
			}
			if !tt.wantErr && got.ImageURL != tt.wantURL {
				t.Fatalf("ImageURL = %q, want %q", got.ImageURL, tt.wantURL)
			}
		})
	}
}

func TestStructuredLogSanitizesToken(t *testing.T) {
	// given
	rec := newEmitRecorder()
	a := newTestApp(&mockRuntime{}, rec)

	// when
	if err := a.Connect("wss://gateway.test/v1/sessions/s-1/game/connect?token=secret"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}

	// then
	for _, evt := range rec.all(EventLogEntry) {
		entry, ok := evt.data.(LogEntry)
		if !ok {
			continue
		}
		for _, value := range entry.Fields {
			if strings.Contains(value, "secret") || strings.Contains(value, "token=") {
				t.Fatalf("log field contains token: %q", value)
			}
		}
	}
}

// mockRuntime implements runtimeService for testing.
type mockRuntime struct {
	connectFn      func(context.Context, string) error
	disconnectFn   func() error
	bindWindowFn   func(uintptr) error
	startCaptureFn func(context.Context) error
	stopCaptureFn  func() error
	clearWindowFn  func() error
}

func (m *mockRuntime) Connect(ctx context.Context, url string) error {
	if m.connectFn != nil {
		return m.connectFn(ctx, url)
	}
	return nil
}

func (m *mockRuntime) Disconnect() error {
	if m.disconnectFn != nil {
		return m.disconnectFn()
	}
	return nil
}

func (m *mockRuntime) BindWindow(hwnd uintptr) error {
	if m.bindWindowFn != nil {
		return m.bindWindowFn(hwnd)
	}
	return nil
}

func (m *mockRuntime) StartCapture(ctx context.Context) error {
	if m.startCaptureFn != nil {
		return m.startCaptureFn(ctx)
	}
	return nil
}

func (m *mockRuntime) StopCapture() error {
	if m.stopCaptureFn != nil {
		return m.stopCaptureFn()
	}
	return nil
}

func (m *mockRuntime) ClearWindow() error {
	if m.clearWindowFn != nil {
		return m.clearWindowFn()
	}
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newSessionClient(handler func(*http.Request) (int, string)) *sessionclient.Client {
	return sessionclient.NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		statusCode, body := handler(r)
		return &http.Response{
			StatusCode: statusCode,
			Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})})
}

// emitRecord stores a single emitted event for verification.
type emitRecord struct {
	name string
	data interface{}
}

// emitRecorder captures emitted events for test assertions.
type emitRecorder struct {
	mu     sync.Mutex
	events []emitRecord
}

func newEmitRecorder() *emitRecorder {
	return new(emitRecorder)
}

func (r *emitRecorder) emit(_ context.Context, name string, data ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var d interface{}
	if len(data) > 0 {
		d = data[0]
	}
	r.events = append(r.events, emitRecord{name: name, data: d})
}

// find returns the first event matching the given name, or nil.
func (r *emitRecorder) find(name string) *emitRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.events {
		if r.events[i].name == name {
			return &r.events[i]
		}
	}
	return nil
}

func (r *emitRecorder) all(name string) []emitRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	var events []emitRecord
	for _, event := range r.events {
		if event.name == name {
			events = append(events, event)
		}
	}
	return events
}

func (r *emitRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}

var _ runtimeService = (*mockRuntime)(nil)
