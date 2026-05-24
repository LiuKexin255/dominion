package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"dominion/projects/game/session/domain"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// fakeSingleResult implements singleResult for in-memory testing.
type fakeSingleResult struct {
	doc *sessionDocument
	err error
}

func (r *fakeSingleResult) Decode(v interface{}) error {
	if r.err != nil {
		return r.err
	}
	ptr, ok := v.(*sessionDocument)
	if !ok {
		return errors.New("invalid decode target type")
	}
	*ptr = *r.doc
	return nil
}

// fakeCollection implements collectionOps with in-memory storage.
type fakeCollection struct {
	docs map[string]*sessionDocument
}

func newFakeCollection() *fakeCollection {
	return &fakeCollection{
		docs: map[string]*sessionDocument{},
	}
}

func (c *fakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	doc, ok := document.(*sessionDocument)
	if !ok {
		return nil, errors.New("invalid document type")
	}
	if _, exists := c.docs[doc.Name]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{
				{Code: 11000, Message: "duplicate key error"},
			},
		}
	}
	c.docs[doc.Name] = doc
	return &mongodriver.InsertOneResult{}, nil
}

func (c *fakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(sessionNameFilter)
	if !ok {
		return &fakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[f.Name]
	if !exists {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &fakeSingleResult{doc: doc}
}

func (c *fakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(sessionNameFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	if _, exists := c.docs[f.Name]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, f.Name)
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

// newTestRepo creates a sessionRepository backed by a fakeCollection.
func newTestRepo() *sessionRepository {
	return &sessionRepository{
		collection: newFakeCollection(),
	}
}

func TestCreateSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		session    *domain.Session
		wantName   string
		wantID     string
		wantErr    bool
		wantErrTyp error
	}{
		{
			name: "success",
			session: &domain.Session{
				Name:      "sessions/abc123",
				SessionID: "abc123",
			},
			wantName: "sessions/abc123",
			wantID:   "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newTestRepo()

			// when
			got, err := repo.Create(ctx, tt.session)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Create() expected error, got nil")
				}
				if tt.wantErrTyp != nil && !errors.Is(err, tt.wantErrTyp) {
					t.Fatalf("Create() error = %v, want %v", err, tt.wantErrTyp)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
			if got.Name != tt.wantName {
				t.Fatalf("Create() name = %q, want %q", got.Name, tt.wantName)
			}
			if got.SessionID != tt.wantID {
				t.Fatalf("Create() session_id = %q, want %q", got.SessionID, tt.wantID)
			}
			if got.CreateTime.IsZero() {
				t.Fatalf("Create() create_time is zero, expected non-zero timestamp")
			}
		})
	}
}

func TestGetSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		seed       *domain.Session
		getName    string
		wantName   string
		wantID     string
		wantErr    bool
		wantErrTyp error
	}{
		{
			name:     "success - fields match after create then get",
			seed:     &domain.Session{Name: "sessions/abc123", SessionID: "abc123"},
			getName:  "sessions/abc123",
			wantName: "sessions/abc123",
			wantID:   "abc123",
		},
		{
			name:       "not found",
			seed:       &domain.Session{Name: "sessions/abc123", SessionID: "abc123"},
			getName:    "sessions/missing",
			wantErr:    true,
			wantErrTyp: domain.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newTestRepo()
			_, err := repo.Create(ctx, tt.seed)
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}

			// when
			got, err := repo.Get(ctx, tt.getName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Get() expected error, got nil")
				}
				if tt.wantErrTyp != nil && !errors.Is(err, tt.wantErrTyp) {
					t.Fatalf("Get() error = %v, want %v", err, tt.wantErrTyp)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get() unexpected error: %v", err)
			}
			if got.Name != tt.wantName {
				t.Fatalf("Get() name = %q, want %q", got.Name, tt.wantName)
			}
			if got.SessionID != tt.wantID {
				t.Fatalf("Get() session_id = %q, want %q", got.SessionID, tt.wantID)
			}
		})
	}
}

func TestCreateSessionAlreadyExists(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	session := &domain.Session{
		Name:      "sessions/abc123",
		SessionID: "abc123",
	}
	_, err := repo.Create(ctx, session)
	if err != nil {
		t.Fatalf("Create() first insert unexpected error: %v", err)
	}

	// when
	_, err = repo.Create(ctx, session)

	// then
	if err == nil {
		t.Fatalf("Create() duplicate expected error, got nil")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("Create() error = %v, want ErrAlreadyExists", err)
	}
}

func TestDeleteSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		seed       *domain.Session
		deleteName string
		wantErr    bool
		wantErrTyp error
	}{
		{
			name:       "success - delete existing session",
			seed:       &domain.Session{Name: "sessions/abc123", SessionID: "abc123"},
			deleteName: "sessions/abc123",
		},
		{
			name:       "not found - delete non-existent session",
			seed:       &domain.Session{Name: "sessions/abc123", SessionID: "abc123"},
			deleteName: "sessions/missing",
			wantErr:    true,
			wantErrTyp: domain.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newTestRepo()
			_, err := repo.Create(ctx, tt.seed)
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}

			// when
			err = repo.Delete(ctx, tt.deleteName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Delete() expected error, got nil")
				}
				if tt.wantErrTyp != nil && !errors.Is(err, tt.wantErrTyp) {
					t.Fatalf("Delete() error = %v, want %v", err, tt.wantErrTyp)
				}
				return
			}
			if err != nil {
				t.Fatalf("Delete() unexpected error: %v", err)
			}
		})
	}
}

func Test_toDomain(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		doc  *sessionDocument
		want *domain.Session
	}{
		{
			name: "full conversion",
			doc: &sessionDocument{
				Name:       "sessions/abc123",
				SessionID:  "abc123",
				CreateTime: now,
			},
			want: &domain.Session{
				Name:       "sessions/abc123",
				SessionID:  "abc123",
				CreateTime: now,
			},
		},
		{
			name: "nil document returns nil",
			doc:  nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := tt.doc.toDomain()

			// then
			if got == nil && tt.want == nil {
				return
			}
			if got == nil || tt.want == nil {
				t.Fatalf("toDomain() = %v, want %v", got, tt.want)
			}
			if got.Name != tt.want.Name {
				t.Fatalf("toDomain() name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.SessionID != tt.want.SessionID {
				t.Fatalf("toDomain() session_id = %q, want %q", got.SessionID, tt.want.SessionID)
			}
			if !got.CreateTime.Equal(tt.want.CreateTime) {
				t.Fatalf("toDomain() create_time = %v, want %v", got.CreateTime, tt.want.CreateTime)
			}
		})
	}
}

func Test_fromDomain(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		session *domain.Session
		want    *sessionDocument
	}{
		{
			name: "full conversion",
			session: &domain.Session{
				Name:       "sessions/abc123",
				SessionID:  "abc123",
				CreateTime: now,
			},
			want: &sessionDocument{
				Name:       "sessions/abc123",
				SessionID:  "abc123",
				CreateTime: now,
			},
		},
		{
			name:    "nil session returns nil",
			session: nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := sessionDocumentFromDomain(tt.session)

			// then
			if got == nil && tt.want == nil {
				return
			}
			if got == nil || tt.want == nil {
				t.Fatalf("sessionDocumentFromDomain() = %v, want %v", got, tt.want)
			}
			if got.Name != tt.want.Name {
				t.Fatalf("sessionDocumentFromDomain() name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.SessionID != tt.want.SessionID {
				t.Fatalf("sessionDocumentFromDomain() session_id = %q, want %q", got.SessionID, tt.want.SessionID)
			}
			if !got.CreateTime.Equal(tt.want.CreateTime) {
				t.Fatalf("sessionDocumentFromDomain() create_time = %v, want %v", got.CreateTime, tt.want.CreateTime)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		session *domain.Session
	}{
		{
			name: "domain to document and back",
			session: &domain.Session{
				Name:       "sessions/roundtrip",
				SessionID:  "roundtrip",
				CreateTime: now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given - session is already in tt.session

			// when
			doc := sessionDocumentFromDomain(tt.session)
			got := doc.toDomain()

			// then
			if got.Name != tt.session.Name {
				t.Fatalf("round-trip name = %q, want %q", got.Name, tt.session.Name)
			}
			if got.SessionID != tt.session.SessionID {
				t.Fatalf("round-trip session_id = %q, want %q", got.SessionID, tt.session.SessionID)
			}
			if !got.CreateTime.Equal(tt.session.CreateTime) {
				t.Fatalf("round-trip create_time = %v, want %v", got.CreateTime, tt.session.CreateTime)
			}
		})
	}
}
