// Package runtime provides the MongoDB-backed SessionRepository implementation.
package runtime

import (
	"dominion/projects/game/session/domain"
	"time"
)

// sessionDocument stores Session documents in MongoDB.
type sessionDocument struct {
	Name       string    `bson:"name"`
	SessionID  string    `bson:"session_id"`
	CreateTime time.Time `bson:"create_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *sessionDocument) toDomain() *domain.Session {
	if d == nil {
		return nil
	}

	return &domain.Session{
		Name:       d.Name,
		SessionID:  d.SessionID,
		CreateTime: d.CreateTime,
	}
}

// sessionDocumentFromDomain converts a domain Session into its MongoDB representation.
func sessionDocumentFromDomain(s *domain.Session) *sessionDocument {
	if s == nil {
		return nil
	}

	return &sessionDocument{
		Name:       s.Name,
		SessionID:  s.SessionID,
		CreateTime: s.CreateTime,
	}
}
