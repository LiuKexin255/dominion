package mongo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
	// filters records the filter of every Find call so tests can assert the
	// seek-condition shape (e.g. its absence on the first page).
	filters []bson.M
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
	if isMap {
		c.filters = append(c.filters, filterMap)
	}

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

// matchesMemoryFilter evaluates the filter the repository builds: equality on
// template/session_id, plus either the folded single-key seek condition or
// the literal two-clause $or seek predicate.
func matchesMemoryFilter(filter bson.M, doc *memoryDocument) bool {
	for field, condition := range filter {
		if field == "$or" {
			continue
		}
		if !matchesMemoryFieldCondition(doc, field, condition) {
			return false
		}
	}
	if clauses, ok := filter["$or"].(bson.A); ok {
		matched := false
		for _, clause := range clauses {
			if matchesMemoryCondition(doc, clause) {
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

// matchesMemoryCondition evaluates one AND-ed condition document (an $or
// clause).
func matchesMemoryCondition(doc *memoryDocument, condition any) bool {
	clause, ok := condition.(bson.M)
	if !ok {
		return false
	}
	for field, value := range clause {
		if !matchesMemoryFieldCondition(doc, field, value) {
			return false
		}
	}
	return true
}

// matchesMemoryFieldCondition evaluates one field condition: an equality
// value or a direction-aware $gt/$lt comparison.
func matchesMemoryFieldCondition(doc *memoryDocument, field string, condition any) bool {
	value := memoryDocFieldValue(doc, field)
	comparison, ok := condition.(bson.M)
	if !ok {
		return memoryValueEqual(value, condition)
	}
	for operator, operand := range comparison {
		if compareMemoryValue(value, operand, operator) {
			return true
		}
	}
	return false
}

// memoryDocFieldValue returns the document value behind a Mongo field name
// used by the repository's filters and sort specs. Unknown fields fail fast
// so a whitelist row without a matching case surfaces in the first test run.
func memoryDocFieldValue(doc *memoryDocument, field string) any {
	switch field {
	case fieldTemplate:
		return doc.Template
	case fieldSessionID:
		return doc.SessionID
	case fieldMemoryID:
		return doc.MemoryID
	case fieldUpdateTime:
		return doc.UpdateTime
	default:
		panic(fmt.Sprintf("memoryDocFieldValue: unknown field %q", field))
	}
}

// memoryValueEqual compares two document values for equality.
func memoryValueEqual(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case time.Time:
		bv, ok := b.(time.Time)
		return ok && av.Equal(bv)
	default:
		return false
	}
}

// compareMemoryValue evaluates one direction-aware comparison from the seek
// predicate.
func compareMemoryValue(value, cursorValue any, operator string) bool {
	switch v := value.(type) {
	case string:
		c, ok := cursorValue.(string)
		if !ok {
			return false
		}
		if operator == "$gt" {
			return v > c
		}
		return v < c
	case time.Time:
		c, ok := cursorValue.(time.Time)
		if !ok {
			return false
		}
		if operator == "$gt" {
			return v.After(c)
		}
		return v.Before(c)
	default:
		return false
	}
}

// sortMemoryDocs applies the repository's Mongo sort spec over any key
// sequence and direction mix.
func sortMemoryDocs(docs []*memoryDocument, spec bson.D) {
	sort.SliceStable(docs, func(i, j int) bool {
		for _, key := range spec {
			cmp := compareMemoryValues(memoryDocFieldValue(docs[i], key.Key), memoryDocFieldValue(docs[j], key.Key))
			if cmp == 0 {
				continue
			}
			if key.Value == -1 {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
}

// compareMemoryValues orders two document values (-1, 0, 1) following the
// natural order of the whitelisted kinds.
func compareMemoryValues(a, b any) int {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		if !ok {
			return 0
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		default:
			return 0
		}
	case time.Time:
		bv, ok := b.(time.Time)
		if !ok {
			return 0
		}
		switch {
		case av.Before(bv):
			return -1
		case av.After(bv):
			return 1
		default:
			return 0
		}
	default:
		return 0
	}
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

// mustSortKey parses order_by into the final sort key, failing the test on
// error.
func mustSortKey(t *testing.T, orderBy string) []*domain.MemorySortTerm {
	t.Helper()
	sort, err := domain.ParseMemoryOrderBy(orderBy)
	if err != nil {
		t.Fatalf("ParseMemoryOrderBy(%q) setup error: %v", orderBy, err)
	}
	return sort
}

// resumeCursor decodes a returned page token into the cursor for the next
// call, failing the test on error.
func resumeCursor(t *testing.T, token string, sort []*domain.MemorySortTerm) *domain.MemoryPageCursor {
	t.Helper()
	cursor, err := domain.DecodeMemoryPageToken(token, sort)
	if err != nil {
		t.Fatalf("DecodeMemoryPageToken() unexpected error: %v", err)
	}
	return cursor
}

// assertMemoryIDs asserts the page's memory ids equal want in order.
func assertMemoryIDs(t *testing.T, memories []*domain.Memory, want []string) {
	t.Helper()
	if len(memories) != len(want) {
		t.Fatalf("ListMemories() returned %d memories, want %d", len(memories), len(want))
	}
	for i, id := range want {
		if memories[i].MemoryID != id {
			t.Fatalf("ListMemories()[%d] memory_id = %q, want %q", i, memories[i].MemoryID, id)
		}
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
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", mustSortKey(t, ""), nil, 100)

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
	sortKey := mustSortKey(t, "")

	// when - first page with pageSize=2
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", sortKey, nil, 2)

	// then - first page has 2 session-1 memories (ASC: alpha, bravo) with next token
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	assertMemoryIDs(t, result, []string{"alpha", "bravo"})
	if nextToken == "" {
		t.Fatalf("ListMemories() next_token is empty, want non-empty")
	}

	// when - second page using the decoded cursor
	result2, nextToken2, err := repo.ListMemories(ctx, "saolei", "session-1", sortKey, resumeCursor(t, nextToken, sortKey), 2)

	// then - second page has 1 memory (charlie), no next token; delta excluded
	if err != nil {
		t.Fatalf("ListMemories() page 2 unexpected error: %v", err)
	}
	assertMemoryIDs(t, result2, []string{"charlie"})
	if nextToken2 != "" {
		t.Fatalf("ListMemories() page 2 next_token = %q, want empty", nextToken2)
	}
}

func TestMemoryListEmpty(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()

	// when - list a session with no memories
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-empty", mustSortKey(t, ""), nil, 100)

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

func TestMemoryListMemoryIDDesc(t *testing.T) {
	ctx := context.Background()

	// given - three entries in one session and the [memory_id desc] key
	repo := newTestRepo()
	for _, id := range []string{"a", "b", "c"} {
		m := &domain.Memory{Template: "saolei", SessionID: "session-1", MemoryID: id, Content: "content-" + id}
		if err := repo.CreateMemory(ctx, m); err != nil {
			t.Fatalf("CreateMemory() seed unexpected error: %v", err)
		}
	}
	sortKey := mustSortKey(t, "memory_id desc")

	// when - walk every page with page_size=2
	var pageSizes []int
	var ids []string
	var cursor *domain.MemoryPageCursor
	for {
		page, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", sortKey, cursor, 2)
		if err != nil {
			t.Fatalf("ListMemories() page unexpected error: %v", err)
		}
		pageSizes = append(pageSizes, len(page))
		for _, m := range page {
			ids = append(ids, m.MemoryID)
		}
		if nextToken == "" {
			break
		}
		cursor = resumeCursor(t, nextToken, sortKey)
	}

	// then - the descending direction flips the cursor comparisons: 2/1 pages,
	// every entry once, order c/b/a
	if len(pageSizes) != 2 || pageSizes[0] != 2 || pageSizes[1] != 1 {
		t.Fatalf("ListMemories() page sizes = %v, want [2 1]", pageSizes)
	}
	if len(ids) != 3 || ids[0] != "c" || ids[1] != "b" || ids[2] != "a" {
		t.Fatalf("ListMemories() memory ids = %v, want [c b a]", ids)
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
	result, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", mustSortKey(t, "update_time desc"), nil, 10)

	// then - update_time descending, the tie broken by memory_id ascending
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	if nextToken != "" {
		t.Fatalf("ListMemories() next_token = %q, want empty", nextToken)
	}
	assertMemoryIDs(t, result, []string{"m-5", "m-3", "m-4", "m-2", "m-1"})
}

func TestMemoryListUpdateTimeDescPagination(t *testing.T) {
	ctx := context.Background()

	// given - 6 entries whose t2 tie group (a, b) is cut by the page_size=2
	// boundary after page 1: resuming page 2 must match b through the
	// equality prefix branch, not only through the range branch.
	repo := newTestRepo()
	t0 := time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)
	t3 := t0.Add(3 * time.Second)
	memories := []*domain.Memory{
		{Template: "saolei", SessionID: "session-1", MemoryID: "f", Content: "最新", UpdateTime: t3},
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
	sortKey := mustSortKey(t, "update_time desc")

	// when - walk every page with page_size=2
	var pageSizes []int
	var gotIDs []string
	var cursor *domain.MemoryPageCursor
	for {
		page, nextToken, err := repo.ListMemories(ctx, "saolei", "session-1", sortKey, cursor, 2)
		if err != nil {
			t.Fatalf("ListMemories() page unexpected error: %v", err)
		}
		pageSizes = append(pageSizes, len(page))
		for _, m := range page {
			gotIDs = append(gotIDs, m.MemoryID)
		}
		if nextToken == "" {
			break
		}
		cursor = resumeCursor(t, nextToken, sortKey)
	}

	// then - three full pages (2/2/2), every entry exactly once, and the
	// total order holds across the cut t2 group
	wantPageSizes := []int{2, 2, 2}
	if len(pageSizes) != len(wantPageSizes) {
		t.Fatalf("ListMemories() produced %d pages, want %d", len(pageSizes), len(wantPageSizes))
	}
	for i, want := range wantPageSizes {
		if pageSizes[i] != want {
			t.Fatalf("ListMemories() page %d size = %d, want %d", i+1, pageSizes[i], want)
		}
	}
	wantIDs := []string{"f", "a", "b", "c", "e", "d"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("ListMemories() returned %d entries in total, want %d", len(gotIDs), len(wantIDs))
	}
	for i, want := range wantIDs {
		if gotIDs[i] != want {
			t.Fatalf("ListMemories() entry %d = %q, want %q", i, gotIDs[i], want)
		}
	}
}

// TestMemoryListUpdateTimeDescExplicitTieBreaker verifies that the explicit
// multi-field spelling and the bare descending spelling (normalized with the
// designated tie-breaker) produce the same page
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §4).
func TestMemoryListUpdateTimeDescExplicitTieBreaker(t *testing.T) {
	ctx := context.Background()

	// given - two spellings of the same final sort key and a tie group
	implicitKey := mustSortKey(t, "update_time desc")
	explicitKey := mustSortKey(t, "update_time desc, memory_id")
	repo := newTestRepo()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	memories := []*domain.Memory{
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-1", Content: "最旧", UpdateTime: base},
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-3", Content: "并列乙", UpdateTime: base.Add(2 * time.Second)},
		{Template: "saolei", SessionID: "session-1", MemoryID: "m-2", Content: "并列甲", UpdateTime: base.Add(2 * time.Second)},
	}
	for _, m := range memories {
		if err := repo.CreateMemory(ctx, m); err != nil {
			t.Fatalf("CreateMemory() seed unexpected error: %v", err)
		}
	}

	// when
	implicit, _, err := repo.ListMemories(ctx, "saolei", "session-1", implicitKey, nil, 10)
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}
	explicit, _, err := repo.ListMemories(ctx, "saolei", "session-1", explicitKey, nil, 10)
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}

	// then - both spellings yield the tie-broken total order
	assertMemoryIDs(t, implicit, []string{"m-2", "m-3", "m-1"})
	assertMemoryIDs(t, explicit, []string{"m-2", "m-3", "m-1"})
}

// TestMemorySeekClauses pins the seek condition as a generic OR ladder: one
// clause per final-key element for any key length, each with the equality
// prefix and the direction-aware comparison — no len(sort) branching and no
// from-scratch prefix rebuild (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 item 6). Cursor entries are built through the
// terms' CursorEntry (the only legal source of their typed values); a
// regression to a per-length switch or to sharing the prefix map between
// clauses fails these cases.
func TestMemorySeekClauses(t *testing.T) {
	now, err := time.Parse(time.RFC3339Nano, "2026-09-16T10:00:00.000000001Z")
	if err != nil {
		t.Fatalf("time.Parse() setup error: %v", err)
	}
	singleKey := mustSortKey(t, "")
	doubleKey := mustSortKey(t, "update_time desc")
	tripleKey := []*domain.MemorySortTerm{
		{Field: "a", MongoField: "a"},
		{Field: "b", MongoField: "b", Descending: true},
		{Field: "c", MongoField: "c"},
	}

	tests := []struct {
		name   string
		sort   []*domain.MemorySortTerm
		cursor *domain.MemoryPageCursor
		want   bson.A
	}{
		{
			name:   "single key yields one clause",
			sort:   singleKey,
			cursor: &domain.MemoryPageCursor{Entries: []*domain.MemoryCursorEntry{singleKey[0].CursorEntry("m-1")}},
			want:   bson.A{bson.M{"memory_id": bson.M{"$gt": "m-1"}}},
		},
		{
			name: "two keys yield the prefix equality and both directions",
			sort: doubleKey,
			cursor: &domain.MemoryPageCursor{Entries: []*domain.MemoryCursorEntry{
				doubleKey[0].CursorEntry(now),
				doubleKey[1].CursorEntry("m-1"),
			}},
			want: bson.A{
				bson.M{"update_time": bson.M{"$lt": now}},
				bson.M{"update_time": now, "memory_id": bson.M{"$gt": "m-1"}},
			},
		},
		{
			name: "seek reads the entries' typed values, not their wire strings",
			sort: doubleKey,
			cursor: &domain.MemoryPageCursor{Entries: []*domain.MemoryCursorEntry{
				corruptWire(doubleKey[0].CursorEntry(now)),
				doubleKey[1].CursorEntry("m-1"),
			}},
			want: bson.A{
				bson.M{"update_time": bson.M{"$lt": now}},
				bson.M{"update_time": now, "memory_id": bson.M{"$gt": "m-1"}},
			},
		},
		{
			name: "three hand-built keys yield exactly three clauses",
			sort: tripleKey,
			cursor: &domain.MemoryPageCursor{Entries: []*domain.MemoryCursorEntry{
				tripleKey[0].CursorEntry("v0"),
				tripleKey[1].CursorEntry("v1"),
				tripleKey[2].CursorEntry("v2"),
			}},
			want: bson.A{
				bson.M{"a": bson.M{"$gt": "v0"}},
				bson.M{"a": "v0", "b": bson.M{"$lt": "v1"}},
				bson.M{"a": "v0", "b": "v1", "c": bson.M{"$gt": "v2"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			clauses := memorySeekClauses(tt.sort, tt.cursor)

			// then
			if !reflect.DeepEqual(clauses, tt.want) {
				t.Fatalf("memorySeekClauses() = %#v, want %#v", clauses, tt.want)
			}
		})
	}
}

// corruptWire replaces an entry's wire value with garbage while keeping its
// typed value: a consumer reading the typed value is unaffected, a consumer
// converting the wire string again breaks — pinning the single-shot conversion
// responsibility (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 item 9). The wire form is only serialized for
// next tokens, never re-parsed by the storage layer.
func corruptWire(entry *domain.MemoryCursorEntry) *domain.MemoryCursorEntry {
	entry.Value = "not-a-time"
	return entry
}

// TestMemoryListFirstPageHasNoSeekCondition pins nil as the single first-page
// criterion: the first-page filter carries no seek condition at all
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 4).
func TestMemoryListFirstPageHasNoSeekCondition(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	fake := repo.collection.(*memoryFakeCollection)
	seed := &domain.Memory{Template: "saolei", SessionID: "session-1", MemoryID: "m-1", Content: "c1"}
	if err := repo.CreateMemory(ctx, seed); err != nil {
		t.Fatalf("CreateMemory() seed unexpected error: %v", err)
	}

	// when - list the first page with a nil cursor
	_, _, err := repo.ListMemories(ctx, "saolei", "session-1", mustSortKey(t, ""), nil, 10)
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}

	// then
	if len(fake.filters) != 1 {
		t.Fatalf("Find() called %d times, want 1", len(fake.filters))
	}
	if _, ok := fake.filters[0]["$or"]; ok {
		t.Fatalf("first-page filter = %v, want no $or", fake.filters[0])
	}
}

// TestMemoryListSeekValuesAreTyped pins the typed-direct path: the seek
// condition's comparison values are the entries' typed values (time.Time for
// time fields), never wire strings re-parsed by the storage layer
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 9).
func TestMemoryListSeekValuesAreTyped(t *testing.T) {
	ctx := context.Background()

	// given - a decoded cursor for the update_time desc key
	sortKey := mustSortKey(t, "update_time desc")
	token := domain.EncodeMemoryPageToken(&domain.MemoryPageCursor{Entries: []*domain.MemoryCursorEntry{
		{Field: domain.MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00.000000001Z"},
		{Field: domain.MemorySortFieldMemoryID, Value: "m-1"},
	}})
	cursor, err := domain.DecodeMemoryPageToken(token, sortKey)
	if err != nil {
		t.Fatalf("DecodeMemoryPageToken() setup error: %v", err)
	}
	repo := newTestRepo()
	fake := repo.collection.(*memoryFakeCollection)

	// when
	_, _, err = repo.ListMemories(ctx, "saolei", "session-1", sortKey, cursor, 10)
	if err != nil {
		t.Fatalf("ListMemories() unexpected error: %v", err)
	}

	// then - the first clause compares update_time with a time.Time value
	clauses, ok := fake.filters[0]["$or"].(bson.A)
	if !ok || len(clauses) != 2 {
		t.Fatalf("seek filter = %v, want a two-clause $or", fake.filters[0])
	}
	clause, ok := clauses[0].(bson.M)
	if !ok {
		t.Fatalf("clause = %T, want bson.M", clauses[0])
	}
	comparison, ok := clause["update_time"].(bson.M)
	if !ok {
		t.Fatalf("clause = %v, want an update_time comparison", clause)
	}
	if value := comparison["$lt"]; value != cursor.Entries[0].Typed() {
		t.Fatalf("seek value = %v (%T), want the entry typed value %v", value, value, cursor.Entries[0].Typed())
	}
	if _, isTime := comparison["$lt"].(time.Time); !isTime {
		t.Fatalf("seek value = %T, want time.Time (typed direct, no repo conversion)", comparison["$lt"])
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
	result, _, err := repo.ListMemories(ctx, "saolei", "session-1", mustSortKey(t, ""), nil, 100)

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
	result, _, err := repo.ListMemories(ctx, "saolei", "session-1", mustSortKey(t, ""), nil, 100)

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
