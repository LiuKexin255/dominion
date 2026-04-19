// Package domain contains the game session domain model.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// SessionType describes the kind of game session.
type SessionType int32

const (
	// SessionTypeUnspecified is the zero value.
	SessionTypeUnspecified SessionType = 0
	// TypeSaolei matches proto SESSION_TYPE_SAOLEI.
	TypeSaolei SessionType = 1
)

// SessionStatus describes the lifecycle state of a session.
type SessionStatus int32

const (
	// StatusUnspecified is the zero value.
	StatusUnspecified SessionStatus = 0
	// StatusPending indicates the session has been created but not activated.
	StatusPending SessionStatus = 1
	// StatusActive indicates the session is actively connected.
	StatusActive SessionStatus = 2
	// StatusDisconnected indicates the session lost its connection and may reconnect.
	StatusDisconnected SessionStatus = 3
	// StatusEnded indicates the session finished normally.
	StatusEnded SessionStatus = 4
	// StatusFailed indicates the session failed.
	StatusFailed SessionStatus = 5
)

// SessionSnapshot captures the persisted session state.
type SessionSnapshot struct {
	ID                  string
	Type                SessionType
	Status              SessionStatus
	GatewayID           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	EndedAt             *time.Time
	ReconnectGeneration int64
	LastError           string
}

// Session is the aggregate root for a game session.
type Session struct {
	id                  string
	sessionType         SessionType
	status              SessionStatus
	gatewayID           string
	createdAt           time.Time
	updatedAt           time.Time
	endedAt             *time.Time
	reconnectGeneration int64
	lastError           string
}

// NewSession constructs a session in the pending state.
// If sessionID is empty, a UUID v4 is generated with crypto/rand.
func NewSession(sessionType SessionType, sessionID string) (*Session, error) {
	if sessionType == SessionTypeUnspecified {
		return nil, ErrInvalidType
	}

	if sessionID == "" {
		var err error
		sessionID, err = generateUUIDv4()
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	return &Session{
		id:          sessionID,
		sessionType: sessionType,
		status:      StatusPending,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// Rehydrate reconstructs a session from persisted state.
func Rehydrate(snapshot SessionSnapshot) (*Session, error) {
	if snapshot.Type == SessionTypeUnspecified {
		return nil, ErrInvalidType
	}
	if !isValidSessionStatus(snapshot.Status) {
		return nil, ErrInvalidState
	}

	return &Session{
		id:                  snapshot.ID,
		sessionType:         snapshot.Type,
		status:              snapshot.Status,
		gatewayID:           snapshot.GatewayID,
		createdAt:           snapshot.CreatedAt,
		updatedAt:           snapshot.UpdatedAt,
		endedAt:             cloneTimePtr(snapshot.EndedAt),
		reconnectGeneration: snapshot.ReconnectGeneration,
		lastError:           snapshot.LastError,
	}, nil
}

// Snapshot returns a read-only copy of the session state.
func (s *Session) Snapshot() SessionSnapshot {
	return SessionSnapshot{
		ID:                  s.id,
		Type:                s.sessionType,
		Status:              s.status,
		GatewayID:           s.gatewayID,
		CreatedAt:           s.createdAt,
		UpdatedAt:           s.updatedAt,
		EndedAt:             cloneTimePtr(s.endedAt),
		ReconnectGeneration: s.reconnectGeneration,
		LastError:           s.lastError,
	}
}

// SetGatewayID sets the assigned gateway for the session.
func (s *Session) SetGatewayID(id string) {
	s.gatewayID = id
	s.updatedAt = time.Now().UTC()
}

// MarkActive transitions the session to active.
func (s *Session) MarkActive() error {
	switch s.status {
	case StatusPending:
	case StatusDisconnected:
		s.reconnectGeneration++
	default:
		return ErrInvalidState
	}

	return s.transitionTo(StatusActive)
}

// MarkDisconnected transitions the session to disconnected.
func (s *Session) MarkDisconnected() error {
	if s.status != StatusActive {
		return ErrInvalidState
	}

	return s.transitionTo(StatusDisconnected)
}

// MarkEnded transitions the session to ended.
func (s *Session) MarkEnded() error {
	switch s.status {
	case StatusPending, StatusActive, StatusDisconnected:
		return s.transitionTo(StatusEnded)
	default:
		return ErrInvalidState
	}
}

// MarkFailed transitions the session to failed.
func (s *Session) MarkFailed(err error) error {
	switch s.status {
	case StatusPending, StatusActive, StatusDisconnected:
		if err != nil {
			s.lastError = err.Error()
		} else {
			s.lastError = ""
		}
		return s.transitionTo(StatusFailed)
	default:
		return ErrInvalidState
	}
}

func (s *Session) transitionTo(next SessionStatus) error {
	if !canTransition(s.status, next) {
		return ErrInvalidState
	}

	now := time.Now().UTC()
	s.status = next
	s.updatedAt = now
	if next != StatusFailed {
		s.lastError = ""
	}
	if next == StatusEnded {
		s.endedAt = &now
	}
	return nil
}

func canTransition(current, next SessionStatus) bool {
	switch current {
	case StatusPending:
		return next == StatusActive || next == StatusEnded || next == StatusFailed
	case StatusActive:
		return next == StatusDisconnected || next == StatusEnded || next == StatusFailed
	case StatusDisconnected:
		return next == StatusActive || next == StatusEnded || next == StatusFailed
	default:
		return false
	}
}

func isValidSessionStatus(status SessionStatus) bool {
	switch status {
	case StatusPending, StatusActive, StatusDisconnected, StatusEnded, StatusFailed:
		return true
	default:
		return false
	}
}

func cloneTimePtr(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}

	cloned := *ts
	return &cloned
}

func generateUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], b[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], b[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], b[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], b[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], b[10:16])

	return string(encoded), nil
}
