// Package mongo provides the MongoDB-backed repository implementation for
// Memory entities (spec 039-planner-memory-calibration FR-006;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §3-4).
package mongo

import (
	"context"
	"errors"
	"fmt"

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
// §3) and the composite index backing the
// (update_time desc, memory_id asc) listing
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 5).
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

	// Create the composite index for the ordered listing: the
	// (template, session_id) equality prefix is followed by the sort keys in
	// index order, so the ordered scan needs no in-memory sort. Ignore the
	// error for a duplicate index (e.g. from a previous service start).
	_, _ = coll.Indexes().CreateOne(context.Background(), mongodriver.IndexModel{
		Keys: bson.D{
			{Key: fieldTemplate, Value: 1},
			{Key: fieldSessionID, Value: 1},
			{Key: fieldUpdateTime, Value: -1},
			{Key: fieldMemoryID, Value: 1},
		},
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

// ListMemories retrieves a page of Memories under a session in the requested
// order (AIP-132: https://google.aip.dev/132).
// ListMemoriesOrderMemoryIDAsc sorts by memory_id ascending and pages by raw
// memory_id tokens; ListMemoriesOrderUpdateTimeDesc sorts by update_time
// descending with memory_id ascending as the tie-break and pages by composite
// (update_time, memory_id) tokens, returning ErrInvalidPageToken when a token
// cannot be decoded
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1).
func (r *memoryRepository) ListMemories(ctx context.Context, template, session string, pageSize int, pageToken string, order domain.ListMemoriesOrder) ([]*domain.Memory, string, error) {
	if order == domain.ListMemoriesOrderUpdateTimeDesc {
		return r.listMemoriesByUpdateTimeDesc(ctx, template, session, pageSize, pageToken)
	}
	return r.listMemoriesByMemoryIDAsc(ctx, template, session, pageSize, pageToken)
}

// listMemoriesByMemoryIDAsc is the default listing: memory_id ascending,
// paged by raw memory_id tokens.
func (r *memoryRepository) listMemoriesByMemoryIDAsc(ctx context.Context, template, session string, pageSize int, pageToken string) ([]*domain.Memory, string, error) {
	filter := bson.M{fieldTemplate: template, fieldSessionID: session}
	if pageToken != "" {
		filter[fieldMemoryID] = bson.M{"$gt": pageToken}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{Key: fieldMemoryID, Value: 1}}).
		SetLimit(limit)

	docs, err := r.find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		nextPageToken = docs[pageSize-1].MemoryID
		docs = docs[:pageSize]
	}

	return memoryDocsToDomain(docs), nextPageToken, nil
}

// listMemoriesByUpdateTimeDesc lists by update_time descending with
// memory_id ascending as the tie-break — a deterministic total order — and
// pages by composite (update_time, memory_id) tokens.
func (r *memoryRepository) listMemoriesByUpdateTimeDesc(ctx context.Context, template, session string, pageSize int, pageToken string) ([]*domain.Memory, string, error) {
	filter := bson.M{fieldTemplate: template, fieldSessionID: session}
	if pageToken != "" {
		cursor, err := domain.DecodeMemoryPageToken(pageToken)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", domain.ErrInvalidPageToken, err)
		}
		filter["$or"] = bson.A{
			bson.M{fieldUpdateTime: bson.M{"$lt": cursor.UpdateTime}},
			bson.M{fieldUpdateTime: cursor.UpdateTime, fieldMemoryID: bson.M{"$gt": cursor.MemoryID}},
		}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{Key: fieldUpdateTime, Value: -1}, {Key: fieldMemoryID, Value: 1}}).
		SetLimit(limit)

	docs, err := r.find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		lastDoc := docs[pageSize-1]
		token, err := domain.EncodeMemoryPageToken(&domain.MemoryPageCursor{
			UpdateTime: lastDoc.UpdateTime,
			MemoryID:   lastDoc.MemoryID,
		})
		if err != nil {
			return nil, "", err
		}
		nextPageToken = token
		docs = docs[:pageSize]
	}

	return memoryDocsToDomain(docs), nextPageToken, nil
}

// find runs the cursor query and decodes the matching documents.
func (r *memoryRepository) find(ctx context.Context, filter bson.M, opts *options.FindOptions) ([]*memoryDocument, error) {
	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []*memoryDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// memoryDocsToDomain converts stored documents into domain memories.
func memoryDocsToDomain(docs []*memoryDocument) []*domain.Memory {
	memories := make([]*domain.Memory, 0, len(docs))
	for _, doc := range docs {
		memories = append(memories, doc.toDomain())
	}
	return memories
}
