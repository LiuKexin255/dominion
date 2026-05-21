// Package storage provides repository implementations for the session service.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/session/domain"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// collectionName is the MongoDB collection used for sessions.
	collectionName = "sessions"

	mongoFieldName                = "name"
	mongoFieldType                = "type"
	mongoFieldStatus              = "status"
	mongoFieldGatewayID           = "gateway_id"
	mongoFieldToken               = "token"
	mongoFieldCreatedAt           = "created_at"
	mongoFieldUpdatedAt           = "updated_at"
	mongoFieldEndedAt             = "ended_at"
	mongoFieldReconnectGeneration = "reconnect_generation"
	mongoFieldLastError           = "last_error"

	spanSave   = "session.storage.save"
	spanGet    = "session.storage.get"
	spanList   = "session.storage.list"
	spanDelete = "session.storage.delete"

	logFieldSessionID = "session_id"
	logFieldCount     = "count"
	logFieldError     = "error"
)

// singleResult wraps the decode behavior of a MongoDB single document query result.
type singleResult interface {
	Decode(v any) error
}

// indexViewOps wraps the index operations used by MongoRepository.
type indexViewOps interface {
	CreateMany(ctx context.Context, models []mongodriver.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error)
}

// CursorOps wraps the iteration behavior of a MongoDB cursor.
type CursorOps interface {
	Next(ctx context.Context) bool
	Decode(v any) error
	Close(ctx context.Context) error
}

// CollectionOps defines the MongoDB collection operations used by MongoRepository.
type CollectionOps interface {
	FindOne(ctx context.Context, filter any, opts ...*options.FindOneOptions) singleResult
	Find(ctx context.Context, filter any, opts ...*options.FindOptions) (CursorOps, error)
	UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error)
	DeleteOne(ctx context.Context, filter any, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
	Indexes() indexViewOps
}

// mongoCollection adapts a *mongodriver.Collection to the collectionOps interface.
type mongoCollection struct {
	*mongodriver.Collection
}

func (c *mongoCollection) FindOne(ctx context.Context, filter any, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *mongoCollection) Find(ctx context.Context, filter any, opts ...*options.FindOptions) (CursorOps, error) {
	return c.Collection.Find(ctx, filter, opts...)
}

func (c *mongoCollection) UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	return c.Collection.UpdateOne(ctx, filter, update, opts...)
}

func (c *mongoCollection) DeleteOne(ctx context.Context, filter any, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

func (c *mongoCollection) Indexes() indexViewOps {
	return c.Collection.Indexes()
}

// mongoSession is the BSON document representation of a game session.
type mongoSession struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty"`
	Name                string             `bson:"name"`
	Type                int32              `bson:"type"`
	Status              int32              `bson:"status"`
	GatewayID           string             `bson:"gateway_id"`
	Token               string             `bson:"token,omitempty"`
	CreatedAt           time.Time          `bson:"created_at"`
	UpdatedAt           time.Time          `bson:"updated_at"`
	EndedAt             *time.Time         `bson:"ended_at,omitempty"`
	ReconnectGeneration int64              `bson:"reconnect_generation"`
	LastError           string             `bson:"last_error,omitempty"`
}

// MongoRepository stores game sessions in MongoDB.
type MongoRepository struct {
	collection CollectionOps
}

// NewMongoRepository creates a MongoDB-backed repository and ensures indexes eagerly.
func NewMongoRepository(ctx context.Context, db *mongodriver.Database) (*MongoRepository, error) {
	repo := &MongoRepository{
		collection: &mongoCollection{Collection: db.Collection(collectionName)},
	}

	if err := repo.ensureIndexes(ctx); err != nil {
		return nil, err
	}

	return repo, nil
}

func (r *MongoRepository) ensureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongodriver.IndexModel{
		{
			Keys:    bson.D{{Key: mongoFieldName, Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	})
	if err != nil {
		return fmt.Errorf("create session indexes: %w", err)
	}

	return nil
}

// Get retrieves a session by name.
func (r *MongoRepository) Get(ctx context.Context, name string) (*domain.Session, error) {
	ctx, span := otel.Tracer().Start(ctx, spanGet)
	defer span.End()

	sessionID := sessionIDFromName(name)
	span.SetAttributes(attribute.String(logFieldSessionID, sessionID))

	filter := bson.M{mongoFieldName: name}
	doc := new(mongoSession)
	if err := r.collection.FindOne(ctx, filter).Decode(doc); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		logs.Error(ctx, "failed to get session", event.String(logFieldSessionID, sessionID), event.Err(err))
		return nil, err
	}

	logs.Info(ctx, "session retrieved", event.String(logFieldSessionID, sessionID))
	return doc.toDomain()
}

// Save upserts a session document by resource name.
func (r *MongoRepository) Save(ctx context.Context, session *domain.Session) error {
	ctx, span := otel.Tracer().Start(ctx, spanSave)
	defer span.End()

	sessionID := session.ID()
	span.SetAttributes(attribute.String(logFieldSessionID, sessionID))

	doc := toMongoSession(session)

	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{mongoFieldName: doc.Name},
		bson.M{
			"$set":         doc.updateDocument(),
			"$setOnInsert": doc.insertDocument(),
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		logs.Error(ctx, "failed to save session", event.String(logFieldSessionID, sessionID), event.Err(err))
		return err
	}

	logs.Info(ctx, "session saved", event.String(logFieldSessionID, sessionID))
	return nil
}

// Delete removes a session by name.
func (r *MongoRepository) Delete(ctx context.Context, name string) error {
	ctx, span := otel.Tracer().Start(ctx, spanDelete)
	defer span.End()

	sessionID := sessionIDFromName(name)
	span.SetAttributes(attribute.String(logFieldSessionID, sessionID))

	result, err := r.collection.DeleteOne(ctx, bson.M{mongoFieldName: name})
	if err != nil {
		logs.Error(ctx, "failed to delete session", event.String(logFieldSessionID, sessionID), event.Err(err))
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	logs.Info(ctx, "session deleted", event.String(logFieldSessionID, sessionID))
	return nil
}

// List retrieves all sessions that have not ended.
func (r *MongoRepository) List(ctx context.Context) ([]*domain.Session, error) {
	ctx, span := otel.Tracer().Start(ctx, spanList)
	defer span.End()

	filter := bson.D{{Key: mongoFieldStatus, Value: bson.D{{Key: "$ne", Value: int32(domain.StatusEnded)}}}}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		logs.Error(ctx, "failed to list sessions", event.Err(err))
		return nil, fmt.Errorf("find sessions: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*domain.Session
	for cursor.Next(ctx) {
		doc := new(mongoSession)
		if err := cursor.Decode(doc); err != nil {
			logs.Error(ctx, "failed to decode session", event.Err(err))
			return nil, fmt.Errorf("decode session: %w", err)
		}
		session, err := doc.toDomain()
		if err != nil {
			return nil, fmt.Errorf("convert session %q: %w", doc.Name, err)
		}
		results = append(results, session)
	}

	span.SetAttributes(attribute.Int(logFieldCount, len(results)))
	logs.Info(ctx, "sessions listed", event.Int(logFieldCount, len(results)))

	if len(results) == 0 {
		return nil, nil
	}

	return results, nil
}

func (m *mongoSession) toDomain() (*domain.Session, error) {
	return domain.Rehydrate(domain.SessionSnapshot{
		ID:                  sessionIDFromName(m.Name),
		Type:                domain.SessionType(m.Type),
		Status:              domain.SessionStatus(m.Status),
		GatewayID:           m.GatewayID,
		Token:               m.Token,
		CreatedAt:           m.CreatedAt,
		UpdatedAt:           m.UpdatedAt,
		EndedAt:             cloneTimePtr(m.EndedAt),
		ReconnectGeneration: m.ReconnectGeneration,
		LastError:           m.LastError,
	})
}

func toMongoSession(session *domain.Session) mongoSession {
	snapshot := session.Snapshot()
	return mongoSession{
		Name:                sessionName(snapshot.ID),
		Type:                int32(snapshot.Type),
		Status:              int32(snapshot.Status),
		GatewayID:           snapshot.GatewayID,
		Token:               snapshot.Token,
		CreatedAt:           snapshot.CreatedAt,
		UpdatedAt:           snapshot.UpdatedAt,
		EndedAt:             cloneTimePtr(snapshot.EndedAt),
		ReconnectGeneration: snapshot.ReconnectGeneration,
		LastError:           snapshot.LastError,
	}
}

func (m *mongoSession) updateDocument() bson.M {
	return bson.M{
		mongoFieldStatus:              m.Status,
		mongoFieldGatewayID:           m.GatewayID,
		mongoFieldToken:               m.Token,
		mongoFieldUpdatedAt:           m.UpdatedAt,
		mongoFieldEndedAt:             m.EndedAt,
		mongoFieldReconnectGeneration: m.ReconnectGeneration,
		mongoFieldLastError:           m.LastError,
	}
}

func (m *mongoSession) insertDocument() bson.M {
	return bson.M{
		mongoFieldName:      m.Name,
		mongoFieldType:      m.Type,
		mongoFieldCreatedAt: m.CreatedAt,
	}
}

// sessionName formats a session ID as a resource name: sessions/{uuid}.
func sessionName(id string) string {
	return "sessions/" + id
}

// sessionIDFromName extracts the session ID from a resource name.
func sessionIDFromName(name string) string {
	return name[len("sessions/"):]
}

func cloneTimePtr(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	cloned := *ts
	return &cloned
}
