// Package storage provides an in-memory FakeStore for testing Mongo demo record operations.
package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	mongodemo "dominion/experimental/mongo_demo"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// FakeStore is an in-memory implementation of MongoRecordStore for testing.
type FakeStore struct {
	mu      sync.RWMutex
	records map[string]*mongoRecord // key = name field value
	nowFunc func() time.Time        // for testability, defaults to time.Now
}

// NewFakeStore creates a new FakeStore ready for use.
func NewFakeStore() *FakeStore {
	return &FakeStore{
		records: make(map[string]*mongoRecord),
		nowFunc: time.Now,
	}
}

// now returns the current time using the configurable nowFunc.
func (s *FakeStore) now() time.Time {
	return s.nowFunc()
}

// Create stores a new record. Returns ErrAlreadyExists if a record with the same name exists.
func (s *FakeStore) Create(_ context.Context, record *mongodemo.MongoRecord) (*mongodemo.MongoRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := record.GetName()
	if _, exists := s.records[name]; exists {
		return nil, ErrAlreadyExists
	}

	r := mongoRecordFromProto(record)
	r.CreateTime = s.now()
	r.UpdateTime = s.now()

	s.records[name] = r

	return r.toProto(), nil
}

// Get retrieves a record by name. Returns ErrNotFound if the record does not exist.
func (s *FakeStore) Get(_ context.Context, name string) (*mongodemo.MongoRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, exists := s.records[name]
	if !exists {
		return nil, ErrNotFound
	}

	return r.toProto(), nil
}

// List returns records matching the given parent prefix with optional filtering and pagination.
func (s *FakeStore) List(_ context.Context, parent string, pageSize int32, pageToken string, showArchived bool) ([]*mongodemo.MongoRecord, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Normalize page size.
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}

	// Decode skip offset from page token.
	skip, err := DecodePageToken(pageToken)
	if err != nil {
		return nil, "", fmt.Errorf("invalid page token: %w", err)
	}

	// Filter records by parent prefix and archived status.
	prefix := parent + "/"
	var filtered []*mongoRecord
	for _, r := range s.records {
		if !isUnderParent(r.Name, prefix) {
			continue
		}
		if !showArchived && r.Archived {
			continue
		}
		filtered = append(filtered, r)
	}

	// Sort by name for stable ordering.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})

	// Apply pagination.
	total := len(filtered)
	if skip >= total {
		return nil, "", nil
	}

	end := skip + int(pageSize)
	if end > total {
		end = total
	}

	page := filtered[skip:end]

	var result []*mongodemo.MongoRecord
	for _, r := range page {
		result = append(result, r.toProto())
	}

	// Determine next page token.
	var nextToken string
	if end < total {
		nextToken = EncodePageToken(end)
	}

	return result, nextToken, nil
}

// Update modifies fields of an existing record according to the update mask.
// Returns ErrNotFound if the record does not exist.
func (s *FakeStore) Update(_ context.Context, record *mongodemo.MongoRecord, updateMask *fieldmaskpb.FieldMask) (*mongodemo.MongoRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := record.GetName()
	existing, exists := s.records[name]
	if !exists {
		return nil, ErrNotFound
	}

	if len(updateMask.GetPaths()) == 0 {
		return nil, fmt.Errorf("update_mask paths cannot be empty")
	}

	for _, path := range updateMask.GetPaths() {
		switch path {
		case "title":
			existing.Title = record.GetTitle()
		case "description":
			existing.Description = record.GetDescription()
		case "labels":
			existing.Labels = cloneStringMap(record.GetLabels())
		case "tags":
			existing.Tags = cloneStringSlice(record.GetTags())
		case "archived":
			existing.Archived = record.GetArchived()
		case "profile":
			if p := record.GetProfile(); p != nil {
				existing.Profile = &mongoProfile{
					Owner:    p.GetOwner(),
					Priority: p.GetPriority(),
					Watchers: cloneStringSlice(p.GetWatchers()),
				}
			} else {
				existing.Profile = nil
			}
		case "profile.owner":
			if existing.Profile == nil {
				existing.Profile = new(mongoProfile)
			}
			existing.Profile.Owner = record.GetProfile().GetOwner()
		case "profile.priority":
			if existing.Profile == nil {
				existing.Profile = new(mongoProfile)
			}
			existing.Profile.Priority = record.GetProfile().GetPriority()
		case "profile.watchers":
			if existing.Profile == nil {
				existing.Profile = new(mongoProfile)
			}
			existing.Profile.Watchers = cloneStringSlice(record.GetProfile().GetWatchers())
		}
	}

	existing.UpdateTime = s.now()

	return existing.toProto(), nil
}

// Delete removes a record by name. Returns ErrNotFound if the record does not exist.
func (s *FakeStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.records[name]; !exists {
		return ErrNotFound
	}

	delete(s.records, name)

	return nil
}

// isUnderParent checks whether a resource name falls under the given parent prefix.
func isUnderParent(name, prefix string) bool {
	return len(name) > len(prefix) && name[:len(prefix)] == prefix
}
