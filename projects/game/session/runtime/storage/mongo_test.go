package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dominion/projects/game/session/domain"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestMongoRepository_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("found returns session", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		session := newTestSession(t, "test-id-1")
		if err := repo.Save(ctx, session); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		got, err := repo.Get(ctx, sessionName("test-id-1"))

		// then
		if err != nil {
			t.Fatalf("Get() unexpected error: %v", err)
		}
		if got.Snapshot().ID != "test-id-1" {
			t.Fatalf("ID = %q, want %q", got.Snapshot().ID, "test-id-1")
		}
		if !reflect.DeepEqual(collection.lastFindOneFilter, bson.M{mongoFieldName: sessionName("test-id-1")}) {
			t.Fatalf("FindOne() filter = %#v, want %#v", collection.lastFindOneFilter, bson.M{mongoFieldName: sessionName("test-id-1")})
		}
	})

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()

		// when
		_, err := repo.Get(ctx, sessionName("missing-id"))

		// then
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Get() error = %v, want %v", err, domain.ErrNotFound)
		}
		if !reflect.DeepEqual(collection.lastFindOneFilter, bson.M{mongoFieldName: sessionName("missing-id")}) {
			t.Fatalf("FindOne() filter = %#v, want %#v", collection.lastFindOneFilter, bson.M{mongoFieldName: sessionName("missing-id")})
		}
	})
}

func TestMongoRepository_Save(t *testing.T) {
	ctx := context.Background()

	t.Run("creates new session", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		session := newTestSession(t, "new-session-id")

		// when
		err := repo.Save(ctx, session)

		// then
		if err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}
		if collection.lastUpdateFilter == nil {
			t.Fatal("Save() did not call UpdateOne")
		}
		if !reflect.DeepEqual(collection.lastUpdateFilter, bson.M{mongoFieldName: sessionName("new-session-id")}) {
			t.Fatalf("UpdateOne() filter = %#v, want %#v", collection.lastUpdateFilter, bson.M{mongoFieldName: sessionName("new-session-id")})
		}
		if collection.lastUpdateUpsert == nil || !*collection.lastUpdateUpsert {
			t.Fatalf("UpdateOne() upsert = %v, want true", collection.lastUpdateUpsert)
		}

		got, err := repo.Get(ctx, sessionName("new-session-id"))
		if err != nil {
			t.Fatalf("Get() after Save() unexpected error: %v", err)
		}
		if got.Snapshot().ID != "new-session-id" {
			t.Fatalf("ID = %q, want %q", got.Snapshot().ID, "new-session-id")
		}
	})

	t.Run("updates existing session", func(t *testing.T) {
		// given
		repo, _ := newMongoRepositoryForTest()
		session := newTestSession(t, "update-id")
		if err := repo.Save(ctx, session); err != nil {
			t.Fatalf("Save() initial unexpected error: %v", err)
		}
		if err := session.MarkActive(); err != nil {
			t.Fatalf("MarkActive() unexpected error: %v", err)
		}

		// when
		err := repo.Save(ctx, session)

		// then
		if err != nil {
			t.Fatalf("Save() update unexpected error: %v", err)
		}
		got, err := repo.Get(ctx, sessionName("update-id"))
		if err != nil {
			t.Fatalf("Get() after update unexpected error: %v", err)
		}
		if got.Snapshot().Status != domain.StatusActive {
			t.Fatalf("Status = %v, want %v", got.Snapshot().Status, domain.StatusActive)
		}
	})

	t.Run("duplicate key maps to already exists", func(t *testing.T) {
		// given
		repo := &MongoRepository{
			collection: &fakeCollectionOps{
				docs: make(map[string]*mongoSession),
				updateErr: mongodriver.WriteException{
					WriteErrors: []mongodriver.WriteError{{Code: 11000, Message: "duplicate key"}},
				},
			},
		}
		session := newTestSession(t, "dup-id")

		// when
		err := repo.Save(ctx, session)

		// then
		if !errors.Is(err, domain.ErrAlreadyExists) {
			t.Fatalf("Save() error = %v, want %v", err, domain.ErrAlreadyExists)
		}
	})
}

func TestMongoRepository_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes existing session", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		session := newTestSession(t, "delete-id")
		if err := repo.Save(ctx, session); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		err := repo.Delete(ctx, sessionName("delete-id"))

		// then
		if err != nil {
			t.Fatalf("Delete() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(collection.lastDeleteFilter, bson.M{mongoFieldName: sessionName("delete-id")}) {
			t.Fatalf("DeleteOne() filter = %#v, want %#v", collection.lastDeleteFilter, bson.M{mongoFieldName: sessionName("delete-id")})
		}
		_, getErr := repo.Get(ctx, sessionName("delete-id"))
		if !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("Get() after Delete() error = %v, want %v", getErr, domain.ErrNotFound)
		}
	})

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		// given
		repo, _ := newMongoRepositoryForTest()

		// when
		err := repo.Delete(ctx, sessionName("ghost-id"))

		// then
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Delete() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestMongoSession_ToDomain_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		status domain.SessionStatus
	}{
		{name: "pending status", status: domain.StatusPending},
		{name: "active status", status: domain.StatusActive},
		{name: "disconnected status", status: domain.StatusDisconnected},
		{name: "ended status", status: domain.StatusEnded},
		{name: "failed status", status: domain.StatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			snap := domain.SessionSnapshot{
				ID:     "round-trip-id",
				Type:   domain.TypeSaolei,
				Status: tt.status,
			}
			session, err := domain.Rehydrate(snap)
			if err != nil {
				t.Fatalf("Rehydrate() error = %v", err)
			}

			// when
			mongoDoc := toMongoSession(session.Snapshot())
			got, err := mongoDoc.toDomain()

			// then
			if err != nil {
				t.Fatalf("toDomain() error = %v", err)
			}
			if got.Snapshot().ID != snap.ID {
				t.Fatalf("ID = %q, want %q", got.Snapshot().ID, snap.ID)
			}
			if got.Snapshot().Type != snap.Type {
				t.Fatalf("Type = %v, want %v", got.Snapshot().Type, snap.Type)
			}
			if got.Snapshot().Status != snap.Status {
				t.Fatalf("Status = %v, want %v", got.Snapshot().Status, snap.Status)
			}
		})
	}
}

func Test_sessionName(t *testing.T) {
	// given / when / then
	if got := sessionName("abc-123"); got != "sessions/abc-123" {
		t.Fatalf("sessionName() = %q, want %q", got, "sessions/abc-123")
	}
}

func Test_sessionIDFromName(t *testing.T) {
	// given / when / then
	if got := sessionIDFromName("sessions/abc-123"); got != "abc-123" {
		t.Fatalf("sessionIDFromName() = %q, want %q", got, "abc-123")
	}
}

// fakeCollectionOps implements collectionOps for testing MongoRepository.
type fakeCollectionOps struct {
	indexes           fakeIndexViewOps
	docs              map[string]*mongoSession
	updateErr         error
	lastFindOneFilter any
	lastUpdateFilter  any
	lastUpdateUpdate  any
	lastUpdateUpsert  *bool
	lastDeleteFilter  any
}

func (f *fakeCollectionOps) FindOne(_ context.Context, filter any, _ ...*options.FindOneOptions) singleResult {
	f.lastFindOneFilter = filter
	filterDoc, _ := anyToBSONMap(filter)
	name, _ := filterDoc["name"].(string)
	if f.docs != nil {
		if doc, ok := f.docs[name]; ok {
			return fakeSingleResult{doc: doc}
		}
	}

	return fakeSingleResult{err: mongodriver.ErrNoDocuments}
}

func (f *fakeCollectionOps) UpdateOne(_ context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	f.lastUpdateFilter = filter
	f.lastUpdateUpdate = update
	if len(opts) > 0 && opts[0] != nil {
		f.lastUpdateUpsert = opts[0].Upsert
	}
	if f.updateErr != nil {
		return nil, f.updateErr
	}

	filterDoc, err := anyToBSONMap(filter)
	if err != nil {
		return nil, err
	}
	key, _ := filterDoc["name"].(string)
	updateDoc, err := anyToBSONMap(update)
	if err != nil {
		return nil, err
	}
	setDoc, err := anyToBSONMap(updateDoc["$set"])
	if err != nil {
		return nil, err
	}
	setOnInsertDoc, err := anyToBSONMap(updateDoc["$setOnInsert"])
	if err != nil {
		return nil, err
	}

	stored := new(mongoSession)
	if existing, ok := f.docs[key]; ok {
		copy := *existing
		stored = &copy
	} else if setOnInsertDoc != nil {
		if err := decodeBSONDocument(setOnInsertDoc, stored); err != nil {
			return nil, err
		}
	}
	if err := decodeBSONDocument(setDoc, stored); err != nil {
		return nil, err
	}
	if f.docs == nil {
		f.docs = make(map[string]*mongoSession)
	}
	if existing, ok := f.docs[key]; ok {
		stored.ID = existing.ID
	} else {
		stored.ID = primitive.NewObjectID()
	}
	matchedCount := int64(0)
	modifiedCount := int64(0)
	upsertedCount := int64(0)
	if _, ok := f.docs[key]; ok {
		matchedCount = 1
		modifiedCount = 1
	} else {
		upsertedCount = 1
	}
	stored.Name = key
	copy := *stored
	f.docs[key] = &copy

	return &mongodriver.UpdateResult{
		MatchedCount:  matchedCount,
		ModifiedCount: modifiedCount,
		UpsertedCount: upsertedCount,
		UpsertedID:    stored.ID,
	}, nil
}

func (f *fakeCollectionOps) DeleteOne(_ context.Context, filter any, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f.lastDeleteFilter = filter

	filterDoc, err := anyToBSONMap(filter)
	if err != nil {
		return nil, err
	}
	key, _ := filterDoc["name"].(string)
	if f.docs == nil {
		return &mongodriver.DeleteResult{}, nil
	}
	if _, ok := f.docs[key]; !ok {
		return &mongodriver.DeleteResult{}, nil
	}
	delete(f.docs, key)

	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (f *fakeCollectionOps) Indexes() indexViewOps {
	return &f.indexes
}

type fakeIndexViewOps struct {
	models []mongodriver.IndexModel
	err    error
}

func (f *fakeIndexViewOps) CreateMany(_ context.Context, models []mongodriver.IndexModel, _ ...*options.CreateIndexesOptions) ([]string, error) {
	f.models = append([]mongodriver.IndexModel(nil), models...)
	if f.err != nil {
		return nil, f.err
	}

	return []string{"idx1"}, nil
}

type fakeSingleResult struct {
	doc any
	err error
}

func (r fakeSingleResult) Decode(v any) error {
	if r.err != nil {
		return r.err
	}

	return decodeBSONDocument(r.doc, v)
}

func newMongoRepositoryForTest() (*MongoRepository, *fakeCollectionOps) {
	collection := &fakeCollectionOps{
		docs: make(map[string]*mongoSession),
	}

	return &MongoRepository{collection: collection}, collection
}

func newTestSession(t *testing.T, id string) *domain.Session {
	t.Helper()

	session, err := domain.NewSession(domain.TypeSaolei, id)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	return session
}

func anyToBSONMap(value any) (bson.M, error) {
	if value == nil {
		return nil, nil
	}

	if doc, ok := value.(bson.M); ok {
		return doc, nil
	}

	raw, err := bson.Marshal(value)
	if err != nil {
		return nil, err
	}

	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	return doc, nil
}

func decodeBSONDocument(doc any, out any) error {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return err
	}

	return bson.Unmarshal(raw, out)
}

func TestFakeStore_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("returns session when exists", func(t *testing.T) {
		// given
		store := NewFakeStore()
		session := newFakeTestSession(t, "fake-get-id")
		name := "sessions/fake-get-id"
		if err := store.Save(ctx, session); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		got, err := store.Get(ctx, name)

		// then
		if err != nil {
			t.Fatalf("Get() unexpected error: %v", err)
		}
		if got.Snapshot().ID != "fake-get-id" {
			t.Fatalf("ID = %q, want %q", got.Snapshot().ID, "fake-get-id")
		}
	})

	t.Run("returns ErrNotFound when missing", func(t *testing.T) {
		// given
		store := NewFakeStore()

		// when
		_, err := store.Get(ctx, "sessions/missing")

		// then
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Get() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestFakeStore_Save(t *testing.T) {
	ctx := context.Background()

	t.Run("creates new session", func(t *testing.T) {
		// given
		store := NewFakeStore()
		session := newFakeTestSession(t, "fake-new-id")

		// when
		err := store.Save(ctx, session)

		// then
		if err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}
		got, err := store.Get(ctx, "sessions/fake-new-id")
		if err != nil {
			t.Fatalf("Get() after Save() unexpected error: %v", err)
		}
		if got.Snapshot().ID != "fake-new-id" {
			t.Fatalf("ID = %q, want %q", got.Snapshot().ID, "fake-new-id")
		}
	})

	t.Run("updates existing session", func(t *testing.T) {
		// given
		store := NewFakeStore()
		session := newFakeTestSession(t, "fake-update-id")
		if err := store.Save(ctx, session); err != nil {
			t.Fatalf("Save() initial unexpected error: %v", err)
		}
		if err := session.MarkActive(); err != nil {
			t.Fatalf("MarkActive() unexpected error: %v", err)
		}

		// when
		err := store.Save(ctx, session)

		// then
		if err != nil {
			t.Fatalf("Save() update unexpected error: %v", err)
		}
		got, err := store.Get(ctx, "sessions/fake-update-id")
		if err != nil {
			t.Fatalf("Get() after update unexpected error: %v", err)
		}
		if got.Snapshot().Status != domain.StatusActive {
			t.Fatalf("Status = %v, want %v", got.Snapshot().Status, domain.StatusActive)
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		// given
		store := NewFakeStore()
		session := newFakeTestSession(t, "copy-id")
		if err := store.Save(ctx, session); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		got, err := store.Get(ctx, "sessions/copy-id")
		if err != nil {
			t.Fatalf("Get() unexpected error: %v", err)
		}
		_ = got.MarkActive()

		// then
		again, err := store.Get(ctx, "sessions/copy-id")
		if err != nil {
			t.Fatalf("Get() second unexpected error: %v", err)
		}
		if again.Snapshot().Status != domain.StatusPending {
			t.Fatalf("original mutated: Status = %v, want %v", again.Snapshot().Status, domain.StatusPending)
		}
	})
}

func TestFakeStore_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes existing session", func(t *testing.T) {
		// given
		store := NewFakeStore()
		session := newFakeTestSession(t, "fake-del-id")
		name := "sessions/fake-del-id"
		if err := store.Save(ctx, session); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		err := store.Delete(ctx, name)

		// then
		if err != nil {
			t.Fatalf("Delete() unexpected error: %v", err)
		}
		_, getErr := store.Get(ctx, name)
		if !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("Get() after Delete() error = %v, want %v", getErr, domain.ErrNotFound)
		}
	})

	t.Run("returns ErrNotFound when missing", func(t *testing.T) {
		// given
		store := NewFakeStore()

		// when
		err := store.Delete(ctx, "sessions/ghost")

		// then
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Delete() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func newFakeTestSession(t *testing.T, id string) *domain.Session {
	t.Helper()

	session, err := domain.NewSession(domain.TypeSaolei, id)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	return session
}
