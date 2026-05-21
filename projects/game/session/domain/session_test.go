package domain

import (
	"errors"
	"testing"
	"time"
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
			if got.Status() != StatusPending {
				t.Fatalf("Status = %v, want %v", got.Status(), StatusPending)
			}
			if tt.sessionID != "" && got.ID() != tt.sessionID {
				t.Fatalf("ID = %q, want %q", got.ID(), tt.sessionID)
			}
			if tt.sessionID == "" && got.ID() == "" {
				t.Fatal("ID is empty, expected auto-generated UUID")
			}
		})
	}
}

func TestMarkActive(t *testing.T) {
	tests := []struct {
		name        string
		setupStatus SessionStatus
		wantErr     error
	}{
		{
			name:        "pending to active",
			setupStatus: StatusPending,
			wantErr:     nil,
		},
		{
			name:        "disconnected to active does not increment reconnectGeneration",
			setupStatus: StatusDisconnected,
			wantErr:     nil,
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
			if session.Status() != StatusActive {
				t.Fatalf("Status = %v, want %v", session.Status(), StatusActive)
			}
			if session.ReconnectGeneration() != 0 {
				t.Fatalf("ReconnectGeneration = %d, want 0 (gateway controls this)", session.ReconnectGeneration())
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
			if session.Status() != StatusDisconnected {
				t.Fatalf("Status = %v, want %v", session.Status(), StatusDisconnected)
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
			if session.Status() != StatusEnded {
				t.Fatalf("Status = %v, want %v", session.Status(), StatusEnded)
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
			if session.Status() != StatusFailed {
				t.Fatalf("Status = %v, want %v", session.Status(), StatusFailed)
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

func TestSessionGetters(t *testing.T) {
	session, err := NewSession(TypeSaolei, "getter-test-id")
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetGatewayID("gw-1")
	session.SetToken("token-xyz")
	session.SetReconnectGeneration(3)

	if session.ID() != "getter-test-id" {
		t.Fatalf("ID() = %q, want %q", session.ID(), "getter-test-id")
	}
	if session.Token() != "token-xyz" {
		t.Fatalf("Token() = %q, want %q", session.Token(), "token-xyz")
	}
	if session.GatewayID() != "gw-1" {
		t.Fatalf("GatewayID() = %q, want %q", session.GatewayID(), "gw-1")
	}
	if session.Status() != StatusPending {
		t.Fatalf("Status() = %v, want %v", session.Status(), StatusPending)
	}
	if session.ReconnectGeneration() != 3 {
		t.Fatalf("ReconnectGeneration() = %d, want %d", session.ReconnectGeneration(), 3)
	}
}

func TestSnapshotRehydrateRoundTrip(t *testing.T) {
	original, err := NewSession(TypeSaolei, "round-trip-id")
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	original.SetGatewayID("gw-1")
	original.SetToken("round-trip-token")
	original.SetReconnectGeneration(2)
	if err := original.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	if err := original.MarkDisconnected(); err != nil {
		t.Fatalf("MarkDisconnected() error = %v", err)
	}
	if err := original.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}

	snap := original.Snapshot()
	rehydrated, err := Rehydrate(snap)
	if err != nil {
		t.Fatalf("Rehydrate() error = %v", err)
	}

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
	if roundSnap.Token != "round-trip-token" {
		t.Fatalf("Token = %q, want %q", roundSnap.Token, "round-trip-token")
	}
}

func Test_Rehydrate(t *testing.T) {
	now := time.Now().UTC()
	snapshot := SessionSnapshot{
		ID:                  "test-id",
		Type:                TypeSaolei,
		Status:              StatusActive,
		GatewayID:           "gw-1",
		Token:               "rehydrate-token",
		CreatedAt:           now,
		UpdatedAt:           now,
		EndedAt:             nil,
		ReconnectGeneration: 2,
		LastError:           "previous error",
	}

	session, err := Rehydrate(snapshot)
	if err != nil {
		t.Fatalf("Rehydrate() error = %v", err)
	}

	got := session.Snapshot()
	if got.ID != snapshot.ID {
		t.Fatalf("ID = %q, want %q", got.ID, snapshot.ID)
	}
	if got.Type != snapshot.Type {
		t.Fatalf("Type = %v, want %v", got.Type, snapshot.Type)
	}
	if got.Status != snapshot.Status {
		t.Fatalf("Status = %v, want %v", got.Status, snapshot.Status)
	}
	if got.GatewayID != snapshot.GatewayID {
		t.Fatalf("GatewayID = %q, want %q", got.GatewayID, snapshot.GatewayID)
	}
	if got.Token != snapshot.Token {
		t.Fatalf("Token = %q, want %q", got.Token, snapshot.Token)
	}
	if got.ReconnectGeneration != snapshot.ReconnectGeneration {
		t.Fatalf("ReconnectGeneration = %d, want %d", got.ReconnectGeneration, snapshot.ReconnectGeneration)
	}
	if got.LastError != snapshot.LastError {
		t.Fatalf("LastError = %q, want %q", got.LastError, snapshot.LastError)
	}
	if got.EndedAt != nil {
		t.Fatalf("EndedAt = %v, want nil", got.EndedAt)
	}
}

func TestUUIDv4Uniqueness(t *testing.T) {
	ids := make(map[string]struct{}, 100)

	for i := range 100 {
		session, err := NewSession(TypeSaolei, "")
		if err != nil {
			t.Fatalf("NewSession() iteration %d error = %v", i, err)
		}
		id := session.ID()
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate session ID generated: %q", id)
		}
		ids[id] = struct{}{}
	}
}
