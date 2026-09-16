package mongo

import (
	"context"
	"encoding/base64"
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

	filterMap, isMap := filter.(bson.M)

	var filtered []*memoryDocument
	for _, key := range c.docsOrder {
		doc := c.docs[key]
		if isMap && !matchesMemoryFilter(filterMap, doc) {
			continue
		}
		filtered = append(filtered, doc)
	}

	sortSpec, _ := findOpts.Sort.(bson.D)
	sortMemoryDocs(filtered, sortSpec)

	if limit > 0 && int64(len(filtered)) > limit {
		filtered = filtered[:limit]
	}

	return &memoryFakeCursor{docs: filtered}, nil
}

// matchesMemoryFilter evaluates the filter shapes the repository builds:
// equality on template/session_id, the raw memory_id $gt cursor of the
// default listing, and the ordered-mode $or composite cursor predicate.
func matchesMemoryFilter(filter bson.M, doc *memoryDocument) bool {
	if tmpl, ok := filter[fieldTemplate]; ok && doc.Template != tmpl {
		return false
	}
	if sess, ok := filter[fieldSessionID]; ok && doc.SessionID != sess {
		return false
	}
	if cond, ok := filter[fieldMemoryID].(bson.M); ok {
		if gt, ok := cond["$gt"].(string); ok && doc.MemoryID <= gt {
			return false
		}
	}
	if clauses, ok := filter["$or"].(bson.A); ok {
		matched := false
		for _, clause := range clauses {
			clauseMap, ok := clause.(bson.M)
			if ok && matchesMemoryCursorClause(clauseMap, doc) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// matchesMemoryCursorClause evaluates one branch of the ordered-mode $or
// cursor predicate: update_time $lt, or update_time equality plus memory_id
// $gt.
func matchesMemoryCursorClause(clause bson.M, doc *memoryDocument) bool {
	if cond, ok := clause[fieldUpdateTime]; ok {
		switch v := cond.(type) {
		case time.Time:
			if !doc.UpdateTime.Equal(v) {
				return false
			}
		case bson.M:
			lt, ok := v["$lt"].(time.Time)
			if !ok || !doc.UpdateTime.Before(lt) {
				return false
			}
		default:
			return false
		}
	}
	if cond, ok := clause[fieldMemoryID].(bson.M); ok {
		gt, ok := cond["$gt"].(string)
		if !ok || doc.MemoryID <= gt {
			return false
		}
	}
	return true
}

// sortMemoryDocs applies the repository's sort specs: update_time descending
// with a memory_id ascending tie-break for the ordered listing, memory_id
// ascending otherwise.
func sortMemoryDocs(docs []*memoryDocument, spec bson.D) {
	updateTimeDesc := false
	for _, elem := range spec {
		if elem.Key == fieldUpdateTime && elem.Value == -1 {
			updateTimeDesc = true
			break
		}
	}

	sort.SliceStable(docs, func(i, j int) bool {
		if updateTimeDesc && !docs[i].UpdateTime.Equal(docs[j].UpdateTime) {
			return docs[i].UpdateTime.After(docs[j].UpdateTime)
		}
		return docs[i].MemoryID < docs[j].MemoryID
	})
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
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", 100, "", domain.ListMemoriesOrderMemoryIDAsc)

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
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", 2, "", domain.ListMemoriesOrderMemoryIDAsc)

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
	result2, nextToken2, err := repo.ListMemories(ctx, "saolei", "session-1", 2, nextToken, domain.ListMemoriesOrderMemoryIDAsc)

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
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-empty", 100, "", domain.ListMemoriesOrderMemoryIDAsc)

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
	result, _, err := repo.ListMemories(ctx, "saolei", "session-1", 100, "", domain.ListMemoriesOrderMemoryIDAsc)

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
	result, _, err := repo.ListMemories(ctx, "saolei", "session-1", 100, "", domain.ListMemoriesOrderMemoryIDAsc)

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

func TestMemoryListUpdateTimeDesc(t *testing.T) {
	ctx := context.Background()

	// given - distinct update times plus a same-millisecond tie group, and one
	// entry of another session that must stay excluded
	repo := newTestRepo()
	base := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	memories := []*domain.Memory{
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-1", Content: "最旧", UpdateTime: base},
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-4", Content: "并列乙", UpdateTime: base.Add(2 * time.Second)},
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-3", Content: "并列甲", UpdateTime: base.Add(2 * time.Second)},
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-2", Content: "中间", UpdateTime: base.Add(1 * time.Second)},
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-5", Content: "最新", UpdateTime: base.Add(3 * time.Second)},
		{Template: "saolei", SessionID: "session-2", MemoryID: "m-9", Content: "其他会话", UpdateTime: base.Add(4 * time.Second)},
	}
	for _, m := range memories {
		if err := repo.CreateMemory(ctx, m); err != nil {
			t.Fatalf("CreateMemory() seed unexpected error: %v", err)
		}
	}

	// when
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", 10, "", domain.ListMemoriesOrderUpdateTimeDesc)

	// then - update_time descending, the tie broken by memory_id ascending
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	if nextToken != "" {
		t.Fatalf("ListMemories() next_token = %q, want empty", nextToken)
	}
	wantIDs := []string{"m-5", "m-3", "m-4", "m-2", "m-1"}
	if len(result) != len(wantIDs) {
		t.Fatalf("ListMemories() got %d memories, want %d", len(result), len(wantIDs))
	}
	for i, want := range wantIDs {
		if result[i].MemoryID != want {
			t.Fatalf("ListMemories()[%d] memory_id = %q, want %q", i, result[i].MemoryID, want)
		}
	}
}

func TestMemoryListUpdateTimeDescPagination(t *testing.T) {
	ctx := context.Background()

	// given - 5 entries whose tie group (t2: a, b) spans a page boundary
	repo := newTestRepo()
	t0 := time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)
	memories := []*domain.Memory{
		{Template: "saolei", SessionID: "session-1", MemoryID: "b", Content: "并列乙", UpdateTime: t2},
		{Template: "saolei", SessionID: "session-1", MemoryID: "d", Content: "最旧", UpdateTime: t0},
		{Template: "saolei", SessionID: "session-1", MemoryID: "a", Content: "并列甲", UpdateTime: t2},
		{Template: "saolei", SessionID: "session-1", MemoryID: "e", Content: "中间乙", UpdateTime: t1},
		{Template: "saolei", SessionID: "session-1", MemoryID: "c", Content: "中间甲", UpdateTime: t1},
	}
	for _, m := range memories {
		if err := repo.CreateMemory(ctx, m); err != nil {
			t.Fatalf("CreateMemory() seed unexpected error: %v", err)
		}
	}

	// when - walk every page with page_size=2
	var pageSizes []int
	var gotIDs []string
	pageToken := ""
	for {
		page, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", 2, pageToken, domain.ListMemoriesOrderUpdateTimeDesc)
		if err != nil {
			t.Fatalf("ListMemories() page unexpected error: %v", err)
		}
		pageSizes = append(pageSizes, len(page))
		for _, m := range page {
			gotIDs = append(gotIDs, m.MemoryID)
		}
		pageToken = nextToken
		if pageToken == "" {
			break
		}
	}

	// then - 2/2/1 pages, every entry exactly once, the total order preserved
	// across the composite cursor
	wantPageSizes := []int{2, 2, 1}
	if len(pageSizes) != len(wantPageSizes) {
		t.Fatalf("ListMemories() produced %d pages, want %d", len(pageSizes), len(wantPageSizes))
	}
	for i, want := range wantPageSizes {
		if pageSizes[i] != want {
			t.Fatalf("ListMemories() page %d size = %d, want %d", i+1, pageSizes[i], want)
		}
	}
	wantIDs := []string{"a", "b", "c", "e", "d"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("ListMemories() returned %d entries in total, want %d", len(gotIDs), len(wantIDs))
	}
	for i, want := range wantIDs {
		if gotIDs[i] != want {
			t.Fatalf("ListMemories() entry %d = %q, want %q", i, gotIDs[i], want)
		}
	}
}

func TestMemoryListUpdateTimeDescInvalidPageToken(t *testing.T) {
	ctx := context.Background()

	mustB64 := func(v string) string {
		return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(v))
	}

	tests := []struct {
		name  string
		token string
	}{
		{name: "non-base64 token", token: "!!!not-base64!!!"},
		{name: "base64 JSON missing update_time", token: mustB64(`{"memory_id":"m-1"}`)},
		{name: "base64 JSON with unparseable update_time", token: mustB64(`{"update_time":"not-a-time","memory_id":"m-1"}`)},
		{name: "default-mode raw memory_id token", token: "m-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newTestRepo()

			// when
			_, _, err := repo.ListMemories(ctx, "saolei", "session-1", 10, tt.token, domain.ListMemoriesOrderUpdateTimeDesc)

			// then
			if err == nil {
				t.Fatalf("ListMemories() expected error, got nil")
			}
			if !errors.Is(err, domain.ErrInvalidPageToken) {
				t.Fatalf("ListMemories() error = %v, want ErrInvalidPageToken", err)
			}
		})
	}
}
