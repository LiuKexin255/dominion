// Package mongo provides the MongoDB-backed repository implementation for
// TeamProfile entities (spec 031-team-template-mode: TeamProfile replaces the
// former AgentProfile/Skill entities, clean break).
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
	// databaseName is the MongoDB database name for prompt storage.
	databaseName = "game_prompt"
	// teamProfilesCollectionName is the MongoDB collection name for team profiles.
	teamProfilesCollectionName = "team_profiles"
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

// collectionOps defines the MongoDB collection operations used by the repository.
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

// teamProfileRepository stores TeamProfile entities in MongoDB.
type teamProfileRepository struct {
	collection collectionOps
}

// NewRepository creates a MongoDB-backed TeamProfileRepository.
// It creates a unique index on team_profile_name.
func NewRepository(client *mongodriver.Client, dbName string) domain.TeamProfileRepository {
	coll := newCollection(client, dbName, teamProfilesCollectionName)

	// Create unique index on team_profile_name.
	_, _ = coll.Indexes().CreateOne(context.Background(), mongodriver.IndexModel{
		Keys:    bson.D{{Key: fieldTeamProfileName, Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &teamProfileRepository{
		collection: coll,
	}
}

// CreateTeamProfile stores a new TeamProfile in MongoDB.
func (r *teamProfileRepository) CreateTeamProfile(ctx context.Context, profile *domain.TeamProfile) error {
	doc := teamProfileDocumentFromDomain(profile)
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}
	if doc.UpdateTime.IsZero() {
		doc.UpdateTime = now
	}

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}

	return nil
}

// GetTeamProfile retrieves a TeamProfile by template and profile name.
func (r *teamProfileRepository) GetTeamProfile(ctx context.Context, template, profileName string) (*domain.TeamProfile, error) {
	filter := teamProfileFilter{TeamProfileName: profileName, Template: template}
	result := new(teamProfileDocument)
	if err := r.collection.FindOne(ctx, filter).Decode(result); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return result.toDomain(), nil
}

// UpdateTeamProfile replaces the stored TeamProfile identified by profile.TeamProfileName.
// The _id and create_time of the existing document are preserved.
func (r *teamProfileRepository) UpdateTeamProfile(ctx context.Context, profile *domain.TeamProfile) (*domain.TeamProfile, error) {
	// The filter carries only the profile name: the template field is not part
	// of the update identity (the caller-provided profile supplies it).
	filter := bson.M{fieldTeamProfileName: profile.TeamProfileName}
	existing := new(teamProfileDocument)
	if err := r.collection.FindOne(ctx, filter).Decode(existing); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	doc := teamProfileDocumentFromDomain(profile)
	doc.ID = existing.ID
	doc.CreateTime = existing.CreateTime

	if _, err := r.collection.ReplaceOne(ctx, filter, doc); err != nil {
		return nil, err
	}

	return doc.toDomain(), nil
}

// ListTeamProfiles retrieves a page of TeamProfiles under a template, sorted
// by team_profile_name ascending. pageSize controls the maximum number of
// results; pageToken is the cursor for the next page.
func (r *teamProfileRepository) ListTeamProfiles(ctx context.Context, template string, pageSize int, pageToken string) ([]*domain.TeamProfile, string, error) {
	filter := bson.M{fieldTemplate: template}
	if pageToken != "" {
		filter[fieldTeamProfileName] = bson.M{"$gt": pageToken}
	}

	limit := int64(pageSize) + 1
	opts := options.Find().
		SetSort(bson.D{{Key: fieldTeamProfileName, Value: 1}}).
		SetLimit(limit)

	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []*teamProfileDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		lastDoc := docs[pageSize-1]
		nextPageToken = lastDoc.TeamProfileName
		docs = docs[:pageSize]
	}

	profiles := make([]*domain.TeamProfile, 0, len(docs))
	for _, doc := range docs {
		profiles = append(profiles, doc.toDomain())
	}

	return profiles, nextPageToken, nil
}

// DeleteTeamProfile removes a TeamProfile by template and profile name.
func (r *teamProfileRepository) DeleteTeamProfile(ctx context.Context, template, profileName string) error {
	filter := teamProfileFilter{TeamProfileName: profileName, Template: template}
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}
