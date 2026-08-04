// Package mongo provides the MongoDB-backed SessionRepository implementation.
package mongo

import (
	"context"
	"errors"

	"dominion/projects/game/session/domain"

	"go.mongodb.org/mongo-driver/bson"
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

// cursorOps wraps the behavior of a MongoDB cursor for iterating query results.
type cursorOps interface {
	All(ctx context.Context, results interface{}) error
	Close(ctx context.Context) error
}

// collectionOps defines the MongoDB collection operations used by sessionRepository.
type collectionOps interface {
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error)
	FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
	Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error)
	Indexes() mongodriver.IndexView
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

func (c *sessionCollection) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
	cur, err := c.Collection.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	return &mongoCursor{Cursor: cur}, nil
}

func (c *sessionCollection) Indexes() mongodriver.IndexView {
	return c.Collection.Indexes()
}

// mongoCursor wraps a MongoDB Cursor to implement cursorOps.
type mongoCursor struct {
	*mongodriver.Cursor
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
// It ensures the composite index {create_time: -1, session_id: -1} exists.
func NewSessionRepository(client *mongodriver.Client, dbName string, collName string) domain.SessionRepository {
	coll := newSessionCollection(client, dbName, collName)
	indexModel := mongodriver.IndexModel{
		Keys: bson.D{{fieldCreateTime, -1}, {fieldSessionID, -1}},
	}
	// Create composite index for cursor-based list pagination.
	// Ignore error for duplicate index (e.g., from previous service start).
	_, _ = coll.Indexes().CreateOne(context.Background(), indexModel)
	return &sessionRepository{
		collection: coll,
	}
}

// Create stores a new session in MongoDB. The caller is responsible for
// populating CreateTime.
func (r *sessionRepository) Create(ctx context.Context, session *domain.Session) (*domain.Session, error) {
	doc := sessionDocumentFromDomain(session)

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

// List retrieves a page of sessions sorted by create_time DESC, session_id DESC.
// The cursor parameter points to the last session of the previous page; pass nil for the first page.
func (r *sessionRepository) List(ctx context.Context, pageSize int, cursor *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
	filter := bson.M{}
	if cursor != nil {
		filter = bson.M{
			"$or": bson.A{
				bson.M{fieldCreateTime: bson.M{"$lt": cursor.CreateTime}},
				bson.M{fieldCreateTime: cursor.CreateTime, fieldSessionID: bson.M{"$lt": cursor.SessionID}},
			},
		}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{fieldCreateTime, -1}, {fieldSessionID, -1}}).
		SetLimit(limit)

	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []*sessionDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		lastDoc := docs[pageSize-1]
		token, err := domain.EncodePageToken(&domain.ListPageCursor{
			CreateTime: lastDoc.CreateTime,
			SessionID:  lastDoc.SessionID,
		})
		if err != nil {
			return nil, err
		}
		nextPageToken = token
		docs = docs[:pageSize]
	}

	sessions := make([]*domain.Session, 0, len(docs))
	for _, doc := range docs {
		sessions = append(sessions, doc.toDomain())
	}

	return &domain.ListSessionsResult{
		Sessions:      sessions,
		NextPageToken: nextPageToken,
	}, nil
}

// sessionFilter is a concrete BSON filter struct for querying by session ID.
type sessionFilter struct {
	SessionID string `bson:"session_id"`
}
