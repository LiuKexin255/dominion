// Package mongo provides the MongoDB-backed repository implementation for
// Memory entities (spec 039-planner-memory-calibration FR-006;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §3-4).
package mongo

import (
	"context"
	"errors"
	"maps"

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
// §3) and the composite index backing the consumed
// [update_time desc, memory_id asc] sort key
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 5-6). Adding a sortable field adds one whitelist row, one
// memoryDocument.sortValue case and the compound index for its consumed
// direction.
func NewRepository(client *mongodriver.Client, dbName string) domain.MemoryRepository {
	coll := newCollection(client, dbName, memoriesCollectionName)

	// Create the unique identity index for memories: it backs the default
	// [memory_id asc] sort key (equality prefix + final key).
	_, _ = coll.Indexes().CreateOne(context.Background(), mongodriver.IndexModel{
		Keys: bson.D{
			{Key: fieldTemplate, Value: 1},
			{Key: fieldSessionID, Value: 1},
			{Key: fieldMemoryID, Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})

	// Create the composite index for the [update_time desc, memory_id asc]
	// sort key (the only consumed non-default direction): the
	// (template, session_id) equality prefix is followed by the sort keys in
	// key order and direction (ESR:
	// https://www.mongodb.com/docs/manual/tutorial/equality-sort-range-guideline/),
	// so the ordered scan needs no in-memory sort. Ignore the error for a
	// duplicate index (e.g. from a previous service start).
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
// sort key order (AIP-132: https://google.aip.dev/132). The inputs are already
// validated — sort is a ParseMemoryOrderBy product, cursor a
// DecodeMemoryPageToken product (nil = first page, the only criterion) — so
// this is a direct translation: the key becomes the Mongo sort document, the
// cursor becomes the generic OR-ladder resume condition, and the page's last
// entry becomes the next token. The repository performs no value conversion:
// cursor entries already carry their typed values
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 5/9).
func (r *memoryRepository) ListMemories(ctx context.Context, template, session string, sort []*domain.MemorySortTerm, cursor *domain.MemoryPageCursor, pageSize int) ([]*domain.Memory, string, error) {
	filter := bson.M{fieldTemplate: template, fieldSessionID: session}
	if cursor != nil {
		filter["$or"] = memorySeekClauses(sort, cursor)
	}

	opts := options.Find().
		SetSort(memorySortDocument(sort)).
		SetLimit(int64(pageSize) + 1)

	docs, err := r.find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		nextPageToken = memoryNextPageToken(docs[pageSize-1], sort)
		docs = docs[:pageSize]
	}

	return memoryDocsToDomain(docs), nextPageToken, nil
}

// memorySeekClauses builds the resume-after-cursor condition for the final key
// of ANY length as the standard OR ladder of the seek method: clause i keeps
// the equality prefix of keys 0..i-1 and compares key i with $gt ($lt for a
// descending key). The prefix is grown incrementally — each key's equality
// form is written once as the loop advances, and each clause is a shallow copy
// of the current prefix plus the current comparison, so the total work is
// proportional to the ladder's own output (entry values are immutable
// scalars, so sharing them across clauses is safe). A single-key final key
// naturally degenerates to a one-clause $or: no length branching and no
// "merge into the scope filter" special case. There is no error path — the
// values were converted once by DecodeMemoryPageToken
// (https://use-the-index-luke.com/sql/partial-results/fetch-next-page;
// specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 6).
func memorySeekClauses(sort []*domain.MemorySortTerm, cursor *domain.MemoryPageCursor) bson.A {
	clauses := bson.A{}
	prefix := bson.M{}
	for i, term := range sort {
		value := cursor.Entries[i].Typed()
		clause := maps.Clone(prefix)
		clause[term.MongoField] = bson.M{memoryCmpOp(term.Descending): value}
		clauses = append(clauses, clause)
		prefix[term.MongoField] = value
	}
	return clauses
}

// memoryCmpOp returns the Mongo comparison operator that seeks past a key:
// ascending keys compare with $gt, descending with $lt — the direction flips
// with the comparison (https://use-the-index-luke.com/sql/partial-results/
// fetch-next-page).
func memoryCmpOp(descending bool) string {
	if descending {
		return "$lt"
	}
	return "$gt"
}

// memorySortDocument translates the final key into the Mongo sort document
// (Mongo field × direction).
func memorySortDocument(sort []*domain.MemorySortTerm) bson.D {
	document := make(bson.D, 0, len(sort))
	for _, term := range sort {
		direction := 1
		if term.Descending {
			direction = -1
		}
		document = append(document, bson.E{Key: term.MongoField, Value: direction})
	}
	return document
}

// memoryNextPageToken encodes the page's last document position on the final
// sort key: each document value goes through its term's CursorEntry, the only
// place a typed cursor value becomes its wire form.
func memoryNextPageToken(doc *memoryDocument, sort []*domain.MemorySortTerm) string {
	var entries []*domain.MemoryCursorEntry
	for _, term := range sort {
		entries = append(entries, term.CursorEntry(doc.sortValue(term.Field)))
	}
	return domain.EncodeMemoryPageToken(&domain.MemoryPageCursor{Entries: entries})
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
