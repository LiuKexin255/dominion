package handler

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/memory/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// inMemoryWikiRepo implements domain.WikiRepository for testing. The identity
// is the (template, wiki_id) tuple; a normalized title is unique within a
// template scope, like the storage unique indexes.
type inMemoryWikiRepo struct {
	mu    sync.Mutex
	wikis map[string]*domain.Wiki

	// lastListCursor / lastListPageSize record the most recent ListWikis
	// inputs so the handler tests can assert token decoding and the resolved
	// page size.
	lastListCursor   *domain.WikiListPageToken
	lastListPageSize int
	// lastSearchQuery records the normalized query the handler passed through.
	lastSearchQuery string
}

func newInMemoryWikiRepo() *inMemoryWikiRepo {
	return &inMemoryWikiRepo{wikis: make(map[string]*domain.Wiki)}
}

func wikiKey(template, wikiID string) string {
	return template + "\x00" + wikiID
}

func (r *inMemoryWikiRepo) CreateWiki(_ context.Context, wiki *domain.Wiki) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.wikis[wikiKey(wiki.Template, wiki.WikiID)]; exists {
		return domain.ErrAlreadyExists
	}
	titleNorm := domain.NormalizeTitle(wiki.Title)
	for _, existing := range r.wikis {
		if existing.Template == wiki.Template && domain.NormalizeTitle(existing.Title) == titleNorm {
			return domain.ErrAlreadyExists
		}
	}
	r.wikis[wikiKey(wiki.Template, wiki.WikiID)] = wiki
	return nil
}

func (r *inMemoryWikiRepo) GetWiki(_ context.Context, template, wikiID string) (*domain.Wiki, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wiki, ok := r.wikis[wikiKey(template, wikiID)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return wiki, nil
}

func (r *inMemoryWikiRepo) UpdateWiki(_ context.Context, wiki *domain.Wiki) (*domain.Wiki, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := wikiKey(wiki.Template, wiki.WikiID)
	existing, ok := r.wikis[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	titleNorm := domain.NormalizeTitle(wiki.Title)
	for otherKey, other := range r.wikis {
		if otherKey == key {
			continue
		}
		if other.Template == wiki.Template && domain.NormalizeTitle(other.Title) == titleNorm {
			return nil, domain.ErrAlreadyExists
		}
	}
	clone := *wiki
	clone.CreateTime = existing.CreateTime
	r.wikis[key] = &clone
	return &clone, nil
}

func (r *inMemoryWikiRepo) DeleteWiki(_ context.Context, template, wikiID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := wikiKey(template, wikiID)
	if _, ok := r.wikis[key]; !ok {
		return domain.ErrNotFound
	}
	delete(r.wikis, key)
	return nil
}

func (r *inMemoryWikiRepo) ListWikis(_ context.Context, template string, cursor *domain.WikiListPageToken, pageSize int) ([]*domain.Wiki, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastListCursor = cursor
	r.lastListPageSize = pageSize

	var scoped []*domain.Wiki
	for _, wiki := range r.wikis {
		if wiki.Template == template {
			scoped = append(scoped, wiki)
		}
	}
	sort.Slice(scoped, func(i, j int) bool { return scoped[i].WikiID < scoped[j].WikiID })

	if cursor != nil {
		start := sort.Search(len(scoped), func(i int) bool { return scoped[i].WikiID > cursor.WikiID })
		scoped = scoped[start:]
	}

	if len(scoped) > pageSize {
		next := domain.EncodeWikiListPageToken(&domain.WikiListPageToken{WikiID: scoped[pageSize-1].WikiID})
		return scoped[:pageSize], next, nil
	}
	return scoped, "", nil
}

func (r *inMemoryWikiRepo) SearchWikis(_ context.Context, template, q string) ([]*domain.WikiSearchHit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSearchQuery = q

	var hits []*domain.WikiSearchHit
	for _, wiki := range r.wikis {
		if wiki.Template != template {
			continue
		}
		hits = append(hits, &domain.WikiSearchHit{
			WikiID:    wiki.WikiID,
			Title:     wiki.Title,
			TitleNorm: domain.NormalizeTitle(wiki.Title),
		})
	}
	return hits, nil
}

// wikiBody returns a proto Wiki with the given title and content.
func wikiBody(title, content string) *game.Wiki {
	return &game.Wiki{Title: title, Content: content}
}

func TestWikiService_CreateGetList(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryWikiRepo()
	h := NewWikiHandler(repo)

	// when - create
	created, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "wiki-1",
		Wiki:   wikiBody("策略复盘", "知识正文"),
	})

	// then
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "templates/saolei/wikis/wiki-1" {
		t.Fatalf("CreateWiki() name = %q, want the resource name", created.GetName())
	}
	if created.GetTitle() != "策略复盘" || created.GetContent() != "知识正文" {
		t.Fatalf("CreateWiki() = %+v, want the posted title/content", created)
	}
	if created.GetCreateTime() == nil || created.GetUpdateTime() == nil {
		t.Fatal("CreateWiki() create_time/update_time are nil, want set")
	}

	// when - get
	got, err := h.GetWiki(ctx, &game.GetWikiRequest{Name: created.GetName()})

	// then
	assertStatusCode(t, err, codes.OK)
	if got.GetContent() != "知识正文" {
		t.Fatalf("GetWiki() content = %q, want %q", got.GetContent(), "知识正文")
	}

	// when - list
	list, err := h.ListWikis(ctx, &game.ListWikisRequest{Parent: "templates/saolei"})

	// then
	assertStatusCode(t, err, codes.OK)
	if len(list.GetWikis()) != 1 || list.GetWikis()[0].GetName() != created.GetName() {
		t.Fatalf("ListWikis() = %+v, want the created wiki", list.GetWikis())
	}
	if list.GetNextPageToken() != "" {
		t.Fatalf("ListWikis() next_page_token = %q, want empty", list.GetNextPageToken())
	}
}

func TestWikiService_CreateValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		req  *game.CreateWikiRequest
	}{
		{
			name: "invalid parent without templates prefix",
			req:  &game.CreateWikiRequest{Parent: "sessions/session-1", WikiId: "wiki-1", Wiki: wikiBody("t", "c")},
		},
		{
			name: "unknown template",
			req:  &game.CreateWikiRequest{Parent: "templates/unknown", WikiId: "wiki-1", Wiki: wikiBody("t", "c")},
		},
		{
			name: "upper-case wiki_id",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "Wiki-1", Wiki: wikiBody("t", "c")},
		},
		{
			name: "wiki_id with slash",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "wiki/1", Wiki: wikiBody("t", "c")},
		},
		{
			name: "empty wiki_id",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", Wiki: wikiBody("t", "c")},
		},
		{
			name: "missing wiki body",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "wiki-1"},
		},
		{
			name: "empty title",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "wiki-1", Wiki: wikiBody("", "c")},
		},
		{
			name: "title over the rune limit",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "wiki-1", Wiki: wikiBody(strings.Repeat("中", domain.MaxWikiTitleRunes+1), "c")},
		},
		{
			name: "empty content",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "wiki-1", Wiki: wikiBody("t", "")},
		},
		{
			name: "content over the byte limit",
			req:  &game.CreateWikiRequest{Parent: "templates/saolei", WikiId: "wiki-1", Wiki: wikiBody("t", strings.Repeat("x", domain.MaxWikiContentBytes+1))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewWikiHandler(newInMemoryWikiRepo())

			// when
			_, err := h.CreateWiki(ctx, tt.req)

			// then
			assertStatusCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestWikiService_CreateAlreadyExists(t *testing.T) {
	ctx := context.Background()

	// given
	h := NewWikiHandler(newInMemoryWikiRepo())
	_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "wiki-1",
		Wiki:   wikiBody("ＡＢＣ", "c"),
	})
	assertStatusCode(t, err, codes.OK)

	// when - duplicate wiki id
	_, err = h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "wiki-1",
		Wiki:   wikiBody("other", "c"),
	})

	// then
	assertStatusCode(t, err, codes.AlreadyExists)

	// when - a full-width spelling normalizing to the existing title
	_, err = h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "wiki-2",
		Wiki:   wikiBody("abc", "c"),
	})

	// then - title uniqueness is enforced across the whole template scope
	assertStatusCode(t, err, codes.AlreadyExists)
}

func TestWikiService_Update(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryWikiRepo()
	h := NewWikiHandler(repo)
	created, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "wiki-1",
		Wiki:   wikiBody("旧标题", "旧内容"),
	})
	assertStatusCode(t, err, codes.OK)

	// when - update title only via update_mask
	updated, err := h.UpdateWiki(ctx, &game.UpdateWikiRequest{
		Wiki:       &game.Wiki{Name: created.GetName(), Title: "新标题", Content: "ignored"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})

	// then - title changed, content preserved
	assertStatusCode(t, err, codes.OK)
	if updated.GetTitle() != "新标题" {
		t.Fatalf("UpdateWiki() title = %q, want %q", updated.GetTitle(), "新标题")
	}
	if updated.GetContent() != "旧内容" {
		t.Fatalf("UpdateWiki() content = %q, want the preserved content", updated.GetContent())
	}

	// when - update content only via update_mask
	updated, err = h.UpdateWiki(ctx, &game.UpdateWikiRequest{
		Wiki:       &game.Wiki{Name: created.GetName(), Content: "新内容"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
	})

	// then - content changed, title preserved, create_time stable
	assertStatusCode(t, err, codes.OK)
	if updated.GetTitle() != "新标题" || updated.GetContent() != "新内容" {
		t.Fatalf("UpdateWiki() = %+v, want new content with preserved title", updated)
	}
	if !updated.GetCreateTime().AsTime().Equal(created.GetCreateTime().AsTime()) {
		t.Fatalf("UpdateWiki() create_time changed: got %v, want %v", updated.GetCreateTime(), created.GetCreateTime())
	}

	// when - omitted mask updates both populated fields
	updated, err = h.UpdateWiki(ctx, &game.UpdateWikiRequest{
		Wiki: &game.Wiki{Name: created.GetName(), Title: "全部标题", Content: "全部内容"},
	})

	// then
	assertStatusCode(t, err, codes.OK)
	if updated.GetTitle() != "全部标题" || updated.GetContent() != "全部内容" {
		t.Fatalf("UpdateWiki() = %+v, want both fields updated", updated)
	}
}

func TestWikiService_UpdateValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		req      *game.UpdateWikiRequest
		wantCode codes.Code
	}{
		{
			name:     "missing wiki body",
			req:      &game.UpdateWikiRequest{},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "malformed name",
			req: &game.UpdateWikiRequest{
				Wiki:       &game.Wiki{Name: "templates/saolei", Title: "t"},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "unknown template",
			req: &game.UpdateWikiRequest{
				Wiki:       &game.Wiki{Name: "templates/unknown/wikis/wiki-1", Title: "t"},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "unknown mask path",
			req: &game.UpdateWikiRequest{
				Wiki:       &game.Wiki{Name: "templates/saolei/wikis/missing", Title: "t"},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"bogus"}},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "empty title when title is updated",
			req: &game.UpdateWikiRequest{
				Wiki:       &game.Wiki{Name: "templates/saolei/wikis/missing", Title: ""},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "empty content when content is updated",
			req: &game.UpdateWikiRequest{
				Wiki:       &game.Wiki{Name: "templates/saolei/wikis/missing", Content: ""},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "missing resource",
			req: &game.UpdateWikiRequest{
				Wiki:       &game.Wiki{Name: "templates/saolei/wikis/missing", Content: "c"},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewWikiHandler(newInMemoryWikiRepo())

			// when
			_, err := h.UpdateWiki(ctx, tt.req)

			// then
			assertStatusCode(t, err, tt.wantCode)
		})
	}
}

func TestWikiService_UpdateTitleCollision(t *testing.T) {
	ctx := context.Background()

	// given - two wikis in one template scope
	h := NewWikiHandler(newInMemoryWikiRepo())
	for _, seed := range []struct{ id, title string }{{"wiki-1", "策略"}, {"wiki-2", "开局"}} {
		_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
			Parent: "templates/saolei",
			WikiId: seed.id,
			Wiki:   wikiBody(seed.title, "c"),
		})
		assertStatusCode(t, err, codes.OK)
	}

	// when - rename wiki-2 to wiki-1's title
	_, err := h.UpdateWiki(ctx, &game.UpdateWikiRequest{
		Wiki:       &game.Wiki{Name: "templates/saolei/wikis/wiki-2", Title: "策略"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})

	// then
	assertStatusCode(t, err, codes.AlreadyExists)
}

func TestWikiService_Delete(t *testing.T) {
	ctx := context.Background()

	// given
	h := NewWikiHandler(newInMemoryWikiRepo())
	created, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "to-delete",
		Wiki:   wikiBody("t", "c"),
	})
	assertStatusCode(t, err, codes.OK)

	// when
	_, err = h.DeleteWiki(ctx, &game.DeleteWikiRequest{Name: created.GetName()})

	// then
	assertStatusCode(t, err, codes.OK)

	// when - delete again
	_, err = h.DeleteWiki(ctx, &game.DeleteWikiRequest{Name: created.GetName()})

	// then
	assertStatusCode(t, err, codes.NotFound)
}

func TestWikiService_ListPagination(t *testing.T) {
	ctx := context.Background()

	// given - three wikis
	repo := newInMemoryWikiRepo()
	h := NewWikiHandler(repo)
	for _, id := range []string{"alpha", "bravo", "charlie"} {
		_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
			Parent: "templates/saolei",
			WikiId: id,
			Wiki:   wikiBody(id, "content-"+id),
		})
		assertStatusCode(t, err, codes.OK)
	}

	// when - first page with page_size=2
	page, err := h.ListWikis(ctx, &game.ListWikisRequest{Parent: "templates/saolei", PageSize: 2})

	// then
	assertStatusCode(t, err, codes.OK)
	if len(page.GetWikis()) != 2 || page.GetWikis()[0].GetTitle() != "alpha" || page.GetWikis()[1].GetTitle() != "bravo" {
		t.Fatalf("ListWikis() page = %+v, want wiki_id ascending alpha/bravo", page.GetWikis())
	}
	if page.GetNextPageToken() == "" {
		t.Fatal("ListWikis() first page next_page_token is empty, want non-empty")
	}

	// when - second page with the token
	second, err := h.ListWikis(ctx, &game.ListWikisRequest{Parent: "templates/saolei", PageSize: 2, PageToken: page.GetNextPageToken()})

	// then
	assertStatusCode(t, err, codes.OK)
	if len(second.GetWikis()) != 1 || second.GetWikis()[0].GetTitle() != "charlie" {
		t.Fatalf("ListWikis() page 2 = %+v, want charlie", second.GetWikis())
	}
	if repo.lastListCursor == nil || repo.lastListCursor.WikiID != "bravo" {
		t.Fatalf("ListWikis() cursor = %+v, want the decoded bravo cursor", repo.lastListCursor)
	}
}

func TestWikiService_ListPageSize(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		pageSize int32
		want     int
	}{
		{name: "zero takes the default", pageSize: 0, want: domain.DefaultWikiPageSize},
		{name: "explicit size passes through", pageSize: 5, want: 5},
		{name: "above the maximum is coerced down", pageSize: 500, want: domain.MaxWikiPageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newInMemoryWikiRepo()
			h := NewWikiHandler(repo)

			// when
			_, err := h.ListWikis(ctx, &game.ListWikisRequest{Parent: "templates/saolei", PageSize: tt.pageSize})

			// then
			assertStatusCode(t, err, codes.OK)
			if repo.lastListPageSize != tt.want {
				t.Fatalf("ListWikis() page size = %d, want %d", repo.lastListPageSize, tt.want)
			}
		})
	}
}

func TestWikiService_ListValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		req  *game.ListWikisRequest
	}{
		{name: "invalid parent", req: &game.ListWikisRequest{Parent: "sessions/session-1"}},
		{name: "unknown template", req: &game.ListWikisRequest{Parent: "templates/unknown"}},
		{name: "negative page size", req: &game.ListWikisRequest{Parent: "templates/saolei", PageSize: -1}},
		{name: "malformed page token", req: &game.ListWikisRequest{Parent: "templates/saolei", PageToken: "not-a-token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewWikiHandler(newInMemoryWikiRepo())

			// when
			_, err := h.ListWikis(ctx, tt.req)

			// then
			assertStatusCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestWikiService_Search(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryWikiRepo()
	h := NewWikiHandler(repo)
	for _, seed := range []struct{ id, title string }{
		{"wiki-1", "策略复盘"},
		{"wiki-2", "策略"},
		{"wiki-3", "中期策略复盘"},
		{"wiki-4", "无关条目"},
	} {
		_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
			Parent: "templates/saolei",
			WikiId: seed.id,
			Wiki:   wikiBody(seed.title, "content-"+seed.id),
		})
		assertStatusCode(t, err, codes.OK)
	}

	// when - full-width query normalizes and matches
	resp, err := h.SearchWikis(ctx, &game.SearchWikisRequest{Parent: "templates/saolei", Query: "策略"})

	// then - exact > prefix > substring; content never returned
	assertStatusCode(t, err, codes.OK)
	wantTitles := []string{"策略", "策略复盘", "中期策略复盘"}
	if len(resp.GetWikis()) != len(wantTitles) {
		t.Fatalf("SearchWikis() returned %d entries, want %d", len(resp.GetWikis()), len(wantTitles))
	}
	for i, want := range wantTitles {
		if resp.GetWikis()[i].GetTitle() != want {
			t.Fatalf("SearchWikis()[%d] title = %q, want %q (exact > prefix > substring)", i, resp.GetWikis()[i].GetTitle(), want)
		}
	}
	if repo.lastSearchQuery != domain.NormalizeTitle("策略") {
		t.Fatalf("SearchWikis() query = %q, want the normalized query", repo.lastSearchQuery)
	}

	// when - a full-width spelling normalizes before reaching the repository
	_, err = h.SearchWikis(ctx, &game.SearchWikisRequest{Parent: "templates/saolei", Query: "ＡＢＣ"})

	// then
	assertStatusCode(t, err, codes.OK)
	if repo.lastSearchQuery != "abc" {
		t.Fatalf("SearchWikis() normalized query = %q, want %q", repo.lastSearchQuery, "abc")
	}
}

func TestWikiService_SearchShortQuery(t *testing.T) {
	ctx := context.Background()

	// given - a single-character query
	repo := newInMemoryWikiRepo()
	h := NewWikiHandler(repo)
	for _, seed := range []struct{ id, title string }{
		{"wiki-1", "策略"},
		{"wiki-2", "策略复盘"},
		{"wiki-3", "无关"},
	} {
		_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
			Parent: "templates/saolei",
			WikiId: seed.id,
			Wiki:   wikiBody(seed.title, "c"),
		})
		assertStatusCode(t, err, codes.OK)
	}

	// when
	resp, err := h.SearchWikis(ctx, &game.SearchWikisRequest{Parent: "templates/saolei", Query: "策"})

	// then - prefix matches first (shorter normalized title first), substring next
	assertStatusCode(t, err, codes.OK)
	wantTitles := []string{"策略", "策略复盘"}
	if len(resp.GetWikis()) != len(wantTitles) {
		t.Fatalf("SearchWikis() returned %d entries, want %d", len(resp.GetWikis()), len(wantTitles))
	}
	for i, want := range wantTitles {
		if resp.GetWikis()[i].GetTitle() != want {
			t.Fatalf("SearchWikis()[%d] title = %q, want %q", i, resp.GetWikis()[i].GetTitle(), want)
		}
	}
}

func TestWikiService_SearchPagination(t *testing.T) {
	ctx := context.Background()

	// given - three matching titles
	h := NewWikiHandler(newInMemoryWikiRepo())
	for _, seed := range []struct{ id, title string }{
		{"wiki-1", "策略"},
		{"wiki-2", "策略复盘"},
		{"wiki-3", "中期策略复盘"},
	} {
		_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
			Parent: "templates/saolei",
			WikiId: seed.id,
			Wiki:   wikiBody(seed.title, "c"),
		})
		assertStatusCode(t, err, codes.OK)
	}

	// when - first page with page_size=1
	page, err := h.SearchWikis(ctx, &game.SearchWikisRequest{Parent: "templates/saolei", Query: "策略", PageSize: 1})

	// then
	assertStatusCode(t, err, codes.OK)
	if len(page.GetWikis()) != 1 || page.GetWikis()[0].GetTitle() != "策略" {
		t.Fatalf("SearchWikis() page = %+v, want the exact match", page.GetWikis())
	}
	if page.GetNextPageToken() == "" {
		t.Fatal("SearchWikis() first page next_page_token is empty, want non-empty")
	}

	// when - second page with the token and the same query
	second, err := h.SearchWikis(ctx, &game.SearchWikisRequest{
		Parent:    "templates/saolei",
		Query:     "策略",
		PageSize:  1,
		PageToken: page.GetNextPageToken(),
	})

	// then
	assertStatusCode(t, err, codes.OK)
	if len(second.GetWikis()) != 1 || second.GetWikis()[0].GetTitle() != "策略复盘" {
		t.Fatalf("SearchWikis() page 2 = %+v, want the prefix match", second.GetWikis())
	}

	// when - a continuation with a different query
	_, err = h.SearchWikis(ctx, &game.SearchWikisRequest{
		Parent:    "templates/saolei",
		Query:     "开局",
		PageToken: page.GetNextPageToken(),
	})

	// then - query mismatch is INVALID_ARGUMENT
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestWikiService_SearchValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		req  *game.SearchWikisRequest
	}{
		{name: "invalid parent", req: &game.SearchWikisRequest{Parent: "sessions/session-1", Query: "x"}},
		{name: "unknown template", req: &game.SearchWikisRequest{Parent: "templates/unknown", Query: "x"}},
		{name: "empty query", req: &game.SearchWikisRequest{Parent: "templates/saolei"}},
		{name: "negative page size", req: &game.SearchWikisRequest{Parent: "templates/saolei", Query: "x", PageSize: -1}},
		{name: "malformed page token", req: &game.SearchWikisRequest{Parent: "templates/saolei", Query: "x", PageToken: "not-a-token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewWikiHandler(newInMemoryWikiRepo())

			// when
			_, err := h.SearchWikis(ctx, tt.req)

			// then
			assertStatusCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestWikiService_SearchResponseShape(t *testing.T) {
	ctx := context.Background()

	// given
	h := NewWikiHandler(newInMemoryWikiRepo())
	_, err := h.CreateWiki(ctx, &game.CreateWikiRequest{
		Parent: "templates/saolei",
		WikiId: "wiki-1",
		Wiki:   wikiBody("策略复盘", "secret"),
	})
	assertStatusCode(t, err, codes.OK)

	// when
	resp, err := h.SearchWikis(ctx, &game.SearchWikisRequest{Parent: "templates/saolei", Query: "策略"})

	// then - the lightweight entry carries name and title only
	assertStatusCode(t, err, codes.OK)
	if len(resp.GetWikis()) != 1 {
		t.Fatalf("SearchWikis() returned %d entries, want 1", len(resp.GetWikis()))
	}
	got := resp.GetWikis()[0]
	if got.GetName() != "templates/saolei/wikis/wiki-1" {
		t.Fatalf("SearchWikis() name = %q, want the resource name", got.GetName())
	}
	if got.GetTitle() != "策略复盘" {
		t.Fatalf("SearchWikis() title = %q, want %q", got.GetTitle(), "策略复盘")
	}
}

func Test_wikiPageSize(t *testing.T) {
	tests := []struct {
		name     string
		pageSize int32
		want     int
		wantErr  bool
	}{
		{name: "zero takes the default", pageSize: 0, want: domain.DefaultWikiPageSize},
		{name: "explicit size passes through", pageSize: 42, want: 42},
		{name: "maximum passes through", pageSize: domain.MaxWikiPageSize, want: domain.MaxWikiPageSize},
		{name: "above the maximum is coerced", pageSize: domain.MaxWikiPageSize + 1, want: domain.MaxWikiPageSize},
		{name: "negative is invalid", pageSize: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := wikiPageSize(tt.pageSize)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("wikiPageSize() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("wikiPageSize() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("wikiPageSize(%d) = %d, want %d", tt.pageSize, got, tt.want)
			}
		})
	}
}

func Test_wikiToProto(t *testing.T) {
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		wiki     *domain.Wiki
		wantNil  bool
		wantName string
	}{
		{
			name:    "nil wiki returns nil",
			wiki:    nil,
			wantNil: true,
		},
		{
			name: "wiki with fields",
			wiki: &domain.Wiki{
				Template:   "saolei",
				WikiID:     "wiki-1",
				Title:      "策略复盘",
				Content:    "知识正文",
				CreateTime: now,
				UpdateTime: now,
			},
			wantName: "templates/saolei/wikis/wiki-1",
		},
		{
			name: "wiki with zero times has no timestamps",
			wiki: &domain.Wiki{
				Template: "saolei",
				WikiID:   "notime",
			},
			wantName: "templates/saolei/wikis/notime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := wikiToProto(tt.wiki)

			// then
			if tt.wantNil {
				if got != nil {
					t.Fatalf("wikiToProto() = %v, want nil", got)
				}
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("wikiToProto() name = %q, want %q", got.GetName(), tt.wantName)
			}
			if got.GetTitle() != tt.wiki.Title || got.GetContent() != tt.wiki.Content {
				t.Fatalf("wikiToProto() = %+v, want the wiki title/content", got)
			}
			if !tt.wiki.CreateTime.IsZero() && got.GetCreateTime() == nil {
				t.Fatal("wikiToProto() create_time is nil, want set")
			}
			if !tt.wiki.UpdateTime.IsZero() && got.GetUpdateTime() == nil {
				t.Fatal("wikiToProto() update_time is nil, want set")
			}
		})
	}
}
