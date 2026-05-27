// Package mongo provides the MongoDB-backed SessionRepository implementation.
package mongo

import (
	"context"
	"errors"
	"time"

	"dominion/projects/game/session/domain"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// databaseName is the MongoDB database name for session storage.
	databaseName = "game_session"
	// collectionName is the MongoDB collection name for sessions.
	collectionName = "sessions"
)

// singleResult wraps the decode behavior of a MongoDB single document query result.
type singleResult interface {
	Decode(v interface{}) error
}

// collectionOps defines the MongoDB collection operations used by sessionRepository.
type collectionOps interface {
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error)
	FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
}

// sessionCollection wraps a MongoDB Collection to implement collectionOps.
type sessionCollection struct {
	*mongodriver.Collection
}

func (c *sessionCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	return c.Collection.InsertOne(ctx, document, opts...)
}

func (c *sessionCollection) FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *sessionCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

// newSessionCollection creates a sessionCollection from a MongoDB client.
var newSessionCollection = func(client *mongodriver.Client, db string, coll string) collectionOps {
	return &sessionCollection{Collection: client.Database(db).Collection(coll)}
}

// sessionRepository stores Session entities in MongoDB.
type sessionRepository struct {
	collection collectionOps
}

// NewSessionRepository creates a MongoDB-backed SessionRepository.
func NewSessionRepository(client *mongodriver.Client, dbName string, collName string) domain.SessionRepository {
	return &sessionRepository{
		collection: newSessionCollection(client, dbName, collName),
	}
}

// Create stores a new session in MongoDB.
func (r *sessionRepository) Create(ctx context.Context, session *domain.Session) (*domain.Session, error) {
	doc := sessionDocumentFromDomain(session)
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return nil, domain.ErrAlreadyExists
		}
		return nil, err
	}

	return doc.toDomain(), nil
}

// Get retrieves a session by its session ID.
func (r *sessionRepository) Get(ctx context.Context, sessionID string) (*domain.Session, error) {
	filter := sessionFilter{SessionID: sessionID}
	result := new(sessionDocument)
	if err := r.collection.FindOne(ctx, filter).Decode(result); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return result.toDomain(), nil
}

// Delete removes a session by its session ID.
func (r *sessionRepository) Delete(ctx context.Context, sessionID string) error {
	filter := sessionFilter{SessionID: sessionID}
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// sessionFilter is a concrete BSON filter struct for querying by session ID.
type sessionFilter struct {
	SessionID string `bson:"session_id"`
}
