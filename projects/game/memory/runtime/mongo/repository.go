// Package mongo provides the MongoDB-backed repository implementation for
// Memory entities (spec 039-planner-memory-calibration FR-006;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §3-4).
package mongo

import (
	"context"
	"errors"

	"dominion/projects/game/memory/domain"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// memoriesCollectionName is the MongoDB collection name for memories.
	memoriesCollectionName = "memories"
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

// collectionOps defines the MongoDB collection operations used by the repository.
type collectionOps interface {
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error)
	FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult
	ReplaceOne(ctx context.Context, filter interface{}, replacement interface{}, opts ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error)
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
	Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error)
	Indexes() mongodriver.IndexView
}

// mongoCollection wraps a MongoDB Collection to implement collectionOps.
type mongoCollection struct {
	*mongodriver.Collection
}

func (c *mongoCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	return c.Collection.InsertOne(ctx, document, opts...)
}

func (c *mongoCollection) FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *mongoCollection) ReplaceOne(ctx context.Context, filter interface{}, replacement interface{}, opts ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error) {
	return c.Collection.ReplaceOne(ctx, filter, replacement, opts...)
}

func (c *mongoCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

func (c *mongoCollection) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
	cur, err := c.Collection.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	return &mongoCursor{Cursor: cur}, nil
}

func (c *mongoCollection) Indexes() mongodriver.IndexView {
	return c.Collection.Indexes()
}

// mongoCursor wraps a MongoDB Cursor to implement cursorOps.
type mongoCursor struct {
	*mongodriver.Cursor
}

// newCollection creates a collectionOps from a MongoDB client.
var newCollection = func(client *mongodriver.Client, db string, coll string) collectionOps {
	return &mongoCollection{Collection: client.Database(db).Collection(coll)}
}

// memoryRepository stores Memory entities in MongoDB.
type memoryRepository struct {
	collection collectionOps
}

// NewRepository creates a MongoDB-backed MemoryRepository in the given
// database (the memory service's own database — "game_memory" per spec 039
// FR-006 / style/mongo.md). It creates the unique index on
// (template, session_id, memory_id)
// (specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §3).
func NewRepository(client *mongodriver.Client, dbName string) domain.MemoryRepository {
	coll := newCollection(client, dbName, memoriesCollectionName)

	// Create the unique identity index for memories.
	_, _ = coll.Indexes().CreateOne(context.Background(), mongodriver.IndexModel{
		Keys: bson.D{
			{Key: fieldTemplate, Value: 1},
			{Key: fieldSessionID, Value: 1},
			{Key: fieldMemoryID, Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})

	return &memoryRepository{
		collection: coll,
	}
}

// CreateMemory stores a new Memory in MongoDB. The caller is responsible for
// populating CreateTime and UpdateTime.
func (r *memoryRepository) CreateMemory(ctx context.Context, memory *domain.Memory) error {
	doc := memoryDocumentFromDomain(memory)

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}

	return nil
}

// UpdateMemory replaces the stored Memory identified by its (template,
// session_id, memory_id) scope. The _id and the server-managed create_time
// are preserved from the stored document; the caller supplies the new
// content/update_time.
func (r *memoryRepository) UpdateMemory(ctx context.Context, memory *domain.Memory) (*domain.Memory, error) {
	filter := memoryFilter{Template: memory.Template, SessionID: memory.SessionID, MemoryID: memory.MemoryID}

	existing := new(memoryDocument)
	if err := r.collection.FindOne(ctx, filter).Decode(existing); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	doc := memoryDocumentFromDomain(memory)
	doc.ID = existing.ID
	doc.CreateTime = existing.CreateTime

	result, err := r.collection.ReplaceOne(ctx, filter, doc)
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, domain.ErrNotFound
	}

	return doc.toDomain(), nil
}

// DeleteMemory removes a Memory by template, session and memory id.
func (r *memoryRepository) DeleteMemory(ctx context.Context, template, session, memoryID string) error {
	filter := memoryFilter{Template: template, SessionID: session, MemoryID: memoryID}
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// ListMemories retrieves a page of Memories under a session, sorted by
// memory_id ascending. pageSize controls the maximum number of results;
// pageToken is the cursor for the next page (AIP-158:
// https://google.aip.dev/158).
func (r *memoryRepository) ListMemories(ctx context.Context, template, session string, pageSize int, pageToken string) ([]*domain.Memory, string, error) {
	filter := bson.M{fieldTemplate: template, fieldSessionID: session}
	if pageToken != "" {
		filter[fieldMemoryID] = bson.M{"$gt": pageToken}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{Key: fieldMemoryID, Value: 1}}).
		SetLimit(limit)

	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []*memoryDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		lastDoc := docs[pageSize-1]
		nextPageToken = lastDoc.MemoryID
		docs = docs[:pageSize]
	}

	memories := make([]*domain.Memory, 0, len(docs))
	for _, doc := range docs {
		memories = append(memories, doc.toDomain())
	}

	return memories, nextPageToken, nil
}
