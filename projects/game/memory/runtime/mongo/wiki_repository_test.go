package mongo

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"testing"
	"time"

	"dominion/projects/game/memory/domain"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Fake implementations ---

// wikiFakeSingleResult implements singleResult for in-memory wiki testing.
type wikiFakeSingleResult struct {
	doc *wikiDocument
	err error
}

func (r *wikiFakeSingleResult) Decode(v interface{}) error {
	if r.err != nil {
		return r.err
	}
	target, ok := v.(*wikiDocument)
	if !ok {
		return errors.New("invalid decode target type")
	}
	*target = *r.doc
	return nil
}

// wikiFakeCollection implements collectionOps with in-memory storage for
// wikis. It emulates both unique indexes: an existing (template, wiki_id)
// identity and an existing template-normalized title both surface as a
// duplicate-key write error.
type wikiFakeCollection struct {
	docs     map[string]*wikiDocument
	order    []string
	filters  []bson.M
	findOpts []*options.FindOptions
}

func newWikiFakeCollection() *wikiFakeCollection {
	return &wikiFakeCollection{
		docs: map[string]*wikiDocument{},
	}
}

// wikiDocKey returns the map key for a wiki document identity.
func wikiDocKey(template, wikiID string) string {
	return template + "\x00" + wikiID
}

func (c *wikiFakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	doc, ok := document.(*wikiDocument)
	if !ok {
		return nil, errors.New("invalid document type")
	}
	if _, exists := c.docs[wikiDocKey(doc.Template, doc.WikiID)]; exists {
		return nil, duplicateWriteError()
	}
	for _, existing := range c.docs {
		if existing.Template == doc.Template && existing.TitleNorm == doc.TitleNorm {
			return nil, duplicateWriteError()
		}
	}
	c.docs[wikiDocKey(doc.Template, doc.WikiID)] = doc
	c.order = append(c.order, wikiDocKey(doc.Template, doc.WikiID))
	return &mongodriver.InsertOneResult{}, nil
}

func (c *wikiFakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(wikiFilter)
	if !ok {
		return &wikiFakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[wikiDocKey(f.Template, f.WikiID)]
	if !exists {
		return &wikiFakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &wikiFakeSingleResult{doc: doc}
}

func (c *wikiFakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(wikiFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	key := wikiDocKey(f.Template, f.WikiID)
	if _, exists := c.docs[key]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, key)
	for i, id := range c.order {
		if id == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (c *wikiFakeCollection) ReplaceOne(_ context.Context, filter interface{}, replacement interface{}, _ ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error) {
	f, ok := filter.(wikiFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	doc, ok := replacement.(*wikiDocument)
	if !ok {
		return nil, errors.New("invalid replacement type")
	}
	key := wikiDocKey(f.Template, f.WikiID)
	existing, exists := c.docs[key]
	if !exists {
		return &mongodriver.UpdateResult{MatchedCount: 0, ModifiedCount: 0}, nil
	}
	for otherKey, other := range c.docs {
		if otherKey == key {
			continue
		}
		if other.Template == doc.Template && other.TitleNorm == doc.TitleNorm {
			return nil, duplicateWriteError()
		}
	}
	doc.ID = existing.ID
	c.docs[key] = doc
	return &mongodriver.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, nil
}

func (c *wikiFakeCollection) Indexes() indexViewOps {
	return new(fakeIndexView)
}

func (c *wikiFakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
	findOpts := options.Find()
	for _, o := range opts {
		if o != nil {
			findOpts = o
		}
	}
	c.findOpts = append(c.findOpts, findOpts)

	var limit int64
	if findOpts.Limit != nil {
		limit = *findOpts.Limit
	}

	filterMap, isMap := filter.(bson.M)
	if isMap {
		c.filters = append(c.filters, filterMap)
	}

	var filtered []*wikiDocument
	for _, key := range c.order {
		doc := c.docs[key]
		if isMap && !matchesWikiFilter(filterMap, doc) {
			continue
		}
		filtered = append(filtered, doc)
	}

	if sortSpec, ok := findOpts.Sort.(bson.D); ok {
		sortWikiDocs(filtered, sortSpec)
	}

	if limit > 0 && int64(len(filtered)) > limit {
		filtered = filtered[:limit]
	}

	return &wikiFakeCursor{docs: filtered}, nil
}

// duplicateWriteError builds the duplicate-key error a unique index produces.
func duplicateWriteError() error {
	return mongodriver.WriteException{
		WriteErrors: []mongodriver.WriteError{
			{Code: 11000, Message: "duplicate key error"},
		},
	}
}

// matchesWikiFilter evaluates the filters the wiki repository builds:
// equality on template/wiki_id, a direction-aware wiki_id seek, a
// title_grams $all prefilter and a title_norm $regex fallback.
func matchesWikiFilter(filter bson.M, doc *wikiDocument) bool {
	for field, condition := range filter {
		if !matchesWikiFieldCondition(doc, field, condition) {
			return false
		}
	}
	return true
}

func matchesWikiFieldCondition(doc *wikiDocument, field string, condition any) bool {
	comparison, ok := condition.(bson.M)
	if !ok {
		return wikiDocFieldValue(doc, field) == condition
	}
	for operator, operand := range comparison {
		switch operator {
		case "$gt":
			if !(wikiDocFieldValue(doc, field) > operand.(string)) {
				return false
			}
		case "$all":
			if !wikiTitleGramsContainAll(doc.TitleGrams, operand) {
				return false
			}
		case "$regex":
			pattern, ok := operand.(string)
			if !ok || !regexp.MustCompile(pattern).MatchString(wikiDocFieldValue(doc, field)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// wikiDocFieldValue returns the document's string value for the fields the
// repository filters on. title_grams is handled by the $all branch and has no
// scalar value.
func wikiDocFieldValue(doc *wikiDocument, field string) string {
	switch field {
	case fieldTemplate:
		return doc.Template
	case fieldWikiID:
		return doc.WikiID
	case fieldTitle:
		return doc.Title
	case fieldTitleNorm:
		return doc.TitleNorm
	default:
		panic("wikiDocFieldValue: unknown field " + field)
	}
}

// wikiTitleGramsContainAll reports whether the document's bigram set contains
// every queried gram.
func wikiTitleGramsContainAll(grams []string, operand any) bool {
	want, ok := operand.([]string)
	if !ok {
		return false
	}
	for _, gram := range want {
		found := false
		for _, have := range grams {
			if have == gram {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sortWikiDocs applies the repository's Mongo sort spec over wiki documents
// (the wiki repository sorts by wiki_id only).
func sortWikiDocs(docs []*wikiDocument, spec bson.D) {
	sort.SliceStable(docs, func(i, j int) bool {
		for _, key := range spec {
			left := wikiDocFieldValue(docs[i], key.Key)
			right := wikiDocFieldValue(docs[j], key.Key)
			if left == right {
				continue
			}
			if key.Value == -1 {
				return left > right
			}
			return left < right
		}
		return false
	})
}

// wikiFakeCursor implements cursorOps with in-memory results.
type wikiFakeCursor struct {
	docs []*wikiDocument
}

func (c *wikiFakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*wikiDocument)
	if !ok {
		return errors.New("invalid results target type")
	}
	*ptr = c.docs
	return nil
}

func (c *wikiFakeCursor) Close(_ context.Context) error {
	return nil
}

// newTestWikiRepo creates a wikiRepository backed by a fake collection.
func newTestWikiRepo() *wikiRepository {
	return &wikiRepository{
		collection: newWikiFakeCollection(),
	}
}

// --- Index model tests ---

// TestWikiIndexModels pins the startup index definitions to the template scope
// (specs/069-wiki-template-web/data-model.md §2.2).
func TestWikiIndexModels(t *testing.T) {
	// when
	models := wikiIndexModels()

	// then
	tests := []struct {
		name   string
		keys   bson.D
		unique bool
	}{
		{
			name:   "identity index",
			keys:   bson.D{{Key: fieldTemplate, Value: 1}, {Key: fieldWikiID, Value: 1}},
			unique: true,
		},
		{
			name:   "title uniqueness index",
			keys:   bson.D{{Key: fieldTemplate, Value: 1}, {Key: fieldTitleNorm, Value: 1}},
			unique: true,
		},
		{
			name:   "title grams index",
			keys:   bson.D{{Key: fieldTemplate, Value: 1}, {Key: fieldTitleGrams, Value: 1}},
			unique: false,
		},
	}
	if len(models) != len(tests) {
		t.Fatalf("wikiIndexModels() returned %d models, want %d", len(models), len(tests))
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := models[i]
			gotKeys, ok := model.Keys.(bson.D)
			if !ok {
				t.Fatalf("index keys = %T, want bson.D", model.Keys)
			}
			if !reflect.DeepEqual(gotKeys, tt.keys) {
				t.Fatalf("index keys = %v, want %v", gotKeys, tt.keys)
			}
			gotUnique := model.Options != nil && model.Options.Unique != nil && *model.Options.Unique
			if gotUnique != tt.unique {
				t.Fatalf("index unique = %v, want %v", gotUnique, tt.unique)
			}
		})
	}
}

// --- Tests ---

func TestWikiCreateGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()
	now := time.Date(2026, 9, 19, 10, 30, 0, 0, time.UTC)
	wiki := &domain.Wiki{
		Template:   "saolei",
		WikiID:     "wiki-1",
		Title:      "策略复盘",
		Content:    "知识正文",
		CreateTime: now,
		UpdateTime: now,
	}

	// when - create
	err := repo.CreateWiki(ctx, wiki)

	// then - stored with derived norm and bigrams
	if err != nil {
		t.Fatalf("CreateWiki() unexpected error: %v", err)
	}
	stored := repo.collection.(*wikiFakeCollection).docs[wikiDocKey("saolei", "wiki-1")]
	if stored == nil {
		t.Fatal("CreateWiki() did not store the document")
	}
	if stored.TitleNorm != "策略复盘" {
		t.Fatalf("stored title_norm = %q, want %q", stored.TitleNorm, "策略复盘")
	}
	assertStringSliceEqual(t, "stored title_grams", stored.TitleGrams, []string{"策略", "略复", "复盘"})

	// when - get
	got, err := repo.GetWiki(ctx, "saolei", "wiki-1")

	// then
	if err != nil {
		t.Fatalf("GetWiki() unexpected error: %v", err)
	}
	if got.Title != "策略复盘" || got.Content != "知识正文" {
		t.Fatalf("GetWiki() = %+v, want the stored title/content", got)
	}
	if !got.CreateTime.Equal(now) || !got.UpdateTime.Equal(now) {
		t.Fatalf("GetWiki() times = %v/%v, want %v", got.CreateTime, got.UpdateTime, now)
	}
}

func TestWikiCreateDuplicate(t *testing.T) {
	ctx := context.Background()

	// given - a seeded wiki, and full-width upper-case spelling of its title
	repo := newTestWikiRepo()
	seed := &domain.Wiki{Template: "saolei", WikiID: "wiki-1", Title: "ＡＢＣ"}
	if err := repo.CreateWiki(ctx, seed); err != nil {
		t.Fatalf("CreateWiki() seed unexpected error: %v", err)
	}

	// when - duplicate wiki id
	err := repo.CreateWiki(ctx, &domain.Wiki{Template: "saolei", WikiID: "wiki-1", Title: "other"})

	// then
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateWiki() duplicate id error = %v, want ErrAlreadyExists", err)
	}

	// when - different id but the same normalized title
	err = repo.CreateWiki(ctx, &domain.Wiki{Template: "saolei", WikiID: "wiki-2", Title: "abc"})

	// then
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateWiki() duplicate title error = %v, want ErrAlreadyExists", err)
	}

	// when - the same title under another template
	err = repo.CreateWiki(ctx, &domain.Wiki{Template: "other", WikiID: "wiki-1", Title: "abc"})

	// then - allowed: title uniqueness is scoped to the template
	if err != nil {
		t.Fatalf("CreateWiki() under other template unexpected error: %v", err)
	}
}

func TestWikiGetNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()

	// when
	_, err := repo.GetWiki(ctx, "saolei", "missing")

	// then
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetWiki() error = %v, want ErrNotFound", err)
	}
}

func TestWikiUpdate(t *testing.T) {
	ctx := context.Background()

	// given - a seeded wiki whose title is later changed
	repo := newTestWikiRepo()
	createTime := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	seed := &domain.Wiki{
		Template:   "saolei",
		WikiID:     "wiki-1",
		Title:      "旧标题",
		Content:    "旧内容",
		CreateTime: createTime,
		UpdateTime: createTime,
	}
	if err := repo.CreateWiki(ctx, seed); err != nil {
		t.Fatalf("CreateWiki() seed unexpected error: %v", err)
	}

	// when
	updateTime := createTime.Add(time.Hour)
	persisted, err := repo.UpdateWiki(ctx, &domain.Wiki{
		Template:   "saolei",
		WikiID:     "wiki-1",
		Title:      "新标题",
		Content:    "新内容",
		UpdateTime: updateTime,
	})

	// then - values replaced, create_time preserved, title index rebuilt
	if err != nil {
		t.Fatalf("UpdateWiki() unexpected error: %v", err)
	}
	if persisted.Title != "新标题" || persisted.Content != "新内容" {
		t.Fatalf("UpdateWiki() = %+v, want the updated title/content", persisted)
	}
	if !persisted.CreateTime.Equal(createTime) {
		t.Fatalf("UpdateWiki() create_time = %v, want %v", persisted.CreateTime, createTime)
	}
	stored := repo.collection.(*wikiFakeCollection).docs[wikiDocKey("saolei", "wiki-1")]
	if stored.TitleNorm != "新标题" {
		t.Fatalf("UpdateWiki() title_norm = %q, want %q", stored.TitleNorm, "新标题")
	}
	assertStringSliceEqual(t, "updated title_grams", stored.TitleGrams, []string{"新标", "标题"})
}

func TestWikiUpdateNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()

	// when
	_, err := repo.UpdateWiki(ctx, &domain.Wiki{Template: "saolei", WikiID: "missing", Title: "x", Content: "y"})

	// then
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateWiki() error = %v, want ErrNotFound", err)
	}
}

func TestWikiUpdateTitleCollision(t *testing.T) {
	ctx := context.Background()

	// given - two wikis in one template scope
	repo := newTestWikiRepo()
	for _, wiki := range []*domain.Wiki{
		{Template: "saolei", WikiID: "wiki-1", Title: "策略"},
		{Template: "saolei", WikiID: "wiki-2", Title: "开局"},
	} {
		if err := repo.CreateWiki(ctx, wiki); err != nil {
			t.Fatalf("CreateWiki() seed unexpected error: %v", err)
		}
	}

	// when - rename wiki-2 to a full-width spelling of wiki-1's title
	_, err := repo.UpdateWiki(ctx, &domain.Wiki{Template: "saolei", WikiID: "wiki-2", Title: "策略", Content: "x"})

	// then
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("UpdateWiki() collision error = %v, want ErrAlreadyExists", err)
	}
}

func TestWikiDelete(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()
	if err := repo.CreateWiki(ctx, &domain.Wiki{Template: "saolei", WikiID: "wiki-1", Title: "t"}); err != nil {
		t.Fatalf("CreateWiki() seed unexpected error: %v", err)
	}

	// when - delete with a mismatched template
	err := repo.DeleteWiki(ctx, "other", "wiki-1")

	// then - not found
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteWiki() mismatched template error = %v, want ErrNotFound", err)
	}

	// when
	err = repo.DeleteWiki(ctx, "saolei", "wiki-1")

	// then
	if err != nil {
		t.Fatalf("DeleteWiki() unexpected error: %v", err)
	}
	if len(repo.collection.(*wikiFakeCollection).docs) != 0 {
		t.Fatal("DeleteWiki() left the document behind")
	}
}

func TestWikiDeleteNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()

	// when
	err := repo.DeleteWiki(ctx, "saolei", "missing")

	// then
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteWiki() error = %v, want ErrNotFound", err)
	}
}

func TestWikiList(t *testing.T) {
	ctx := context.Background()

	// given - three wikis under one template and one under another
	repo := newTestWikiRepo()
	seeds := []*domain.Wiki{
		{Template: "saolei", WikiID: "bravo", Title: "b", Content: "c2"},
		{Template: "saolei", WikiID: "alpha", Title: "a", Content: "c1"},
		{Template: "saolei", WikiID: "charlie", Title: "c", Content: "c3"},
		{Template: "other", WikiID: "delta", Title: "d", Content: "c4"},
	}
	for _, wiki := range seeds {
		if err := repo.CreateWiki(ctx, wiki); err != nil {
			t.Fatalf("CreateWiki() seed unexpected error: %v", err)
		}
	}
	fake := repo.collection.(*wikiFakeCollection)

	// when - first page with pageSize=2
	page, nextPageToken, err := repo.ListWikis(ctx, "saolei", nil, 2)

	// then - wiki_id ascending, the other template excluded, template-scoped only
	if err != nil {
		t.Fatalf("ListWikis() unexpected error: %v", err)
	}
	assertTemplateScopedFilter(t, fake.filters[len(fake.filters)-1])
	assertWikiIDs(t, page, []string{"alpha", "bravo"})
	if nextPageToken == "" {
		t.Fatal("ListWikis() first page next_token is empty, want non-empty")
	}

	// when - second page resumes with the decoded cursor
	cursor, err := domain.DecodeWikiListPageToken(nextPageToken)
	if err != nil {
		t.Fatalf("DecodeWikiListPageToken() setup error: %v", err)
	}
	second, secondToken, err := repo.ListWikis(ctx, "saolei", cursor, 2)

	// then
	if err != nil {
		t.Fatalf("ListWikis() page 2 unexpected error: %v", err)
	}
	assertWikiIDs(t, second, []string{"charlie"})
	if secondToken != "" {
		t.Fatalf("ListWikis() page 2 next_token = %q, want empty", secondToken)
	}
}

func TestWikiListEmpty(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()

	// when
	page, nextPageToken, err := repo.ListWikis(ctx, "saolei", nil, 20)

	// then
	if err != nil {
		t.Fatalf("ListWikis() unexpected error: %v", err)
	}
	if len(page) != 0 || nextPageToken != "" {
		t.Fatalf("ListWikis() = %d entries / token %q, want empty", len(page), nextPageToken)
	}
}

func TestWikiSearchBigramPrefilter(t *testing.T) {
	ctx := context.Background()

	// given - two titles sharing the query bigram, one under another template
	repo := newTestWikiRepo()
	seeds := []*domain.Wiki{
		{Template: "saolei", WikiID: "wiki-1", Title: "策略复盘", Content: "c1"},
		{Template: "saolei", WikiID: "wiki-2", Title: "开局策略", Content: "c2"},
		{Template: "other", WikiID: "wiki-9", Title: "策略笔记", Content: "c9"},
	}
	for _, wiki := range seeds {
		if err := repo.CreateWiki(ctx, wiki); err != nil {
			t.Fatalf("CreateWiki() seed unexpected error: %v", err)
		}
	}
	fake := repo.collection.(*wikiFakeCollection)

	// when - query with two runes goes through the bigram prefilter
	hits, err := repo.SearchWikis(ctx, "saolei", domain.NormalizeTitle("策略"))

	// then - the $all filter was built over the query bigrams, template-scoped
	if err != nil {
		t.Fatalf("SearchWikis() unexpected error: %v", err)
	}
	filter := fake.filters[len(fake.filters)-1]
	assertTemplateScopedFilter(t, filter)
	if _, ok := filter[fieldTitleNorm]; ok {
		t.Fatalf("SearchWikis() bigram filter = %v, want no title_norm regex", filter)
	}
	all, ok := filter[fieldTitleGrams].(bson.M)["$all"].([]string)
	if !ok {
		t.Fatalf("SearchWikis() filter = %v, want a title_grams $all", filter)
	}
	assertStringSliceEqual(t, "query grams", all, []string{"策略"})

	// then - both sharing titles in the template are candidates, the other
	// template is not
	assertWikiHitIDs(t, hits, []string{"wiki-1", "wiki-2"})
	for _, hit := range hits {
		if hit.TitleNorm == "" {
			t.Fatalf("SearchWikis() hit %+v has no normalized title", hit)
		}
	}
}

func TestWikiSearchShortQueryFallback(t *testing.T) {
	ctx := context.Background()

	// given - a one-rune query (single Chinese character)
	repo := newTestWikiRepo()
	seeds := []*domain.Wiki{
		{Template: "saolei", WikiID: "wiki-1", Title: "策略复盘", Content: "c1"},
		{Template: "saolei", WikiID: "wiki-2", Title: "开局", Content: "c2"},
	}
	for _, wiki := range seeds {
		if err := repo.CreateWiki(ctx, wiki); err != nil {
			t.Fatalf("CreateWiki() seed unexpected error: %v", err)
		}
	}
	fake := repo.collection.(*wikiFakeCollection)

	// when
	hits, err := repo.SearchWikis(ctx, "saolei", domain.NormalizeTitle("策"))

	// then - the regex fallback is used and the single-rune query still matches
	if err != nil {
		t.Fatalf("SearchWikis() unexpected error: %v", err)
	}
	filter := fake.filters[len(fake.filters)-1]
	assertTemplateScopedFilter(t, filter)
	if _, ok := filter[fieldTitleGrams]; ok {
		t.Fatalf("SearchWikis() short-query filter = %v, want no title_grams", filter)
	}
	regexCondition, ok := filter[fieldTitleNorm].(bson.M)
	if !ok {
		t.Fatalf("SearchWikis() short-query filter = %v, want a title_norm $regex", filter)
	}
	if regexCondition["$regex"] != "策" {
		t.Fatalf("SearchWikis() regex = %v, want %q", regexCondition["$regex"], "策")
	}
	assertWikiHitIDs(t, hits, []string{"wiki-1"})
}

func TestWikiSearchProjectionExcludesContent(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestWikiRepo()
	if err := repo.CreateWiki(ctx, &domain.Wiki{Template: "saolei", WikiID: "wiki-1", Title: "策略复盘", Content: "c1"}); err != nil {
		t.Fatalf("CreateWiki() seed unexpected error: %v", err)
	}
	fake := repo.collection.(*wikiFakeCollection)

	// when
	if _, err := repo.SearchWikis(ctx, "saolei", domain.NormalizeTitle("策略")); err != nil {
		t.Fatalf("SearchWikis() unexpected error: %v", err)
	}

	// then - the projection carries title and wiki_id only
	projection, ok := fake.findOpts[len(fake.findOpts)-1].Projection.(bson.M)
	if !ok {
		t.Fatalf("SearchWikis() projection = %T, want bson.M", fake.findOpts[len(fake.findOpts)-1].Projection)
	}
	if projection[fieldTitle] != 1 || projection[fieldWikiID] != 1 {
		t.Fatalf("SearchWikis() projection = %v, want title and wiki_id", projection)
	}
	if _, ok := projection[fieldContent]; ok {
		t.Fatalf("SearchWikis() projection = %v, must not include content", projection)
	}
}

// assertWikiIDs asserts the page's wiki ids equal want in order.
func assertWikiIDs(t *testing.T, wikis []*domain.Wiki, want []string) {
	t.Helper()
	if len(wikis) != len(want) {
		t.Fatalf("ListWikis() returned %d wikis, want %d", len(wikis), len(want))
	}
	for i, id := range want {
		if wikis[i].WikiID != id {
			t.Fatalf("ListWikis()[%d] wiki_id = %q, want %q", i, wikis[i].WikiID, id)
		}
	}
}

// assertWikiHitIDs asserts the hit set equals want as a set (search candidate
// order is unspecified; the handler ranks).
func assertWikiHitIDs(t *testing.T, hits []*domain.WikiSearchHit, want []string) {
	t.Helper()
	if len(hits) != len(want) {
		t.Fatalf("SearchWikis() returned %d hits, want %d", len(hits), len(want))
	}
	got := make(map[string]bool, len(hits))
	for _, hit := range hits {
		got[hit.WikiID] = true
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("SearchWikis() hits = %v, want %q", hits, id)
		}
	}
}

// assertStringSliceEqual asserts got equals want element for element, naming
// the asserted object (`what`) in the failure message.
func assertStringSliceEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q (full: %v, want %v)", what, i, got[i], want[i], got, want)
		}
	}
}

// assertTemplateScopedFilter asserts every key of a wiki query filter belongs
// to the template-scope vocabulary: the wiki scope is the template alone
// (specs/069-wiki-template-web/data-model.md §1.1), so no other dimension may
// enter a query.
func assertTemplateScopedFilter(t *testing.T, filter bson.M) {
	t.Helper()
	for key := range filter {
		switch key {
		case fieldTemplate, fieldWikiID, fieldTitleNorm, fieldTitleGrams:
		default:
			t.Fatalf("wiki filter = %v, key %q is outside the template scope", filter, key)
		}
	}
}
