package mongo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dominion/projects/game/proxy/domain"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// fakeSingleResult implements singleResult for testing.
type fakeSingleResult struct {
	doc bson.M
	err error
}

func (r *fakeSingleResult) Decode(v interface{}) error {
	if r.err != nil {
		return r.err
	}
	data, err := bson.Marshal(r.doc)
	if err != nil {
		return err
	}
	return bson.Unmarshal(data, v)
}

// ownerRecordKey returns the composite storage key (template_id, session_id)
// of an owner document: a session is identified by the resource pattern
// templates/{template}/sessions/{session}, so the same session ID under
// different templates is a distinct record.
func ownerRecordKey(templateID, sessionID string) string {
	return templateID + "\x00" + sessionID
}

// fakeCollection implements collectionOps with in-memory storage.
type fakeCollection struct {
	mu      sync.Mutex
	records map[string]bson.M // keyed by (template_id, session_id)
}

func newFakeCollection() *fakeCollection {
	return &fakeCollection{
		records: make(map[string]bson.M),
	}
}

func (c *fakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	doc, ok := document.(bson.M)
	if !ok {
		// Try converting via bson marshal/unmarshal round-trip.
		data, err := bson.Marshal(document)
		if err != nil {
			return nil, err
		}
		doc = make(bson.M)
		if err := bson.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
	}

	templateID, _ := doc["template_id"].(string)
	sessionID, _ := doc["session_id"].(string)
	key := ownerRecordKey(templateID, sessionID)
	if _, exists := c.records[key]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{{Code: 11000, Message: "duplicate key"}},
		}
	}

	c.records[key] = doc
	return new(mongodriver.InsertOneResult), nil
}

func (c *fakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	f, ok := filter.(bson.M)
	if !ok {
		// Try converting via bson marshal/unmarshal round-trip.
		data, err := bson.Marshal(filter)
		if err != nil {
			return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
		}
		f = make(bson.M)
		if err := bson.Unmarshal(data, &f); err != nil {
			return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
		}
	}

	templateID, _ := f["template_id"].(string)
	sessionID, _ := f["session_id"].(string)
	doc, exists := c.records[ownerRecordKey(templateID, sessionID)]
	if !exists {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}

	return &fakeSingleResult{doc: doc}
}

func (c *fakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	f, ok := filter.(bson.M)
	if !ok {
		// Try converting via bson marshal/unmarshal round-trip.
		data, err := bson.Marshal(filter)
		if err != nil {
			return &mongodriver.DeleteResult{DeletedCount: 0}, nil
		}
		f = make(bson.M)
		if err := bson.Unmarshal(data, &f); err != nil {
			return &mongodriver.DeleteResult{DeletedCount: 0}, nil
		}
	}

	templateID, _ := f["template_id"].(string)
	sessionID, _ := f["session_id"].(string)
	key := ownerRecordKey(templateID, sessionID)
	if _, exists := c.records[key]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}

	delete(c.records, key)
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

// newStoreWithFakeCollection creates a mongoOwnerStore with a fake collection for testing.
func newStoreWithFakeCollection() *mongoOwnerStore {
	return &mongoOwnerStore{
		collection: newFakeCollection(),
	}
}

func TestMongoOwnerStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithFakeCollection()

	owner := &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "session-001",
		OwnerIndex: 2,
		Owner:      "agent-2",
		CreateTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	// when: create owner
	if err := store.Create(ctx, owner); err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	// when: get owner
	got, err := store.Get(ctx, "saolei", "session-001")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}

	// then: fields match
	if got.TemplateID != owner.TemplateID {
		t.Fatalf("Get().TemplateID = %q, want %q", got.TemplateID, owner.TemplateID)
	}
	if got.SessionID != owner.SessionID {
		t.Fatalf("Get().SessionID = %q, want %q", got.SessionID, owner.SessionID)
	}
	if got.OwnerIndex != owner.OwnerIndex {
		t.Fatalf("Get().OwnerIndex = %d, want %d", got.OwnerIndex, owner.OwnerIndex)
	}
	if got.Owner != owner.Owner {
		t.Fatalf("Get().Owner = %q, want %q", got.Owner, owner.Owner)
	}
}

func TestMongoOwnerStore_CreateDuplicate(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithFakeCollection()

	owner := &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "session-dup",
		OwnerIndex: 0,
		Owner:      "agent-0",
		CreateTime: time.Now(),
	}
	if err := store.Create(ctx, owner); err != nil {
		t.Fatalf("Create() first insert unexpected error: %v", err)
	}

	// when: create duplicate
	err := store.Create(ctx, owner)

	// then
	if err == nil {
		t.Fatalf("Create() duplicate expected error, got nil")
	}
	if !errors.Is(err, domain.ErrOwnerAlreadyExists) {
		t.Fatalf("Create() error = %v, want ErrOwnerAlreadyExists", err)
	}
}

func TestMongoOwnerStore_GetNotFound(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithFakeCollection()

	// when
	_, err := store.Get(ctx, "saolei", "nonexistent-session")

	// then
	if err == nil {
		t.Fatalf("Get() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		t.Fatalf("Get() error = %v, want ErrOwnerNotFound", err)
	}
}

func TestMongoOwnerStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithFakeCollection()

	owner := &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "session-del",
		OwnerIndex: 1,
		Owner:      "agent-1",
		CreateTime: time.Now(),
	}
	if err := store.Create(ctx, owner); err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	// when
	err := store.Delete(ctx, "saolei", "session-del")

	// then
	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	// then: get after delete returns not found
	_, err = store.Get(ctx, "saolei", "session-del")
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want ErrOwnerNotFound", err)
	}
}

func TestMongoOwnerStore_DeleteNotFound(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithFakeCollection()

	// when
	err := store.Delete(ctx, "saolei", "nonexistent-session")

	// then
	if err == nil {
		t.Fatalf("Delete() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		t.Fatalf("Delete() error = %v, want ErrOwnerNotFound", err)
	}
}

func TestMongoOwnerStore_CompositeKeyIsolation(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithFakeCollection()

	// given: same session id under two different templates
	saoleiOwner := &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "session-x",
		OwnerIndex: 1,
		Owner:      "agent-1",
		CreateTime: time.Now(),
	}
	otherOwner := &domain.AgentOwner{
		TemplateID: "other",
		SessionID:  "session-x",
		OwnerIndex: 2,
		Owner:      "agent-2",
		CreateTime: time.Now(),
	}

	// when: create both
	if err := store.Create(ctx, saoleiOwner); err != nil {
		t.Fatalf("Create() saolei owner unexpected error: %v", err)
	}
	if err := store.Create(ctx, otherOwner); err != nil {
		t.Fatalf("Create() other-template owner unexpected error: %v", err)
	}

	// then: each is found under its own composite key
	got, err := store.Get(ctx, "saolei", "session-x")
	if err != nil {
		t.Fatalf("Get(saolei) unexpected error: %v", err)
	}
	if got.Owner != "agent-1" {
		t.Fatalf("Get(saolei).Owner = %q, want %q", got.Owner, "agent-1")
	}

	got, err = store.Get(ctx, "other", "session-x")
	if err != nil {
		t.Fatalf("Get(other) unexpected error: %v", err)
	}
	if got.Owner != "agent-2" {
		t.Fatalf("Get(other).Owner = %q, want %q", got.Owner, "agent-2")
	}

	// then: duplicate check is per composite key, not per session id
	err = store.Create(ctx, saoleiOwner)
	if !errors.Is(err, domain.ErrOwnerAlreadyExists) {
		t.Fatalf("Create() same composite key error = %v, want ErrOwnerAlreadyExists", err)
	}

	// when: delete only the saolei record
	if err := store.Delete(ctx, "saolei", "session-x"); err != nil {
		t.Fatalf("Delete(saolei) unexpected error: %v", err)
	}

	// then: the other template's record is untouched
	if _, err = store.Get(ctx, "other", "session-x"); err != nil {
		t.Fatalf("Get(other) after Delete(saolei) unexpected error: %v", err)
	}
	if _, err = store.Get(ctx, "saolei", "session-x"); !errors.Is(err, domain.ErrOwnerNotFound) {
		t.Fatalf("Get(saolei) after Delete() error = %v, want ErrOwnerNotFound", err)
	}
}

func Test_agentOwnerDocument_RoundTrip(t *testing.T) {
	original := &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "session-roundtrip",
		OwnerIndex: 3,
		Owner:      "agent-3",
		CreateTime: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
	}

	// when: convert to BSON document and back
	doc := agentOwnerDocumentFromDomain(original)
	got := doc.toDomain()

	// then: fields match
	if got.TemplateID != original.TemplateID {
		t.Fatalf("toDomain().TemplateID = %q, want %q", got.TemplateID, original.TemplateID)
	}
	if got.SessionID != original.SessionID {
		t.Fatalf("toDomain().SessionID = %q, want %q", got.SessionID, original.SessionID)
	}
	if got.OwnerIndex != original.OwnerIndex {
		t.Fatalf("toDomain().OwnerIndex = %d, want %d", got.OwnerIndex, original.OwnerIndex)
	}
	if got.Owner != original.Owner {
		t.Fatalf("toDomain().Owner = %q, want %q", got.Owner, original.Owner)
	}
	if !got.CreateTime.Equal(original.CreateTime) {
		t.Fatalf("toDomain().CreateTime = %v, want %v", got.CreateTime, original.CreateTime)
	}
}

func Test_agentOwnerDocument_NilConversions(t *testing.T) {
	t.Run("nil document to domain", func(t *testing.T) {
		var doc *agentOwnerDocument
		// when
		got := doc.toDomain()

		// then
		if got != nil {
			t.Fatalf("toDomain() on nil = %v, want nil", got)
		}
	})

	t.Run("nil domain to document", func(t *testing.T) {
		// when
		got := agentOwnerDocumentFromDomain(nil)

		// then
		if got != nil {
			t.Fatalf("agentOwnerDocumentFromDomain(nil) = %v, want nil", got)
		}
	})
}
