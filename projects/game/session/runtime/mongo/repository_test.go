package mongo

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"dominion/projects/game/session/domain"

	"go.mongodb.org/mongo-driver/bson"
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
	docs      map[string]*sessionDocument
	docsOrder []string
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
	if _, exists := c.docs[doc.SessionID]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{
				{Code: 11000, Message: "duplicate key error"},
			},
		}
	}
	c.docs[doc.SessionID] = doc
	c.docsOrder = append(c.docsOrder, doc.SessionID)
	return &mongodriver.InsertOneResult{}, nil
}

func (c *fakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(sessionFilter)
	if !ok {
		return &fakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[f.SessionID]
	if !exists {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &fakeSingleResult{doc: doc}
}

func (c *fakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(sessionFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	if _, exists := c.docs[f.SessionID]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, f.SessionID)
	for i, id := range c.docsOrder {
		if id == f.SessionID {
			c.docsOrder = append(c.docsOrder[:i], c.docsOrder[i+1:]...)
			break
		}
	}
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (c *fakeCollection) Indexes() mongodriver.IndexView {
	return mongodriver.IndexView{}
}

// matchOrCondition evaluates whether a doc matches any condition in a $or filter.
// Each condition is a bson.M with create_time and/or session_id constraints.
func (c *fakeCollection) matchOrCondition(doc *sessionDocument, orList bson.A) bool {
	for _, cond := range orList {
		condMap, ok := cond.(bson.M)
		if !ok {
			continue
		}
		if ctVal, hasCT := condMap["create_time"]; hasCT {
			switch ct := ctVal.(type) {
			case bson.M:
				if ltVal, ok2 := ct["$lt"]; ok2 {
					cursorTime := ltVal.(time.Time)
					if doc.CreateTime.Before(cursorTime) {
						return true
					}
				}
			case time.Time:
				if doc.CreateTime.Equal(ct) {
					if sidVal, hasSID := condMap["session_id"]; hasSID {
						if sidMap, ok2 := sidVal.(bson.M); ok2 {
							if ltSID, ok3 := sidMap["$lt"]; ok3 {
								cursorSID := ltSID.(string)
								if doc.SessionID < cursorSID {
									return true
								}
							}
						}
					}
				}
			}
		}
	}
	return false
}

func (c *fakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
	findOpts := options.Find()
	for _, o := range opts {
		if o != nil {
			findOpts = o
		}
	}

	var sortKeys []string
	var sortDirs []int
	if findOpts.Sort != nil {
		if s, ok := findOpts.Sort.(bson.D); ok {
			for _, e := range s {
				sortKeys = append(sortKeys, e.Key)
				sortDirs = append(sortDirs, e.Value.(int))
			}
		}
	}

	var limit int64
	if findOpts.Limit != nil {
		limit = *findOpts.Limit
	}

	var filtered []*sessionDocument
	filterMap, isMap := filter.(bson.M)

	for _, id := range c.docsOrder {
		doc := c.docs[id]

		if isMap {
			if orConditions, hasOr := filterMap["$or"]; hasOr {
				orList, ok := orConditions.(bson.A)
				if !ok {
					filtered = append(filtered, doc)
					continue
				}
				matched := c.matchOrCondition(doc, orList)
				if !matched {
					continue
				}
			}
		}

		filtered = append(filtered, doc)
	}

	if len(sortKeys) > 0 {
		sort.Slice(filtered, func(i, j int) bool {
			for k := 0; k < len(sortKeys); k++ {
				var cmp int
				switch sortKeys[k] {
				case "create_time":
					if filtered[i].CreateTime.Before(filtered[j].CreateTime) {
						cmp = -1
					} else if filtered[i].CreateTime.After(filtered[j].CreateTime) {
						cmp = 1
					}
				case "session_id":
					si, sj := filtered[i].SessionID, filtered[j].SessionID
					if si < sj {
						cmp = -1
					} else if si > sj {
						cmp = 1
					}
				default:
					continue
				}
				if sortDirs[k] == -1 {
					cmp = -cmp
				}
				if cmp != 0 {
					return cmp < 0
				}
			}
			return false
		})
	}

	if limit > 0 && int64(len(filtered)) > limit {
		filtered = filtered[:limit]
	}

	return &fakeCursor{docs: filtered}, nil
}

// fakeCursor implements cursorOps with in-memory results.
type fakeCursor struct {
	docs []*sessionDocument
}

func (c *fakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*sessionDocument)
	if !ok {
		return errors.New("invalid results target type")
	}
	*ptr = c.docs
	return nil
}

func (c *fakeCursor) Close(_ context.Context) error {
	return nil
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
		wantID     string
		wantErr    bool
		wantErrTyp error
	}{
		{
			name: "success",
			session: &domain.Session{
				SessionID: "abc123",
			},
			wantID: "abc123",
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
		getID      string
		wantID     string
		wantErr    bool
		wantErrTyp error
	}{
		{
			name:   "success - fields match after create then get",
			seed:   &domain.Session{SessionID: "abc123"},
			getID:  "abc123",
			wantID: "abc123",
		},
		{
			name:       "not found",
			seed:       &domain.Session{SessionID: "abc123"},
			getID:      "missing",
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
			got, err := repo.Get(ctx, tt.getID)

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
		deleteID   string
		wantErr    bool
		wantErrTyp error
	}{
		{
			name:     "success - delete existing session",
			seed:     &domain.Session{SessionID: "abc123"},
			deleteID: "abc123",
		},
		{
			name:       "not found - delete non-existent session",
			seed:       &domain.Session{SessionID: "abc123"},
			deleteID:   "missing",
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
			err = repo.Delete(ctx, tt.deleteID)

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
			name: "session_id and create_time are preserved",
			doc: &sessionDocument{
				SessionID:  "abc123",
				CreateTime: now,
			},
			want: &domain.Session{
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
			name: "name is not stored in document",
			session: &domain.Session{
				SessionID:  "abc123",
				CreateTime: now,
			},
			want: &sessionDocument{
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
			name: "domain to document and back - name is no longer in domain model",
			session: &domain.Session{
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
			if got.SessionID != tt.session.SessionID {
				t.Fatalf("round-trip session_id = %q, want %q", got.SessionID, tt.session.SessionID)
			}
			if !got.CreateTime.Equal(tt.session.CreateTime) {
				t.Fatalf("round-trip create_time = %v, want %v", got.CreateTime, tt.session.CreateTime)
			}
		})
	}
}

func TestListSessions(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)

	// given - seed 3 sessions with distinct create times
	repo := newTestRepo()
	_, err := repo.Create(ctx, &domain.Session{SessionID: "session_a", CreateTime: t1})
	if err != nil {
		t.Fatalf("Create() seed unexpected error: %v", err)
	}
	_, err = repo.Create(ctx, &domain.Session{SessionID: "session_b", CreateTime: t2})
	if err != nil {
		t.Fatalf("Create() seed unexpected error: %v", err)
	}
	_, err = repo.Create(ctx, &domain.Session{SessionID: "session_c", CreateTime: t3})
	if err != nil {
		t.Fatalf("Create() seed unexpected error: %v", err)
	}

	// when - first page with pageSize=2
	result, err := repo.List(ctx, 2, nil)

	// then - first page has 2 sessions (DESC: c, b) with next token encoding c
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("List() got %d sessions, want 2", len(result.Sessions))
	}
	if result.Sessions[0].SessionID != "session_c" {
		t.Fatalf("List() first session_id = %q, want %q", result.Sessions[0].SessionID, "session_c")
	}
	if result.Sessions[1].SessionID != "session_b" {
		t.Fatalf("List() second session_id = %q, want %q", result.Sessions[1].SessionID, "session_b")
	}
	if result.NextPageToken == "" {
		t.Fatalf("List() next_page_token is empty, want non-empty")
	}

	// when - second page using cursor from first page
	page2Cursor, err := domain.DecodePageToken(result.NextPageToken)
	if err != nil {
		t.Fatalf("DecodePageToken() unexpected error: %v", err)
	}
	result2, err := repo.List(ctx, 2, page2Cursor)

	// then - second page has 1 session, no next token
	if err != nil {
		t.Fatalf("List() page 2 unexpected error: %v", err)
	}
	if len(result2.Sessions) != 1 {
		t.Fatalf("List() page 2 got %d sessions, want 1", len(result2.Sessions))
	}
	if result2.Sessions[0].SessionID != "session_a" {
		t.Fatalf("List() page 2 session_id = %q, want %q", result2.Sessions[0].SessionID, "session_a")
	}
	if result2.NextPageToken != "" {
		t.Fatalf("List() page 2 next_page_token = %q, want empty", result2.NextPageToken)
	}
}

func TestListSessions_Empty(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		cursor *domain.ListPageCursor
	}{
		{
			name:   "nil cursor returns nil,nil",
			cursor: nil,
		},
		{
			name: "non-nil cursor returns nil,nil",
			cursor: &domain.ListPageCursor{
				CreateTime: t1,
				SessionID:  "session_a",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newTestRepo()

			// when
			result, err := repo.List(ctx, 10, tt.cursor)

			// then
			if err != nil {
				t.Fatalf("List() unexpected error: %v", err)
			}
			if result != nil {
				t.Fatalf("List() result = %v, want nil", result)
			}
		})
	}
}

func TestListSessions_DefaultPageSize(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		pageSize     int
		wantLen      int
		wantNextPage bool
	}{
		{
			name:         "pageSize 1 returns 1 session with next token",
			pageSize:     1,
			wantLen:      1,
			wantNextPage: true,
		},
		{
			name:         "pageSize 2 returns 2 sessions with next token",
			pageSize:     2,
			wantLen:      2,
			wantNextPage: true,
		},
		{
			name:         "pageSize 3 returns all 3 sessions without next token",
			pageSize:     3,
			wantLen:      3,
			wantNextPage: false,
		},
		{
			name:         "pageSize 10 exceeds available sessions, no next token",
			pageSize:     10,
			wantLen:      3,
			wantNextPage: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given - seed 3 sessions
			repo := newTestRepo()
			_, err := repo.Create(ctx, &domain.Session{SessionID: "session_a", CreateTime: t1})
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}
			_, err = repo.Create(ctx, &domain.Session{SessionID: "session_b", CreateTime: t2})
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}
			_, err = repo.Create(ctx, &domain.Session{SessionID: "session_c", CreateTime: t3})
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}

			// when
			result, err := repo.List(ctx, tt.pageSize, nil)

			// then
			if err != nil {
				t.Fatalf("List() unexpected error: %v", err)
			}
			if len(result.Sessions) != tt.wantLen {
				t.Fatalf("List() got %d sessions, want %d", len(result.Sessions), tt.wantLen)
			}
			hasNext := result.NextPageToken != ""
			if hasNext != tt.wantNextPage {
				t.Fatalf("List() next_page_token present = %v, want %v", hasNext, tt.wantNextPage)
			}
		})
	}
}

func TestListSessions_CursorPagination(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		pageSize  int
		wantIDs   []string
		wantToken bool
	}{
		{
			name:      "page through all 3 sessions one at a time",
			pageSize:  1,
			wantIDs:   []string{"session_c"},
			wantToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given - seed 3 sessions with distinct create times
			repo := newTestRepo()
			_, err := repo.Create(ctx, &domain.Session{SessionID: "session_a", CreateTime: t1})
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}
			_, err = repo.Create(ctx, &domain.Session{SessionID: "session_b", CreateTime: t2})
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}
			_, err = repo.Create(ctx, &domain.Session{SessionID: "session_c", CreateTime: t3})
			if err != nil {
				t.Fatalf("Create() seed unexpected error: %v", err)
			}

			var cursor *domain.ListPageCursor
			allIDs := make([]string, 0, 3)

			for page := 0; page < 3; page++ {
				// when
				result, err := repo.List(ctx, 1, cursor)

				// then
				if err != nil {
					t.Fatalf("List() page %d unexpected error: %v", page, err)
				}
				if result == nil {
					// no more results expected
					if page < 3 {
						t.Fatalf("List() page %d returned nil, want non-nil", page)
					}
					break
				}
				if len(result.Sessions) != 1 {
					t.Fatalf("List() page %d got %d sessions, want 1", page, len(result.Sessions))
				}
				allIDs = append(allIDs, result.Sessions[0].SessionID)

				if result.NextPageToken != "" {
					cursor, err = domain.DecodePageToken(result.NextPageToken)
					if err != nil {
						t.Fatalf("DecodePageToken() page %d unexpected error: %v", page, err)
					}
				} else {
					cursor = nil
				}
			}

			// then - expect descending order: c, b, a
			if len(allIDs) != 3 {
				t.Fatalf("List() pagination complete got %d total sessions, want 3", len(allIDs))
			}
			if allIDs[0] != "session_c" || allIDs[1] != "session_b" || allIDs[2] != "session_a" {
				t.Fatalf("List() all page order = %v, want [session_c session_b session_a]", allIDs)
			}
		})
	}
}
