// Package mongo provides the MongoDB-backed SessionRepository implementation.
package mongo

import (
	"time"

	gameconst "dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/session/domain"
)

// sessionDocument stores Session documents in MongoDB.
// Name is NOT stored in BSON; it is synthesized from SessionID on read.
type sessionDocument struct {
	SessionID  string    `bson:"session_id"`
	CreateTime time.Time `bson:"create_time"`
}

// toDomain converts a MongoDB document into its domain representation,
// generating the Name field from SessionID.
func (d *sessionDocument) toDomain() *domain.Session {
	if d == nil {
		return nil
	}

	return &domain.Session{
		Name:       gameconst.SessionName(d.SessionID),
		SessionID:  d.SessionID,
		CreateTime: d.CreateTime,
	}
}

// sessionDocumentFromDomain converts a domain Session into its MongoDB representation.
// The Name field is not stored in BSON.
func sessionDocumentFromDomain(s *domain.Session) *sessionDocument {
	if s == nil {
		return nil
	}

	return &sessionDocument{
		SessionID:  s.SessionID,
		CreateTime: s.CreateTime,
	}
}
