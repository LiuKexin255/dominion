// Package mongo provides the MongoDB-backed repository implementations for
// AgentProfile and Skill entities.
package mongo

import (
	"context"
	"errors"
	"time"

	"dominion/projects/game/prompt/domain"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// profilesCollectionName is the MongoDB collection name for agent profiles.
	profilesCollectionName = "agent_profiles"
	// skillsCollectionName is the MongoDB collection name for skills.
	skillsCollectionName = "skills"
)

// singleResult wraps the decode behavior of a MongoDB single document query result.
type singleResult interface {
	Decode(v interface{}) error
}

// cursorOps wraps the behavior of a MongoDB cursor for iterating query results.
type cursorOps interface {
	All(ctx context.Context, results interface{}) error
	Close(ctx context.Context) error
}

// collectionOps defines the MongoDB collection operations used by repositories.
type collectionOps interface {
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error)
	FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult
	ReplaceOne(ctx context.Context, filter interface{}, replacement interface{}, opts ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error)
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
	Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error)
	Indexes() mongodriver.IndexView
}

// mongoCollection wraps a MongoDB Collection to implement collectionOps.
type mongoCollection struct {
	*mongodriver.Collection
}

func (c *mongoCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	return c.Collection.InsertOne(ctx, document, opts...)
}

func (c *mongoCollection) FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *mongoCollection) ReplaceOne(ctx context.Context, filter interface{}, replacement interface{}, opts ...*options.ReplaceOptions) (*mongodriver.UpdateResult, error) {
	return c.Collection.ReplaceOne(ctx, filter, replacement, opts...)
}

func (c *mongoCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

func (c *mongoCollection) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursorOps, error) {
	cur, err := c.Collection.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	return &mongoCursor{Cursor: cur}, nil
}

func (c *mongoCollection) Indexes() mongodriver.IndexView {
	return c.Collection.Indexes()
}

// mongoCursor wraps a MongoDB Cursor to implement cursorOps.
type mongoCursor struct {
	*mongodriver.Cursor
}

// newCollection creates a collectionOps from a MongoDB client.
var newCollection = func(client *mongodriver.Client, db string, coll string) collectionOps {
	return &mongoCollection{Collection: client.Database(db).Collection(coll)}
}

// Repository stores AgentProfile and Skill entities in MongoDB.
type Repository struct {
	profiles collectionOps
	skills   collectionOps
}

// NewRepository creates a MongoDB-backed Repository.
// It creates unique indexes on agent_profile_name and skill_name.
func NewRepository(client *mongodriver.Client, dbName string) *Repository {
	profilesColl := newCollection(client, dbName, profilesCollectionName)
	skillsColl := newCollection(client, dbName, skillsCollectionName)

	// Create unique index on agent_profile_name.
	_, _ = profilesColl.Indexes().CreateOne(context.Background(), mongodriver.IndexModel{
		Keys:    bson.D{{Key: fieldAgentProfileName, Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// Create unique index on skill_name.
	_, _ = skillsColl.Indexes().CreateOne(context.Background(), mongodriver.IndexModel{
		Keys:    bson.D{{Key: fieldSkillName, Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &Repository{
		profiles: profilesColl,
		skills:   skillsColl,
	}
}

// --- AgentProfileRepository ---

// Create stores a new AgentProfile in MongoDB.
func (r *Repository) CreateAgentProfile(ctx context.Context, profile *domain.AgentProfile) error {
	doc := agentProfileDocumentFromDomain(profile)
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}
	if doc.UpdateTime.IsZero() {
		doc.UpdateTime = now
	}

	if _, err := r.profiles.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}

	return nil
}

// GetAgentProfile retrieves an AgentProfile by its profile name.
func (r *Repository) GetAgentProfile(ctx context.Context, profileName string) (*domain.AgentProfile, error) {
	filter := agentProfileFilter{AgentProfileName: profileName}
	result := new(agentProfileDocument)
	if err := r.profiles.FindOne(ctx, filter).Decode(result); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return result.toDomain(), nil
}

// UpdateAgentProfile replaces the stored AgentProfile identified by profile.AgentProfileName.
// The _id and create_time of the existing document are preserved.
func (r *Repository) UpdateAgentProfile(ctx context.Context, profile *domain.AgentProfile) (*domain.AgentProfile, error) {
	filter := agentProfileFilter{AgentProfileName: profile.AgentProfileName}
	existing := new(agentProfileDocument)
	if err := r.profiles.FindOne(ctx, filter).Decode(existing); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	doc := agentProfileDocumentFromDomain(profile)
	doc.ID = existing.ID
	doc.CreateTime = existing.CreateTime

	if _, err := r.profiles.ReplaceOne(ctx, filter, doc); err != nil {
		return nil, err
	}

	return doc.toDomain(), nil
}

// ListAgentProfiles retrieves a page of AgentProfiles sorted by agent_profile_name ascending.
// pageSize controls the maximum number of results; pageToken is the cursor for the next page.
func (r *Repository) ListAgentProfiles(ctx context.Context, pageSize int, pageToken string) ([]*domain.AgentProfile, string, error) {
	return listProfiles(r.profiles, ctx, pageSize, pageToken)
}

// DeleteAgentProfile removes an AgentProfile by its profile name.
func (r *Repository) DeleteAgentProfile(ctx context.Context, profileName string) error {
	filter := agentProfileFilter{AgentProfileName: profileName}
	result, err := r.profiles.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// --- SkillRepository ---

// CreateSkill stores a new Skill in MongoDB.
func (r *Repository) CreateSkill(ctx context.Context, skill *domain.Skill) error {
	doc := skillDocumentFromDomain(skill)
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}
	if doc.UpdateTime.IsZero() {
		doc.UpdateTime = now
	}

	if _, err := r.skills.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}

	return nil
}

// GetSkill retrieves a Skill by its skill name.
func (r *Repository) GetSkill(ctx context.Context, skillName string) (*domain.Skill, error) {
	filter := skillFilter{SkillName: skillName}
	result := new(skillDocument)
	if err := r.skills.FindOne(ctx, filter).Decode(result); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return result.toDomain(), nil
}

// UpdateSkill replaces the stored Skill identified by skill.SkillName.
// The _id and create_time of the existing document are preserved.
func (r *Repository) UpdateSkill(ctx context.Context, skill *domain.Skill) (*domain.Skill, error) {
	filter := skillFilter{SkillName: skill.SkillName}
	existing := new(skillDocument)
	if err := r.skills.FindOne(ctx, filter).Decode(existing); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	doc := skillDocumentFromDomain(skill)
	doc.ID = existing.ID
	doc.CreateTime = existing.CreateTime

	if _, err := r.skills.ReplaceOne(ctx, filter, doc); err != nil {
		return nil, err
	}

	return doc.toDomain(), nil
}

// ListSkills retrieves a page of Skills sorted by skill_name ascending.
func (r *Repository) ListSkills(ctx context.Context, pageSize int, pageToken string) ([]*domain.Skill, string, error) {
	return listSkills(r.skills, ctx, pageSize, pageToken)
}

// DeleteSkill removes a Skill by its skill name.
func (r *Repository) DeleteSkill(ctx context.Context, skillName string) error {
	filter := skillFilter{SkillName: skillName}
	result, err := r.skills.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// --- Pagination helpers ---

// listProfiles returns a page of agentProfileDocuments using cursor-based pagination.
func listProfiles(coll collectionOps, ctx context.Context, pageSize int, pageToken string) ([]*domain.AgentProfile, string, error) {
	filter := bson.M{}
	if pageToken != "" {
		filter = bson.M{
			fieldAgentProfileName: bson.M{"$gt": pageToken},
		}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{Key: fieldAgentProfileName, Value: 1}}).
		SetLimit(limit)

	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []*agentProfileDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		lastDoc := docs[pageSize-1]
		nextPageToken = lastDoc.AgentProfileName
		docs = docs[:pageSize]
	}

	profiles := make([]*domain.AgentProfile, 0, len(docs))
	for _, doc := range docs {
		profiles = append(profiles, doc.toDomain())
	}

	return profiles, nextPageToken, nil
}

// listSkills returns a page of skillDocuments using cursor-based pagination.
func listSkills(coll collectionOps, ctx context.Context, pageSize int, pageToken string) ([]*domain.Skill, string, error) {
	filter := bson.M{}
	if pageToken != "" {
		filter = bson.M{
			fieldSkillName: bson.M{"$gt": pageToken},
		}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{Key: fieldSkillName, Value: 1}}).
		SetLimit(limit)

	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []*skillDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		lastDoc := docs[pageSize-1]
		nextPageToken = lastDoc.SkillName
		docs = docs[:pageSize]
	}

	skills := make([]*domain.Skill, 0, len(docs))
	for _, doc := range docs {
		skills = append(skills, doc.toDomain())
	}

	return skills, nextPageToken, nil
}
