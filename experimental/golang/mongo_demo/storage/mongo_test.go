package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	mongodemo "dominion/experimental/golang/mongo_demo"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeSingleResult implements singleResult for testing.
type fakeSingleResult struct {
	record *mongoRecord
	err    error
}

func (r *fakeSingleResult) Decode(v interface{}) error {
	if r.err != nil {
		return r.err
	}
	ptr, ok := v.(*mongoRecord)
	if !ok {
		return fmt.Errorf("unexpected decode target type")
	}
	*ptr = *r.record
	return nil
}

// fakeCursor implements cursor for testing.
type fakeCursor struct {
	records []*mongoRecord
}

func (c *fakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*mongoRecord)
	if !ok {
		return fmt.Errorf("unexpected results type")
	}
	*ptr = append(*ptr, c.records...)
	return nil
}

func (c *fakeCursor) Close(_ context.Context) error { return nil }

// fakeCollection implements collectionOps for testing.
type fakeCollection struct {
	mu      sync.RWMutex
	records map[string]*mongoRecord
	idSeq   int64
}

func newFakeCollection() *fakeCollection {
	return &fakeCollection{
		records: make(map[string]*mongoRecord),
	}
}

func (fc *fakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	r, ok := document.(*mongoRecord)
	if !ok {
		return nil, fmt.Errorf("unexpected document type")
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, exists := fc.records[r.Name]; exists {
		var we mongodriver.WriteException
		we.WriteErrors = []mongodriver.WriteError{{Code: 11000}}
		return nil, we
	}

	fc.idSeq++
	fc.records[r.Name] = r

	return &mongodriver.InsertOneResult{InsertedID: fc.idSeq}, nil
}

func (fc *fakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(bson.M)
	if !ok {
		return &fakeSingleResult{err: fmt.Errorf("unexpected filter type")}
	}

	name, ok := f["name"].(string)
	if !ok {
		return &fakeSingleResult{err: fmt.Errorf("unexpected name type")}
	}

	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if r, exists := fc.records[name]; exists {
		return &fakeSingleResult{record: r}
	}

	return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
}

func (fc *fakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursor, error) {
	f, ok := filter.(bson.M)
	if !ok {
		return nil, fmt.Errorf("unexpected filter type")
	}

	fc.mu.RLock()
	defer fc.mu.RUnlock()

	var matched []*mongoRecord
	for _, r := range fc.records {
		if matchesFilter(r, f) {
			matched = append(matched, r)
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Name < matched[j].Name
	})

	var skip, limit int64
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if opt.Skip != nil {
			skip = *opt.Skip
		}
		if opt.Limit != nil {
			limit = *opt.Limit
		}
	}

	if skip > 0 {
		if int(skip) >= len(matched) {
			return &fakeCursor{}, nil
		}
		matched = matched[skip:]
	}

	if limit > 0 && int(limit) < len(matched) {
		matched = matched[:limit]
	}

	return &fakeCursor{records: matched}, nil
}

func (fc *fakeCollection) UpdateOne(_ context.Context, filter interface{}, update interface{}, _ ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	f, ok := filter.(bson.M)
	if !ok {
		return nil, fmt.Errorf("unexpected filter type")
	}

	name, ok := f["name"].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected name type")
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	r, exists := fc.records[name]
	if !exists {
		return &mongodriver.UpdateResult{}, nil
	}

	updateMap, ok := update.(bson.M)
	if !ok {
		return nil, fmt.Errorf("unexpected update type")
	}

	if setMap, ok := updateMap["$set"].(bson.M); ok {
		applySet(r, setMap)
	}

	return &mongodriver.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, nil
}

func (fc *fakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(bson.M)
	if !ok {
		return nil, fmt.Errorf("unexpected filter type")
	}

	name, ok := f["name"].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected name type")
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, exists := fc.records[name]; !exists {
		return &mongodriver.DeleteResult{}, nil
	}

	delete(fc.records, name)

	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (fc *fakeCollection) CountDocuments(_ context.Context, filter interface{}, _ ...*options.CountOptions) (int64, error) {
	f, ok := filter.(bson.M)
	if !ok {
		return 0, fmt.Errorf("unexpected filter type")
	}

	fc.mu.RLock()
	defer fc.mu.RUnlock()

	var count int64
	for _, r := range fc.records {
		if matchesFilter(r, f) {
			count++
		}
	}

	return count, nil
}

func matchesFilter(r *mongoRecord, filter bson.M) bool {
	for key, value := range filter {
		switch key {
		case "name":
			name, ok := value.(string)
			if !ok || r.Name != name {
				return false
			}
		case "app":
			app, ok := value.(string)
			if !ok || r.App != app {
				return false
			}
		case "archived":
			archivedMap, ok := value.(bson.M)
			if !ok {
				continue
			}
			ne, _ := archivedMap["$ne"]
			neBool, ok := ne.(bool)
			if !ok {
				continue
			}
			if r.Archived == neBool {
				return false
			}
		}
	}

	return true
}

func applySet(r *mongoRecord, setMap bson.M) {
	for key, value := range setMap {
		switch key {
		case "title":
			r.Title = value.(string)
		case "description":
			r.Description = value.(string)
		case "labels":
			if value == nil {
				r.Labels = nil
			} else {
				r.Labels = cloneStringMap(value.(map[string]string))
			}
		case "tags":
			if value == nil {
				r.Tags = nil
			} else {
				r.Tags = cloneStringSlice(value.([]string))
			}
		case "archived":
			r.Archived = value.(bool)
		case "profile":
			if value == nil {
				r.Profile = nil
			} else {
				r.Profile = value.(*mongoProfile)
			}
		case "profile.owner":
			if r.Profile == nil {
				r.Profile = new(mongoProfile)
			}
			r.Profile.Owner = value.(string)
		case "profile.priority":
			if r.Profile == nil {
				r.Profile = new(mongoProfile)
			}
			r.Profile.Priority = value.(int32)
		case "profile.watchers":
			if r.Profile == nil {
				r.Profile = new(mongoProfile)
			}
			if value == nil {
				r.Profile.Watchers = nil
			} else {
				r.Profile.Watchers = cloneStringSlice(value.([]string))
			}
		case "update_time":
			r.UpdateTime = value.(time.Time)
		}
	}
}

func overrideNewCollection(t *testing.T, fc *fakeCollection) {
	t.Helper()

	originalNewCollection := newCollection
	newCollection = func(_ *mongodriver.Client, _, _ string) collectionOps {
		return fc
	}
	t.Cleanup(func() { newCollection = originalNewCollection })
}

func TestMongoStore_Create(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*fakeCollection)
		record  *mongodemo.MongoRecord
		wantErr bool
	}{
		{
			name: "success",
			record: &mongodemo.MongoRecord{
				Name:        "apps/myapp/mongoRecords/rec1",
				Title:       "Test Record",
				Description: "A test description",
			},
		},
		{
			name: "duplicate key returns ErrAlreadyExists",
			setup: func(fc *fakeCollection) {
				fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
					Name:  "apps/myapp/mongoRecords/rec1",
					Title: "Existing",
				}
			},
			record: &mongodemo.MongoRecord{
				Name:  "apps/myapp/mongoRecords/rec1",
				Title: "Duplicate",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fc := newFakeCollection()
			if tt.setup != nil {
				tt.setup(fc)
			}
			store := &mongoStore{collection: fc}

			// when
			got, err := store.Create(ctx, tt.record)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Create() expected error, got nil")
				}
				if !errors.Is(err, ErrAlreadyExists) {
					t.Fatalf("Create() error = %v, want ErrAlreadyExists", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
			if got.GetName() != tt.record.GetName() {
				t.Fatalf("Create() name = %q, want %q", got.GetName(), tt.record.GetName())
			}
			if got.GetTitle() != tt.record.GetTitle() {
				t.Fatalf("Create() title = %q, want %q", got.GetTitle(), tt.record.GetTitle())
			}
			if fc.records[tt.record.GetName()].App != "apps/myapp" {
				t.Fatalf("Create() app = %q, want apps/myapp", fc.records[tt.record.GetName()].App)
			}
			if got.GetCreateTime() == nil {
				t.Fatalf("Create() create_time is nil")
			}
			if got.GetUpdateTime() == nil {
				t.Fatalf("Create() update_time is nil")
			}
		})
	}
}

func TestMongoStore_Get(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*fakeCollection)
		nameArg string
		wantErr bool
	}{
		{
			name: "existing record",
			setup: func(fc *fakeCollection) {
				fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
					Name:  "apps/myapp/mongoRecords/rec1",
					Title: "Found",
				}
			},
			nameArg: "apps/myapp/mongoRecords/rec1",
		},
		{
			name:    "non-existing record returns ErrNotFound",
			nameArg: "apps/myapp/mongoRecords/nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fc := newFakeCollection()
			if tt.setup != nil {
				tt.setup(fc)
			}
			store := &mongoStore{collection: fc}

			// when
			got, err := store.Get(ctx, tt.nameArg)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Get() expected error, got nil")
				}
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("Get() error = %v, want ErrNotFound", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Get() unexpected error: %v", err)
			}
			if got.GetName() != tt.nameArg {
				t.Fatalf("Get() name = %q, want %q", got.GetName(), tt.nameArg)
			}
		})
	}
}

func TestMongoStore_List(t *testing.T) {
	ctx := context.Background()

	t.Run("filter by parent", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/app1/mongoRecords/rec1"] = &mongoRecord{Name: "apps/app1/mongoRecords/rec1", App: "apps/app1", Title: "A1-1"}
		fc.records["apps/app1/mongoRecords/rec2"] = &mongoRecord{Name: "apps/app1/mongoRecords/rec2", App: "apps/app1", Title: "A1-2"}
		fc.records["apps/app2/mongoRecords/rec3"] = &mongoRecord{Name: "apps/app2/mongoRecords/rec3", App: "apps/app2", Title: "A2-1"}
		store := &mongoStore{collection: fc}

		// when
		got, nextToken, err := store.List(ctx, "apps/app1", 0, "", false)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("List() returned %d records, want 2", len(got))
		}
		if nextToken != "" {
			t.Fatalf("List() next_token = %q, want empty", nextToken)
		}
	})

	t.Run("exclude archived records", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{Name: "apps/myapp/mongoRecords/rec1", App: "apps/myapp", Title: "Active", Archived: false}
		fc.records["apps/myapp/mongoRecords/rec2"] = &mongoRecord{Name: "apps/myapp/mongoRecords/rec2", App: "apps/myapp", Title: "Archived", Archived: true}
		store := &mongoStore{collection: fc}

		// when
		got, _, err := store.List(ctx, "apps/myapp", 0, "", false)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("List() returned %d records, want 1", len(got))
		}
		if got[0].GetName() != "apps/myapp/mongoRecords/rec1" {
			t.Fatalf("List() returned %q, want apps/myapp/mongoRecords/rec1", got[0].GetName())
		}
	})

	t.Run("include archived when showArchived=true", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{Name: "apps/myapp/mongoRecords/rec1", App: "apps/myapp", Title: "Active", Archived: false}
		fc.records["apps/myapp/mongoRecords/rec2"] = &mongoRecord{Name: "apps/myapp/mongoRecords/rec2", App: "apps/myapp", Title: "Archived", Archived: true}
		store := &mongoStore{collection: fc}

		// when
		got, _, err := store.List(ctx, "apps/myapp", 0, "", true)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("List() returned %d records, want 2", len(got))
		}
	})

	t.Run("pagination with pageSize=2", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		for i := 0; i < 5; i++ {
			name := fmt.Sprintf("apps/myapp/mongoRecords/rec%d", i)
			fc.records[name] = &mongoRecord{Name: name, App: "apps/myapp", Title: fmt.Sprintf("Record %d", i)}
		}
		store := &mongoStore{collection: fc}

		// when - page 1
		page1, nextToken, err := store.List(ctx, "apps/myapp", 2, "", false)

		// then
		if err != nil {
			t.Fatalf("List() page 1 unexpected error: %v", err)
		}
		if len(page1) != 2 {
			t.Fatalf("List() page 1 returned %d records, want 2", len(page1))
		}
		if nextToken == "" {
			t.Fatalf("List() page 1 next_token is empty, expected token")
		}

		// when - page 2
		page2, nextToken2, err := store.List(ctx, "apps/myapp", 2, nextToken, false)

		// then
		if err != nil {
			t.Fatalf("List() page 2 unexpected error: %v", err)
		}
		if len(page2) != 2 {
			t.Fatalf("List() page 2 returned %d records, want 2", len(page2))
		}
		if nextToken2 == "" {
			t.Fatalf("List() page 2 next_token is empty, expected token")
		}

		// when - page 3 (last)
		page3, nextToken3, err := store.List(ctx, "apps/myapp", 2, nextToken2, false)

		// then
		if err != nil {
			t.Fatalf("List() page 3 unexpected error: %v", err)
		}
		if len(page3) != 1 {
			t.Fatalf("List() page 3 returned %d records, want 1", len(page3))
		}
		if nextToken3 != "" {
			t.Fatalf("List() page 3 next_token = %q, want empty", nextToken3)
		}
	})

	t.Run("empty parent returns nil", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{Name: "apps/myapp/mongoRecords/rec1", App: "apps/myapp", Title: "Record"}
		store := &mongoStore{collection: fc}

		// when
		got, nextToken, err := store.List(ctx, "apps/other", 0, "", false)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("List() returned %v, want nil", got)
		}
		if nextToken != "" {
			t.Fatalf("List() next_token = %q, want empty", nextToken)
		}
	})
}

func TestMongoStore_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("update title with mask", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
			Name:        "apps/myapp/mongoRecords/rec1",
			Title:       "Original Title",
			Description: "Original Description",
		}
		store := &mongoStore{collection: fc}

		update := &mongodemo.MongoRecord{
			Name:        "apps/myapp/mongoRecords/rec1",
			Title:       "Updated Title",
			Description: "Should Not Change",
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"title"}}

		// when
		got, err := store.Update(ctx, update, mask)

		// then
		if err != nil {
			t.Fatalf("Update() unexpected error: %v", err)
		}
		if got.GetTitle() != "Updated Title" {
			t.Fatalf("Update() title = %q, want %q", got.GetTitle(), "Updated Title")
		}
		if got.GetDescription() != "Original Description" {
			t.Fatalf("Update() description = %q, want %q", got.GetDescription(), "Original Description")
		}
	})

	t.Run("update non-existing returns ErrNotFound", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		store := &mongoStore{collection: fc}

		update := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/nonexistent",
			Title: "Updated",
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"title"}}

		// when
		_, err := store.Update(ctx, update, mask)

		// then
		if err == nil {
			t.Fatalf("Update() expected error, got nil")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("empty mask returns error", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Original",
		}
		store := &mongoStore{collection: fc}

		update := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Updated",
		}
		mask := &fieldmaskpb.FieldMask{}

		// when
		_, err := store.Update(ctx, update, mask)

		// then
		if err == nil {
			t.Fatalf("Update() expected error for empty mask, got nil")
		}
	})

	t.Run("update profile.owner with nested mask", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Original",
		}
		store := &mongoStore{collection: fc}

		update := &mongodemo.MongoRecord{
			Name: "apps/myapp/mongoRecords/rec1",
			Profile: &mongodemo.MongoRecordProfile{
				Owner:    "new-owner",
				Priority: 99,
			},
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"profile.owner"}}

		// when
		got, err := store.Update(ctx, update, mask)

		// then
		if err != nil {
			t.Fatalf("Update() unexpected error: %v", err)
		}
		if got.GetProfile().GetOwner() != "new-owner" {
			t.Fatalf("Update() profile.owner = %q, want %q", got.GetProfile().GetOwner(), "new-owner")
		}
		if got.GetProfile().GetPriority() != 0 {
			t.Fatalf("Update() profile.priority = %d, want 0 (unchanged)", got.GetProfile().GetPriority())
		}
	})

	t.Run("update profile.owner when profile already exists", func(t *testing.T) {
		// given
		fc := newFakeCollection()
		fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Original",
			Profile: &mongoProfile{
				Owner:    "old-owner",
				Priority: 5,
				Watchers: []string{"watcher1"},
			},
		}
		store := &mongoStore{collection: fc}

		update := &mongodemo.MongoRecord{
			Name: "apps/myapp/mongoRecords/rec1",
			Profile: &mongodemo.MongoRecordProfile{
				Owner: "new-owner",
			},
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"profile.owner"}}

		// when
		got, err := store.Update(ctx, update, mask)

		// then
		if err != nil {
			t.Fatalf("Update() unexpected error: %v", err)
		}
		if got.GetProfile().GetOwner() != "new-owner" {
			t.Fatalf("Update() profile.owner = %q, want %q", got.GetProfile().GetOwner(), "new-owner")
		}
		if got.GetProfile().GetPriority() != 5 {
			t.Fatalf("Update() profile.priority = %d, want 5 (unchanged)", got.GetProfile().GetPriority())
		}
		if len(got.GetProfile().GetWatchers()) != 1 || got.GetProfile().GetWatchers()[0] != "watcher1" {
			t.Fatalf("Update() profile.watchers changed unexpectedly: %v", got.GetProfile().GetWatchers())
		}
	})
}

func TestMongoStore_Delete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*fakeCollection)
		nameArg string
		wantErr bool
	}{
		{
			name: "delete existing record",
			setup: func(fc *fakeCollection) {
				fc.records["apps/myapp/mongoRecords/rec1"] = &mongoRecord{
					Name:  "apps/myapp/mongoRecords/rec1",
					Title: "To Delete",
				}
			},
			nameArg: "apps/myapp/mongoRecords/rec1",
		},
		{
			name:    "delete non-existing returns ErrNotFound",
			nameArg: "apps/myapp/mongoRecords/nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fc := newFakeCollection()
			if tt.setup != nil {
				tt.setup(fc)
			}
			store := &mongoStore{collection: fc}

			// when
			err := store.Delete(ctx, tt.nameArg)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Delete() expected error, got nil")
				}
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("Delete() error = %v, want ErrNotFound", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Delete() unexpected error: %v", err)
			}
		})
	}
}
