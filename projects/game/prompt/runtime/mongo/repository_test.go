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
	switch target := v.(type) {
	case *agentProfileDocument:
		src, ok := r.doc.(*agentProfileDocument)
		if !ok {
			return errors.New("invalid decode target type")
		}
		*target = *src
	case *skillDocument:
		src, ok := r.doc.(*skillDocument)
		if !ok {
			return errors.New("invalid decode target type")
		}
		*target = *src
	default:
		return errors.New("invalid decode target type")
	}
	return nil
}

// profileFakeCollection implements collectionOps with in-memory storage for agent profiles.
type profileFakeCollection struct {
	docs      map[string]*agentProfileDocument
	docsOrder []string
}

func newProfileFakeCollection() *profileFakeCollection {
	return &profileFakeCollection{
		docs: map[string]*agentProfileDocument{},
	}
}

func (c *profileFakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	doc, ok := document.(*agentProfileDocument)
	if !ok {
		return nil, errors.New("invalid document type")
	}
	if _, exists := c.docs[doc.AgentProfileName]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{
				{Code: 11000, Message: "duplicate key error"},
			},
		}
	}
	c.docs[doc.AgentProfileName] = doc
	c.docsOrder = append(c.docsOrder, doc.AgentProfileName)
	return &mongodriver.InsertOneResult{}, nil
}

func (c *profileFakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(agentProfileFilter)
	if !ok {
		return &fakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[f.AgentProfileName]
	if !exists {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &fakeSingleResult{doc: doc}
}

func (c *profileFakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(agentProfileFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	if _, exists := c.docs[f.AgentProfileName]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, f.AgentProfileName)
	for i, id := range c.docsOrder {
		if id == f.AgentProfileName {
			c.docsOrder = append(c.docsOrder[:i], c.docsOrder[i+1:]...)
			break
		}
	}
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (c *profileFakeCollection) Indexes() mongodriver.IndexView {
	return mongodriver.IndexView{}
}

func (c *profileFakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
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

	var filtered []*agentProfileDocument
	filterMap, isMap := filter.(bson.M)

	for _, name := range c.docsOrder {
		doc := c.docs[name]

		if isMap {
			if gtVal, hasGT := filterMap[fieldAgentProfileName]; hasGT {
				if gtMap, ok := gtVal.(bson.M); ok {
					if gt, ok2 := gtMap["$gt"]; ok2 {
						if doc.AgentProfileName <= gt.(string) {
							continue
						}
					}
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
				case fieldAgentProfileName:
					si, sj := filtered[i].AgentProfileName, filtered[j].AgentProfileName
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

	return &profileFakeCursor{docs: filtered}, nil
}

// profileFakeCursor implements cursorOps with in-memory results.
type profileFakeCursor struct {
	docs []*agentProfileDocument
}

func (c *profileFakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*agentProfileDocument)
	if !ok {
		return errors.New("invalid results target type")
	}
	*ptr = c.docs
	return nil
}

func (c *profileFakeCursor) Close(_ context.Context) error {
	return nil
}

// skillFakeCollection implements collectionOps with in-memory storage for skills.
type skillFakeCollection struct {
	docs      map[string]*skillDocument
	docsOrder []string
}

func newSkillFakeCollection() *skillFakeCollection {
	return &skillFakeCollection{
		docs: map[string]*skillDocument{},
	}
}

func (c *skillFakeCollection) InsertOne(_ context.Context, document interface{}, _ ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	doc, ok := document.(*skillDocument)
	if !ok {
		return nil, errors.New("invalid document type")
	}
	if _, exists := c.docs[doc.SkillName]; exists {
		return nil, mongodriver.WriteException{
			WriteErrors: []mongodriver.WriteError{
				{Code: 11000, Message: "duplicate key error"},
			},
		}
	}
	c.docs[doc.SkillName] = doc
	c.docsOrder = append(c.docsOrder, doc.SkillName)
	return &mongodriver.InsertOneResult{}, nil
}

func (c *skillFakeCollection) FindOne(_ context.Context, filter interface{}, _ ...*options.FindOneOptions) singleResult {
	f, ok := filter.(skillFilter)
	if !ok {
		return &fakeSingleResult{err: errors.New("invalid filter type")}
	}
	doc, exists := c.docs[f.SkillName]
	if !exists {
		return &fakeSingleResult{err: mongodriver.ErrNoDocuments}
	}
	return &fakeSingleResult{doc: doc}
}

func (c *skillFakeCollection) DeleteOne(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f, ok := filter.(skillFilter)
	if !ok {
		return nil, errors.New("invalid filter type")
	}
	if _, exists := c.docs[f.SkillName]; !exists {
		return &mongodriver.DeleteResult{DeletedCount: 0}, nil
	}
	delete(c.docs, f.SkillName)
	for i, id := range c.docsOrder {
		if id == f.SkillName {
			c.docsOrder = append(c.docsOrder[:i], c.docsOrder[i+1:]...)
			break
		}
	}
	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (c *skillFakeCollection) Indexes() mongodriver.IndexView {
	return mongodriver.IndexView{}
}

func (c *skillFakeCollection) Find(_ context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
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

	var filtered []*skillDocument
	filterMap, isMap := filter.(bson.M)

	for _, name := range c.docsOrder {
		doc := c.docs[name]

		if isMap {
			if gtVal, hasGT := filterMap[fieldSkillName]; hasGT {
				if gtMap, ok := gtVal.(bson.M); ok {
					if gt, ok2 := gtMap["$gt"]; ok2 {
						if doc.SkillName <= gt.(string) {
							continue
						}
					}
				}
			}
		}

		filtered = append(filtered, doc)
	}

	if limit > 0 && int64(len(filtered)) > limit {
		filtered = filtered[:limit]
	}

	return &skillFakeCursor{docs: filtered}, nil
}

// skillFakeCursor implements cursorOps with in-memory results.
type skillFakeCursor struct {
	docs []*skillDocument
}

func (c *skillFakeCursor) All(_ context.Context, results interface{}) error {
	ptr, ok := results.(*[]*skillDocument)
	if !ok {
		return errors.New("invalid results target type")
	}
	*ptr = c.docs
	return nil
}

func (c *skillFakeCursor) Close(_ context.Context) error {
	return nil
}

// newTestRepo creates a Repository backed by fake collections.
func newTestRepo() *Repository {
	return &Repository{
		profiles: newProfileFakeCollection(),
		skills:   newSkillFakeCollection(),
	}
}

// --- Tests ---

func TestAgentProfileCreateGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.AgentProfile{
		AgentProfileName: "test-profile",
		Model:            "opencode-go/deepseek-v4-pro",
		SystemPrompt:     "You are a helpful assistant.",
		SkillNames:       []string{"skill-a"},
		MCPNames:         []string{"mcp-b"},
		Enabled:          true,
	}

	// when - create
	err := repo.CreateAgentProfile(ctx, profile)

	// then
	if err != nil {
		t.Fatalf("CreateAgentProfile() unexpected error: %v", err)
	}

	// when - get
	got, err := repo.GetAgentProfile(ctx, "test-profile")

	// then
	if err != nil {
		t.Fatalf("GetAgentProfile() unexpected error: %v", err)
	}
	if got.AgentProfileName != "test-profile" {
		t.Fatalf("GetAgentProfile() name = %q, want %q", got.AgentProfileName, "test-profile")
	}
	if got.Model != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("GetAgentProfile() model = %q, want %q", got.Model, "opencode-go/deepseek-v4-pro")
	}
	if got.SystemPrompt != "You are a helpful assistant." {
		t.Fatalf("GetAgentProfile() system_prompt = %q, want %q", got.SystemPrompt, "You are a helpful assistant.")
	}
	if got.CreateTime.IsZero() {
		t.Fatalf("GetAgentProfile() create_time is zero, expected non-zero timestamp")
	}
	if got.UpdateTime.IsZero() {
		t.Fatalf("GetAgentProfile() update_time is zero, expected non-zero timestamp")
	}
}

func TestSkillCreateGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	skill := &domain.Skill{
		SkillName: "test-skill",
		Content:   "You are an expert coder.",
		Enabled:   true,
	}

	// when - create
	err := repo.CreateSkill(ctx, skill)

	// then
	if err != nil {
		t.Fatalf("CreateSkill() unexpected error: %v", err)
	}

	// when - get
	got, err := repo.GetSkill(ctx, "test-skill")

	// then
	if err != nil {
		t.Fatalf("GetSkill() unexpected error: %v", err)
	}
	if got.SkillName != "test-skill" {
		t.Fatalf("GetSkill() name = %q, want %q", got.SkillName, "test-skill")
	}
	if got.Content != "You are an expert coder." {
		t.Fatalf("GetSkill() content = %q, want %q", got.Content, "You are an expert coder.")
	}
	if got.CreateTime.IsZero() {
		t.Fatalf("GetSkill() create_time is zero, expected non-zero timestamp")
	}
}

func TestAgentProfileCreateDuplicate(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.AgentProfile{
		AgentProfileName: "test-profile",
		Model:            "opencode-go/deepseek-v4-pro",
	}
	err := repo.CreateAgentProfile(ctx, profile)
	if err != nil {
		t.Fatalf("CreateAgentProfile() first insert unexpected error: %v", err)
	}

	// when - create duplicate
	err = repo.CreateAgentProfile(ctx, profile)

	// then
	if err == nil {
		t.Fatalf("CreateAgentProfile() duplicate expected error, got nil")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateAgentProfile() error = %v, want ErrAlreadyExists", err)
	}
}

func TestAgentProfileGetNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()

	// when
	_, err := repo.GetAgentProfile(ctx, "nonexistent")

	// then
	if err == nil {
		t.Fatalf("GetAgentProfile() expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetAgentProfile() error = %v, want ErrNotFound", err)
	}
}

func TestAgentProfileList(t *testing.T) {
	ctx := context.Background()

	// given - seed 3 profiles
	repo := newTestRepo()
	profiles := []*domain.AgentProfile{
		{AgentProfileName: "alpha", Model: "opencode-go/deepseek-v4-pro"},
		{AgentProfileName: "bravo", Model: "opencode-go/deepseek-v4-pro"},
		{AgentProfileName: "charlie", Model: "opencode-go/deepseek-v4-pro"},
	}
	for _, p := range profiles {
		err := repo.CreateAgentProfile(ctx, p)
		if err != nil {
			t.Fatalf("CreateAgentProfile() seed unexpected error: %v", err)
		}
	}

	// when - first page with pageSize=2
	result, nextToken, err := repo.ListAgentProfiles(ctx, 2, "")

	// then - first page has 2 profiles (ASC: alpha, bravo) with next token
	if err != nil {
		t.Fatalf("ListAgentProfiles() unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListAgentProfiles() got %d profiles, want 2", len(result))
	}
	if result[0].AgentProfileName != "alpha" {
		t.Fatalf("ListAgentProfiles() first name = %q, want %q", result[0].AgentProfileName, "alpha")
	}
	if result[1].AgentProfileName != "bravo" {
		t.Fatalf("ListAgentProfiles() second name = %q, want %q", result[1].AgentProfileName, "bravo")
	}
	if nextToken == "" {
		t.Fatalf("ListAgentProfiles() next_token is empty, want non-empty")
	}

	// when - second page using cursor from first page
	result2, nextToken2, err := repo.ListAgentProfiles(ctx, 2, nextToken)

	// then - second page has 1 profile, no next token
	if err != nil {
		t.Fatalf("ListAgentProfiles() page 2 unexpected error: %v", err)
	}
	if len(result2) != 1 {
		t.Fatalf("ListAgentProfiles() page 2 got %d profiles, want 1", len(result2))
	}
	if result2[0].AgentProfileName != "charlie" {
		t.Fatalf("ListAgentProfiles() page 2 name = %q, want %q", result2[0].AgentProfileName, "charlie")
	}
	if nextToken2 != "" {
		t.Fatalf("ListAgentProfiles() page 2 next_token = %q, want empty", nextToken2)
	}
}

func TestAgentProfileDelete(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newTestRepo()
	profile := &domain.AgentProfile{
		AgentProfileName: "to-delete",
		Model:            "opencode-go/deepseek-v4-pro",
	}
	err := repo.CreateAgentProfile(ctx, profile)
	if err != nil {
		t.Fatalf("CreateAgentProfile() seed unexpected error: %v", err)
	}

	// when
	err = repo.DeleteAgentProfile(ctx, "to-delete")

	// then
	if err != nil {
		t.Fatalf("DeleteAgentProfile() unexpected error: %v", err)
	}

	// when - get after delete
	_, err = repo.GetAgentProfile(ctx, "to-delete")

	// then
	if err == nil {
		t.Fatalf("GetAgentProfile() after delete expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetAgentProfile() after delete error = %v, want ErrNotFound", err)
	}
}
