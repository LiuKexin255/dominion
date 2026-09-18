package mongo

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"dominion/projects/game/memory/domain"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Fake implementations ---

// fakeSingleResult implements singleResult for in-memory testing.
type fakeSingleResult struct {
	doc interface{}
	err error
}

func (r *fakeSingleResult) Decode(v interface{}) error {
	if r.err != nil {
		return r.err
	}
	target, ok := v.(*memoryDocument)
	if !ok {
		return errors.New("invalid decode target type")
	}
	src, ok := r.doc.(*memoryDocument)
	if !ok {
		return errors.New("invalid decode target type")
	}
	*target = *src
	return nil
}

// memoryFakeCollection implements collectionOps with in-memory storage for
// memories. The identity key is the (template, session_id, memory_id) tuple.
type memoryFakeCollection struct {
	docs      map[string]*memoryDocument
	docsOrder []string
}

func newMemoryFakeCollection() *memoryFakeCollection {
	return &memoryFakeCollection{
		docs: map[string]*memoryDocument{},
	}
}

// docKey returns the map key for a memory document identity.
func docKey(template, session, memoryID string) string {
	return template + "\x00" + session + "\x00" + memoryID
}

func (c *memoryFakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	doc, ok := document.(*memoryDocument)
	if !ok {
		return nil, errors.New("invalid document type")
	}
	key := docKey(doc.Template, doc.SessionID, doc.MemoryID)
	if _, exists := c.docs[key]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{
				{Code: 11000, Message: "duplicate key error"},
			},
		}
	}
	c.docs[key] = doc
	c.docsOrder = append(c.docsOrder, key)
	return &mongodriver.InsertOneResult{}, nil
}

func (c *memoryFakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(memoryFilter)
	if !ok {
		return &fakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[docKey(f.Template, f.SessionID, f.MemoryID)]
	if !exists {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &fakeSingleResult{doc: doc}
}

func (c *memoryFakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(memoryFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	key := docKey(f.Template, f.SessionID, f.MemoryID)
	if _, exists := c.docs[key]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, key)
	for i, id := range c.docsOrder {
		if id == key {
			c.docsOrder = append(c.docsOrder[:i], c.docsOrder[i+1:]...)
			break
		}
	}
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (c *memoryFakeCollection) ReplaceOne(_ context.Context, filter interface{}, replacement interface{}, _ ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error) {
	f, ok := filter.(memoryFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	doc, ok := replacement.(*memoryDocument)
	if !ok {
		return nil, errors.New("invalid replacement type")
	}
	key := docKey(f.Template, f.SessionID, f.MemoryID)
	existing, exists := c.docs[key]
	if !exists {
		return &mongodriver.UpdateResult{MatchedCount: 0, ModifiedCount: 0}, nil
	}
	doc.ID = existing.ID
	c.docs[key] = doc
	return &mongodriver.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, nil
}

func (c *memoryFakeCollection) Indexes() mongodriver.IndexView {
	return mongodriver.IndexView{}
}

func (c *memoryFakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
	findOpts := options.Find()
	for _, o := range opts {
		if o != nil {
			findOpts = o
		}
	}

	var limit int64
	if findOpts.Limit != nil {
		limit = *findOpts.Limit
	}

	var filtered []*memoryDocument
	filterMap, isMap := filter.(bson.M)

	for _, key := range c.docsOrder {
		doc := c.docs[key]

		if isMap {
			if tmpl, hasTmpl := filterMap[fieldTemplate]; hasTmpl {
				if doc.Template != tmpl {
					continue
				}
			}
			if sess, hasSess := filterMap[fieldSessionID]; hasSess {
				if doc.SessionID != sess {
					continue
				}
			}
			if gtVal, hasGT := filterMap[fieldMemoryID]; hasGT {
				if gtMap, ok := gtVal.(bson.M); ok {
					if gt, ok2 := gtMap["$gt"]; ok2 {
						if doc.MemoryID <= gt.(string) {
							continue
						}
					}
				}
			}
		}

		filtered = append(filtered, doc)
	}

	if len(filtered) > 1 {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].MemoryID < filtered[j].MemoryID
		})
	}

	if limit > 0 && int64(len(filtered)) > limit {
		filtered = filtered[:limit]
	}

	return &memoryFakeCursor{docs: filtered}, nil
}

// memoryFakeCursor implements cursorOps with in-memory results.
type memoryFakeCursor struct {
	docs []*memoryDocument
}

func (c *memoryFakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*memoryDocument)
	if !ok {
		return errors.New("invalid results target type")
	}
	*ptr = c.docs
	return nil
}

func (c *memoryFakeCursor) Close(_ context.Context) error {
	return nil
}

// newTestRepo creates a memoryRepository backed by a fake collection.
func newTestRepo() *memoryRepository {
	return &memoryRepository{
		collection: newMemoryFakeCollection(),
	}
}

// --- Tests ---

func TestMemoryCreateGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	memory := &domain.Memory{
		Template:   "saolei",
		SessionID:  "session-1",
		MemoryID:   "mem-1",
		Content:    "player repeats the same mistake",
		CreateTime: now,
		UpdateTime: now,
	}

	// when - create
	err := repo.CreateMemory(ctx, memory)

	// then
	if err != nil {
		t.Fatalf("CreateMemory() unexpected error: %v", err)
	}

	// when - list back
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", 100, "")

	// then
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	if nextToken != "" {
		t.Fatalf("ListMemories() next_token = %q, want empty", nextToken)
	}
	if len(result) != 1 {
		t.Fatalf("ListMemories() got %d memories, want 1", len(result))
	}
	got := result[0]
	if got.Template != "saolei" {
		t.Fatalf("ListMemories() template = %q, want %q", got.Template, "saolei")
	}
	if got.SessionID != "session-1" {
		t.Fatalf("ListMemories() session_id = %q, want %q", got.SessionID, "session-1")
	}
	if got.MemoryID != "mem-1" {
		t.Fatalf("ListMemories() memory_id = %q, want %q", got.MemoryID, "mem-1")
	}
	if got.Content != "player repeats the same mistake" {
		t.Fatalf("ListMemories() content = %q, want %q", got.Content, "player repeats the same mistake")
	}
	if !got.CreateTime.Equal(now) {
		t.Fatalf("ListMemories() create_time = %v, want %v", got.CreateTime, now)
	}
	if !got.UpdateTime.Equal(now) {
		t.Fatalf("ListMemories() update_time = %v, want %v", got.UpdateTime, now)
	}
}

func TestMemoryCreateDuplicate(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	memory := &domain.Memory{
		Template:  "saolei",
		SessionID: "session-1",
		MemoryID:  "mem-1",
	}
	err := repo.CreateMemory(ctx, memory)
	if err != nil {
		t.Fatalf("CreateMemory() first insert unexpected error: %v", err)
	}

	// when - create duplicate under the same session
	err = repo.CreateMemory(ctx, memory)

	// then
	if err == nil {
		t.Fatalf("CreateMemory() duplicate expected error, got nil")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateMemory() error = %v, want ErrAlreadyExists", err)
	}

	// when - same memory_id under a different session
	otherSession := &domain.Memory{
		Template:  "saolei",
		SessionID: "session-2",
		MemoryID:  "mem-1",
	}
	err = repo.CreateMemory(ctx, otherSession)

	// then - allowed: identity is scoped to the session
	if err != nil {
		t.Fatalf("CreateMemory() under other session unexpected error: %v", err)
	}
}

func TestMemoryList(t *testing.T) {
	ctx := context.Background()

	// given - seed 3 saolei/session-1 memories and 1 memory of another session
	repo := newTestRepo()
	memories := []*domain.Memory{
		{Template: "saolei", SessionID: "session-1", MemoryID: "alpha", Content: "c1"},
		{Template: "saolei", SessionID: "session-1", MemoryID: "bravo", Content: "c2"},
		{Template: "saolei", SessionID: "session-1", MemoryID: "charlie", Content: "c3"},
		{Template: "saolei", SessionID: "session-2", MemoryID: "delta", Content: "c4"},
	}
	for _, m := range memories {
		err := repo.CreateMemory(ctx, m)
		if err != nil {
			t.Fatalf("CreateMemory() seed unexpected error: %v", err)
		}
	}

	// when - first page with pageSize=2
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", 2, "")

	// then - first page has 2 session-1 memories (ASC: alpha, bravo) with next token
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListMemories() got %d memories, want 2", len(result))
	}
	if result[0].MemoryID != "alpha" {
		t.Fatalf("ListMemories() first memory_id = %q, want %q", result[0].MemoryID, "alpha")
	}
	if result[1].MemoryID != "bravo" {
		t.Fatalf("ListMemories() second memory_id = %q, want %q", result[1].MemoryID, "bravo")
	}
	if nextToken == "" {
		t.Fatalf("ListMemories() next_token is empty, want non-empty")
	}

	// when - second page using cursor from first page
	result2, nextToken2, err := repo.ListMemories(ctx, "saolei", "session-1", 2, nextToken)

	// then - second page has 1 memory (charlie), no next token; delta excluded
	if err != nil {
		t.Fatalf("ListMemories() page 2 unexpected error: %v", err)
	}
	if len(result2) != 1 {
		t.Fatalf("ListMemories() page 2 got %d memories, want 1", len(result2))
	}
	if result2[0].MemoryID != "charlie" {
		t.Fatalf("ListMemories() page 2 memory_id = %q, want %q", result2[0].MemoryID, "charlie")
	}
	if nextToken2 != "" {
		t.Fatalf("ListMemories() page 2 next_token = %q, want empty", nextToken2)
	}
}

func TestMemoryListEmpty(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()

	// when - list a session with no memories
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-empty", 100, "")

	// then
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("ListMemories() got %d memories, want 0", len(result))
	}
	if nextToken != "" {
		t.Fatalf("ListMemories() next_token = %q, want empty", nextToken)
	}
}

func TestMemoryUpdate(t *testing.T) {
	ctx := context.Background()

	// given - seed a memory
	repo := newTestRepo()
	seedCreateTime := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	seed := &domain.Memory{
		Template:   "saolei",
		SessionID:  "session-1",
		MemoryID:   "updatable",
		Content:    "old content",
		CreateTime: seedCreateTime,
		UpdateTime: seedCreateTime,
	}
	if err := repo.CreateMemory(ctx, seed); err != nil {
		t.Fatalf("CreateMemory() seed unexpected error: %v", err)
	}

	// when - update content
	updated := *seed
	updated.Content = "new content"
	persisted, err := repo.UpdateMemory(ctx, &updated)

	// then
	if err != nil {
		t.Fatalf("UpdateMemory() unexpected error: %v", err)
	}
	if persisted.Content != "new content" {
		t.Fatalf("UpdateMemory() content = %q, want %q", persisted.Content, "new content")
	}
	if !persisted.CreateTime.Equal(seedCreateTime) {
		t.Fatalf("UpdateMemory() create_time changed: got %v, want %v", persisted.CreateTime, seedCreateTime)
	}

	// when - re-read from repository
	result, _, err := repo.ListMemories(ctx, "saolei", "session-1", 100, "")

	// then - persisted value matches
	if err != nil {
		t.Fatalf("ListMemories() after update unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("ListMemories() after update got %d memories, want 1", len(result))
	}
	if result[0].Content != "new content" {
		t.Fatalf("ListMemories() after update content = %q, want %q", result[0].Content, "new content")
	}
}

func TestMemoryUpdateNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	memory := &domain.Memory{
		Template:  "saolei",
		SessionID: "session-1",
		MemoryID:  "ghost",
	}

	// when
	_, err := repo.UpdateMemory(ctx, memory)

	// then
	if err == nil {
		t.Fatalf("UpdateMemory() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateMemory() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryDelete(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	memory := &domain.Memory{
		Template:  "saolei",
		SessionID: "session-1",
		MemoryID:  "to-delete",
	}
	err := repo.CreateMemory(ctx, memory)
	if err != nil {
		t.Fatalf("CreateMemory() seed unexpected error: %v", err)
	}

	// when - delete with mismatched session
	err = repo.DeleteMemory(ctx, "saolei", "session-other", "to-delete")

	// then - not found
	if err == nil {
		t.Fatalf("DeleteMemory() with mismatched session expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteMemory() with mismatched session error = %v, want ErrNotFound", err)
	}

	// when - delete
	err = repo.DeleteMemory(ctx, "saolei", "session-1", "to-delete")

	// then
	if err != nil {
		t.Fatalf("DeleteMemory() unexpected error: %v", err)
	}

	// when - list after delete
	result, _, err := repo.ListMemories(ctx, "saolei", "session-1", 100, "")

	// then
	if err != nil {
		t.Fatalf("ListMemories() after delete unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("ListMemories() after delete got %d memories, want 0", len(result))
	}
}

func TestMemoryDeleteNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()

	// when
	err := repo.DeleteMemory(ctx, "saolei", "session-1", "nonexistent")

	// then
	if err == nil {
		t.Fatalf("DeleteMemory() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteMemory() error = %v, want ErrNotFound", err)
	}
}
