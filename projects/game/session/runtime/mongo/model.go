// Package mongo provides the MongoDB-backed SessionRepository implementation.
package mongo

import (
	"time"

	"dominion/projects/game/session/domain"
)

// BSON field name constants for MongoDB documents.
const (
	fieldCreateTime = "create_time"
	fieldSessionID  = "session_id"
	fieldTemplate   = "template"
)

// sessionDocument stores Session documents in MongoDB.
// Name is not stored in BSON; it is synthesized at the handler boundary.
type sessionDocument struct {
	Template   string    `bson:"template"`
	SessionID  string    `bson:"session_id"`
	CreateTime time.Time `bson:"create_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *sessionDocument) toDomain() *domain.Session {
	if d == nil {
		return nil
	}

	return &domain.Session{
		Template:   d.Template,
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
		Template:   s.Template,
		SessionID:  s.SessionID,
		CreateTime: s.CreateTime,
	}
}
