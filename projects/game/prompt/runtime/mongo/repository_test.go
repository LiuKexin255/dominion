package mongo

import (
	"context"
	"errors"
	"sort"
	"testing"

	"dominion/projects/game/prompt/domain"

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
	target, ok := v.(*teamProfileDocument)
	if !ok {
		return errors.New("invalid decode target type")
	}
	src, ok := r.doc.(*teamProfileDocument)
	if !ok {
		return errors.New("invalid decode target type")
	}
	*target = *src
	return nil
}

// teamProfileFakeCollection implements collectionOps with in-memory storage
// for team profiles.
type teamProfileFakeCollection struct {
	docs      map[string]*teamProfileDocument
	docsOrder []string
}

func newTeamProfileFakeCollection() *teamProfileFakeCollection {
	return &teamProfileFakeCollection{
		docs: map[string]*teamProfileDocument{},
	}
}

func (c *teamProfileFakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	doc, ok := document.(*teamProfileDocument)
	if !ok {
		return nil, errors.New("invalid document type")
	}
	if _, exists := c.docs[doc.TeamProfileName]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{
				{Code: 11000, Message: "duplicate key error"},
			},
		}
	}
	c.docs[doc.TeamProfileName] = doc
	c.docsOrder = append(c.docsOrder, doc.TeamProfileName)
	return &mongodriver.InsertOneResult{}, nil
}

func (c *teamProfileFakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	// The update path filters by bson.M{team_profile_name}; the get/delete
	// paths use the typed teamProfileFilter.
	var name, template string
	switch f := filter.(type) {
	case teamProfileFilter:
		name, template = f.TeamProfileName, f.Template
	case bson.M:
		name, _ = f[fieldTeamProfileName].(string)
		template, _ = f[fieldTemplate].(string)
	default:
		return &fakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[name]
	if !exists || (template != "" && doc.Template != template) {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &fakeSingleResult{doc: doc}
}

func (c *teamProfileFakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(teamProfileFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	doc, exists := c.docs[f.TeamProfileName]
	if !exists || doc.Template != f.Template {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, f.TeamProfileName)
	for i, id := range c.docsOrder {
		if id == f.TeamProfileName {
			c.docsOrder = append(c.docsOrder[:i], c.docsOrder[i+1:]...)
			break
		}
	}
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (c *teamProfileFakeCollection) ReplaceOne(_ context.Context, filter interface{}, replacement interface{}, _ ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error) {
	f, ok := filter.(bson.M)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	doc, ok := replacement.(*teamProfileDocument)
	if !ok {
		return nil, errors.New("invalid replacement type")
	}
	name, ok := f[fieldTeamProfileName].(string)
	if !ok {
		return nil, errors.New("invalid filter: missing team_profile_name")
	}
	existing, exists := c.docs[name]
	if !exists {
		return &mongodriver.UpdateResult{MatchedCount: 0, ModifiedCount: 0}, nil
	}
	doc.ID = existing.ID
	c.docs[name] = doc
	return &mongodriver.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, nil
}

func (c *teamProfileFakeCollection) Indexes() mongodriver.IndexView {
	return mongodriver.IndexView{}
}

func (c *teamProfileFakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
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

	var filtered []*teamProfileDocument
	filterMap, isMap := filter.(bson.M)

	for _, name := range c.docsOrder {
		doc := c.docs[name]

		if isMap {
			if tmpl, hasTmpl := filterMap[fieldTemplate]; hasTmpl {
				if doc.Template != tmpl {
					continue
				}
			}
			if gtVal, hasGT := filterMap[fieldTeamProfileName]; hasGT {
				if gtMap, ok := gtVal.(bson.M); ok {
					if gt, ok2 := gtMap["$gt"]; ok2 {
						if doc.TeamProfileName <= gt.(string) {
							continue
						}
					}
				}
			}
		}

		filtered = append(filtered, doc)
	}

	if len(filtered) > 1 {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].TeamProfileName < filtered[j].TeamProfileName
		})
	}

	if limit > 0 && int64(len(filtered)) > limit {
		filtered = filtered[:limit]
	}

	return &teamProfileFakeCursor{docs: filtered}, nil
}

// teamProfileFakeCursor implements cursorOps with in-memory results.
type teamProfileFakeCursor struct {
	docs []*teamProfileDocument
}

func (c *teamProfileFakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*teamProfileDocument)
	if !ok {
		return errors.New("invalid results target type")
	}
	*ptr = c.docs
	return nil
}

func (c *teamProfileFakeCursor) Close(_ context.Context) error {
	return nil
}

// newTestRepo creates a teamProfileRepository backed by a fake collection.
func newTestRepo() *teamProfileRepository {
	return &teamProfileRepository{
		collection: newTeamProfileFakeCollection(),
	}
}

// --- Tests ---

func TestTeamProfileCreateGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.TeamProfile{
		TeamProfileName:    "default",
		Template:           "saolei",
		SaoleiPlayerModel:  "opencode-go/deepseek-v4-pro",
		SaoleiPlannerModel: "opencode-go/deepseek-v4-pro",
	}

	// when - create
	err := repo.CreateTeamProfile(ctx, profile)

	// then
	if err != nil {
		t.Fatalf("CreateTeamProfile() unexpected error: %v", err)
	}

	// when - get
	got, err := repo.GetTeamProfile(ctx, "saolei", "default")

	// then
	if err != nil {
		t.Fatalf("GetTeamProfile() unexpected error: %v", err)
	}
	if got.TeamProfileName != "default" {
		t.Fatalf("GetTeamProfile() name = %q, want %q", got.TeamProfileName, "default")
	}
	if got.Template != "saolei" {
		t.Fatalf("GetTeamProfile() template = %q, want %q", got.Template, "saolei")
	}
	if got.SaoleiPlayerModel != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("GetTeamProfile() player_model = %q, want %q", got.SaoleiPlayerModel, "opencode-go/deepseek-v4-pro")
	}
	if got.SaoleiPlannerModel != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("GetTeamProfile() planner_model = %q, want %q", got.SaoleiPlannerModel, "opencode-go/deepseek-v4-pro")
	}
	if got.CreateTime.IsZero() {
		t.Fatalf("GetTeamProfile() create_time is zero, expected non-zero timestamp")
	}
	if got.UpdateTime.IsZero() {
		t.Fatalf("GetTeamProfile() update_time is zero, expected non-zero timestamp")
	}
}

func TestTeamProfileCreateDuplicate(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.TeamProfile{
		TeamProfileName: "default",
		Template:        "saolei",
	}
	err := repo.CreateTeamProfile(ctx, profile)
	if err != nil {
		t.Fatalf("CreateTeamProfile() first insert unexpected error: %v", err)
	}

	// when - create duplicate
	err = repo.CreateTeamProfile(ctx, profile)

	// then
	if err == nil {
		t.Fatalf("CreateTeamProfile() duplicate expected error, got nil")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateTeamProfile() error = %v, want ErrAlreadyExists", err)
	}
}

func TestTeamProfileGetNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()

	// when - missing profile
	_, err := repo.GetTeamProfile(ctx, "saolei", "nonexistent")

	// then
	if err == nil {
		t.Fatalf("GetTeamProfile() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetTeamProfile() error = %v, want ErrNotFound", err)
	}

	// given - profile exists under another template
	profile := &domain.TeamProfile{
		TeamProfileName: "default",
		Template:        "saolei",
	}
	if err := repo.CreateTeamProfile(ctx, profile); err != nil {
		t.Fatalf("CreateTeamProfile() seed unexpected error: %v", err)
	}

	// when - get with mismatched template
	_, err = repo.GetTeamProfile(ctx, "other-template", "default")

	// then
	if err == nil {
		t.Fatalf("GetTeamProfile() with mismatched template expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetTeamProfile() with mismatched template error = %v, want ErrNotFound", err)
	}
}

func TestTeamProfileList(t *testing.T) {
	ctx := context.Background()

	// given - seed 3 saolei profiles and 1 other-template profile
	repo := newTestRepo()
	profiles := []*domain.TeamProfile{
		{TeamProfileName: "alpha", Template: "saolei", SaoleiPlayerModel: "m1"},
		{TeamProfileName: "bravo", Template: "saolei", SaoleiPlayerModel: "m1"},
		{TeamProfileName: "charlie", Template: "saolei", SaoleiPlayerModel: "m1"},
		{TeamProfileName: "delta", Template: "other-template", SaoleiPlayerModel: "m1"},
	}
	for _, p := range profiles {
		err := repo.CreateTeamProfile(ctx, p)
		if err != nil {
			t.Fatalf("CreateTeamProfile() seed unexpected error: %v", err)
		}
	}

	// when - first page with pageSize=2
	result, nextToken, err := repo.ListTeamProfiles(ctx, "saolei", 2, "")

	// then - first page has 2 saolei profiles (ASC: alpha, bravo) with next token
	if err != nil {
		t.Fatalf("ListTeamProfiles() unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListTeamProfiles() got %d profiles, want 2", len(result))
	}
	if result[0].TeamProfileName != "alpha" {
		t.Fatalf("ListTeamProfiles() first name = %q, want %q", result[0].TeamProfileName, "alpha")
	}
	if result[1].TeamProfileName != "bravo" {
		t.Fatalf("ListTeamProfiles() second name = %q, want %q", result[1].TeamProfileName, "bravo")
	}
	if nextToken == "" {
		t.Fatalf("ListTeamProfiles() next_token is empty, want non-empty")
	}

	// when - second page using cursor from first page
	result2, nextToken2, err := repo.ListTeamProfiles(ctx, "saolei", 2, nextToken)

	// then - second page has 1 profile (charlie), no next token; delta excluded
	if err != nil {
		t.Fatalf("ListTeamProfiles() page 2 unexpected error: %v", err)
	}
	if len(result2) != 1 {
		t.Fatalf("ListTeamProfiles() page 2 got %d profiles, want 1", len(result2))
	}
	if result2[0].TeamProfileName != "charlie" {
		t.Fatalf("ListTeamProfiles() page 2 name = %q, want %q", result2[0].TeamProfileName, "charlie")
	}
	if nextToken2 != "" {
		t.Fatalf("ListTeamProfiles() page 2 next_token = %q, want empty", nextToken2)
	}
}

func TestTeamProfileUpdate(t *testing.T) {
	ctx := context.Background()

	// given - seed a profile
	repo := newTestRepo()
	seed := &domain.TeamProfile{
		TeamProfileName:    "updatable",
		Template:           "saolei",
		SaoleiPlayerModel:  "model-a",
		SaoleiPlannerModel: "model-b",
	}
	if err := repo.CreateTeamProfile(ctx, seed); err != nil {
		t.Fatalf("CreateTeamProfile() seed unexpected error: %v", err)
	}

	original, err := repo.GetTeamProfile(ctx, "saolei", "updatable")
	if err != nil {
		t.Fatalf("GetTeamProfile() seed unexpected error: %v", err)
	}

	// when - update player model
	updated := *original
	updated.SaoleiPlayerModel = "model-c"
	persisted, err := repo.UpdateTeamProfile(ctx, &updated)

	// then
	if err != nil {
		t.Fatalf("UpdateTeamProfile() unexpected error: %v", err)
	}
	if persisted.SaoleiPlayerModel != "model-c" {
		t.Fatalf("UpdateTeamProfile() player_model = %q, want %q", persisted.SaoleiPlayerModel, "model-c")
	}
	if persisted.SaoleiPlannerModel != "model-b" {
		t.Fatalf("UpdateTeamProfile() planner_model = %q, want %q", persisted.SaoleiPlannerModel, "model-b")
	}
	if !persisted.CreateTime.Equal(original.CreateTime) {
		t.Fatalf("UpdateTeamProfile() create_time changed: got %v, want %v", persisted.CreateTime, original.CreateTime)
	}

	// when - re-read from repository
	reread, err := repo.GetTeamProfile(ctx, "saolei", "updatable")

	// then - persisted value matches
	if err != nil {
		t.Fatalf("GetTeamProfile() after update unexpected error: %v", err)
	}
	if reread.SaoleiPlayerModel != "model-c" {
		t.Fatalf("GetTeamProfile() after update player_model = %q, want %q", reread.SaoleiPlayerModel, "model-c")
	}
}

func TestTeamProfileUpdateNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.TeamProfile{
		TeamProfileName: "ghost",
		Template:        "saolei",
	}

	// when
	_, err := repo.UpdateTeamProfile(ctx, profile)

	// then
	if err == nil {
		t.Fatalf("UpdateTeamProfile() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateTeamProfile() error = %v, want ErrNotFound", err)
	}
}

func TestTeamProfileDelete(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.TeamProfile{
		TeamProfileName: "to-delete",
		Template:        "saolei",
	}
	err := repo.CreateTeamProfile(ctx, profile)
	if err != nil {
		t.Fatalf("CreateTeamProfile() seed unexpected error: %v", err)
	}

	// when - delete with mismatched template
	err = repo.DeleteTeamProfile(ctx, "other-template", "to-delete")

	// then - not found
	if err == nil {
		t.Fatalf("DeleteTeamProfile() with mismatched template expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteTeamProfile() with mismatched template error = %v, want ErrNotFound", err)
	}

	// when - delete
	err = repo.DeleteTeamProfile(ctx, "saolei", "to-delete")

	// then
	if err != nil {
		t.Fatalf("DeleteTeamProfile() unexpected error: %v", err)
	}

	// when - get after delete
	_, err = repo.GetTeamProfile(ctx, "saolei", "to-delete")

	// then
	if err == nil {
		t.Fatalf("GetTeamProfile() after delete expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetTeamProfile() after delete error = %v, want ErrNotFound", err)
	}
}
