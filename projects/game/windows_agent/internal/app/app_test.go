package app

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func newTestApp(rt runtimeService, rec *emitRecorder) *App {
	return &App{
		ctx:      context.Background(),
		rt:       rt,
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
				State:     "Connected",
				SessionID: "s-1",
				BoundWindow: &WindowRef{
					HWND:  100,
					Title: "Game",
				},
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

// mockRuntime implements runtimeService for testing.
type mockRuntime struct {
	connectFn    func(context.Context, string) error
	disconnectFn func() error
	bindWindowFn func(uintptr) error
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

func (r *emitRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}

var _ runtimeService = (*mockRuntime)(nil)
