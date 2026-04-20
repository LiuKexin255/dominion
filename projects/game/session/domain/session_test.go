package domain

import (
	"errors"
	"testing"
)

func TestNewSession(t *testing.T) {
	tests := []struct {
		name        string
		sessionType SessionType
		sessionID   string
		wantErr     error
	}{
		{
			name:        "creates session in pending status",
			sessionType: TypeSaolei,
			sessionID:   "test-id-1",
			wantErr:     nil,
		},
		{
			name:        "generates UUID when sessionID is empty",
			sessionType: TypeSaolei,
			sessionID:   "",
			wantErr:     nil,
		},
		{
			name:        "uses provided sessionID when non-empty",
			sessionType: TypeSaolei,
			sessionID:   "my-custom-id",
			wantErr:     nil,
		},
		{
			name:        "returns ErrInvalidType for SessionTypeUnspecified",
			sessionType: SessionTypeUnspecified,
			sessionID:   "some-id",
			wantErr:     ErrInvalidType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := NewSession(tt.sessionType, tt.sessionID)

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewSession() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSession() unexpected error: %v", err)
			}
			if got.Snapshot().Status != StatusPending {
				t.Fatalf("Status = %v, want %v", got.Snapshot().Status, StatusPending)
			}
			if tt.sessionID != "" && got.Snapshot().ID != tt.sessionID {
				t.Fatalf("ID = %q, want %q", got.Snapshot().ID, tt.sessionID)
			}
			if tt.sessionID == "" && got.Snapshot().ID == "" {
				t.Fatal("ID is empty, expected auto-generated UUID")
			}
		})
	}
}

func TestMarkActive(t *testing.T) {
	tests := []struct {
		name          string
		setupStatus   SessionStatus
		wantErr       error
		wantReconnect int64
	}{
		{
			name:        "pending to active",
			setupStatus: StatusPending,
			wantErr:     nil,
		},
		{
			name:          "disconnected to active increments reconnectGeneration",
			setupStatus:   StatusDisconnected,
			wantErr:       nil,
			wantReconnect: 1,
		},
		{
			name:        "active to active returns ErrInvalidState",
			setupStatus: StatusActive,
			wantErr:     ErrInvalidState,
		},
		{
			name:        "ended to active returns ErrInvalidState",
			setupStatus: StatusEnded,
			wantErr:     ErrInvalidState,
		},
		{
			name:        "failed to active returns ErrInvalidState",
			setupStatus: StatusFailed,
			wantErr:     ErrInvalidState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			snap := SessionSnapshot{
				ID:     "test-id",
				Type:   TypeSaolei,
				Status: tt.setupStatus,
			}
			session, err := Rehydrate(snap)
			if err != nil {
				t.Fatalf("Rehydrate() error = %v", err)
			}

			// when
			err = session.MarkActive()

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("MarkActive() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("MarkActive() unexpected error: %v", err)
			}
			if session.Snapshot().Status != StatusActive {
				t.Fatalf("Status = %v, want %v", session.Snapshot().Status, StatusActive)
			}
			if session.Snapshot().ReconnectGeneration != tt.wantReconnect {
				t.Fatalf("ReconnectGeneration = %d, want %d", session.Snapshot().ReconnectGeneration, tt.wantReconnect)
			}
		})
	}
}

func TestMarkDisconnected(t *testing.T) {
	tests := []struct {
		name        string
		setupStatus SessionStatus
		wantErr     error
	}{
		{
			name:        "active to disconnected",
			setupStatus: StatusActive,
			wantErr:     nil,
		},
		{
			name:        "pending to disconnected returns ErrInvalidState",
			setupStatus: StatusPending,
			wantErr:     ErrInvalidState,
		},
		{
			name:        "disconnected to disconnected returns ErrInvalidState",
			setupStatus: StatusDisconnected,
			wantErr:     ErrInvalidState,
		},
		{
			name:        "ended to disconnected returns ErrInvalidState",
			setupStatus: StatusEnded,
			wantErr:     ErrInvalidState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			snap := SessionSnapshot{
				ID:     "test-id",
				Type:   TypeSaolei,
				Status: tt.setupStatus,
			}
			session, err := Rehydrate(snap)
			if err != nil {
				t.Fatalf("Rehydrate() error = %v", err)
			}

			// when
			err = session.MarkDisconnected()

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("MarkDisconnected() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("MarkDisconnected() unexpected error: %v", err)
			}
			if session.Snapshot().Status != StatusDisconnected {
				t.Fatalf("Status = %v, want %v", session.Snapshot().Status, StatusDisconnected)
			}
		})
	}
}

func TestMarkEnded(t *testing.T) {
	tests := []struct {
		name        string
		setupStatus SessionStatus
		wantErr     error
	}{
		{
			name:        "pending to ended",
			setupStatus: StatusPending,
			wantErr:     nil,
		},
		{
			name:        "active to ended",
			setupStatus: StatusActive,
			wantErr:     nil,
		},
		{
			name:        "disconnected to ended",
			setupStatus: StatusDisconnected,
			wantErr:     nil,
		},
		{
			name:        "ended to ended returns ErrInvalidState",
			setupStatus: StatusEnded,
			wantErr:     ErrInvalidState,
		},
		{
			name:        "failed to ended returns ErrInvalidState",
			setupStatus: StatusFailed,
			wantErr:     ErrInvalidState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			snap := SessionSnapshot{
				ID:     "test-id",
				Type:   TypeSaolei,
				Status: tt.setupStatus,
			}
			session, err := Rehydrate(snap)
			if err != nil {
				t.Fatalf("Rehydrate() error = %v", err)
			}

			// when
			err = session.MarkEnded()

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("MarkEnded() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("MarkEnded() unexpected error: %v", err)
			}
			if session.Snapshot().Status != StatusEnded {
				t.Fatalf("Status = %v, want %v", session.Snapshot().Status, StatusEnded)
			}
			if session.Snapshot().EndedAt == nil {
				t.Fatal("EndedAt is nil, want non-nil")
			}
		})
	}
}

func TestMarkFailed(t *testing.T) {
	tests := []struct {
		name        string
		setupStatus SessionStatus
		failErr     error
		wantErr     error
	}{
		{
			name:        "pending to failed sets lastError",
			setupStatus: StatusPending,
			failErr:     errors.New("connection refused"),
			wantErr:     nil,
		},
		{
			name:        "active to failed sets lastError",
			setupStatus: StatusActive,
			failErr:     errors.New("timeout"),
			wantErr:     nil,
		},
		{
			name:        "disconnected to failed sets lastError",
			setupStatus: StatusDisconnected,
			failErr:     errors.New("reconnect failed"),
			wantErr:     nil,
		},
		{
			name:        "ended to failed returns ErrInvalidState",
			setupStatus: StatusEnded,
			failErr:     errors.New("boom"),
			wantErr:     ErrInvalidState,
		},
		{
			name:        "failed to failed returns ErrInvalidState",
			setupStatus: StatusFailed,
			failErr:     errors.New("double fail"),
			wantErr:     ErrInvalidState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			snap := SessionSnapshot{
				ID:     "test-id",
				Type:   TypeSaolei,
				Status: tt.setupStatus,
			}
			session, err := Rehydrate(snap)
			if err != nil {
				t.Fatalf("Rehydrate() error = %v", err)
			}

			// when
			err = session.MarkFailed(tt.failErr)

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("MarkFailed() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("MarkFailed() unexpected error: %v", err)
			}
			if session.Snapshot().Status != StatusFailed {
				t.Fatalf("Status = %v, want %v", session.Snapshot().Status, StatusFailed)
			}
			if tt.failErr != nil && session.Snapshot().LastError != tt.failErr.Error() {
				t.Fatalf("LastError = %q, want %q", session.Snapshot().LastError, tt.failErr.Error())
			}
		})
	}
}

func TestEndedSessionCannotTransition(t *testing.T) {
	// given
	snap := SessionSnapshot{
		ID:     "test-id",
		Type:   TypeSaolei,
		Status: StatusEnded,
	}
	session, err := Rehydrate(snap)
	if err != nil {
		t.Fatalf("Rehydrate() error = %v", err)
	}

	// when / then
	if err := session.MarkActive(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("MarkActive() error = %v, want ErrInvalidState", err)
	}
	if err := session.MarkDisconnected(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("MarkDisconnected() error = %v, want ErrInvalidState", err)
	}
	if err := session.MarkEnded(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("MarkEnded() error = %v, want ErrInvalidState", err)
	}
	if err := session.MarkFailed(errors.New("x")); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("MarkFailed() error = %v, want ErrInvalidState", err)
	}
}

func TestSnapshotRehydrateRoundTrip(t *testing.T) {
	// given
	original, err := NewSession(TypeSaolei, "round-trip-id")
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := original.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	if err := original.MarkDisconnected(); err != nil {
		t.Fatalf("MarkDisconnected() error = %v", err)
	}
	if err := original.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}

	// when
	snap := original.Snapshot()
	rehydrated, err := Rehydrate(snap)
	if err != nil {
		t.Fatalf("Rehydrate() error = %v", err)
	}

	// then
	roundSnap := rehydrated.Snapshot()
	if roundSnap.ID != snap.ID {
		t.Fatalf("ID = %q, want %q", roundSnap.ID, snap.ID)
	}
	if roundSnap.Type != snap.Type {
		t.Fatalf("Type = %v, want %v", roundSnap.Type, snap.Type)
	}
	if roundSnap.Status != snap.Status {
		t.Fatalf("Status = %v, want %v", roundSnap.Status, snap.Status)
	}
	if roundSnap.ReconnectGeneration != snap.ReconnectGeneration {
		t.Fatalf("ReconnectGeneration = %d, want %d", roundSnap.ReconnectGeneration, snap.ReconnectGeneration)
	}
	if roundSnap.ReconnectGeneration != 1 {
		t.Fatalf("ReconnectGeneration = %d, want 1", roundSnap.ReconnectGeneration)
	}
}

func TestUUIDv4Uniqueness(t *testing.T) {
	ids := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		session, err := NewSession(TypeSaolei, "")
		if err != nil {
			t.Fatalf("NewSession() iteration %d error = %v", i, err)
		}
		id := session.Snapshot().ID
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate session ID generated: %q", id)
		}
		ids[id] = struct{}{}
	}
}
