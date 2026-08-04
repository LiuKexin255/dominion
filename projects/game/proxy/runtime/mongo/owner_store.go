// Package mongo provides the MongoDB-backed OwnerStore implementation.
package mongo

import (
	"context"
	"errors"

	"dominion/projects/game/proxy/domain"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ownerFilter is a concrete BSON filter struct for querying by the
// (template_id, session_id) composite key: a session is identified by the
// resource pattern templates/{template}/sessions/{session}
// (projects/game/game.proto), so the same session ID under different
// templates is a distinct session.
type ownerFilter struct {
	TemplateID string `bson:"template_id"`
	SessionID  string `bson:"session_id"`
}

const (
	// databaseName is the MongoDB database name for proxy storage.
	databaseName = "game_proxy"
	// collectionName is the MongoDB collection name for agent owners.
	collectionName = "agent_owners"
)

// singleResult wraps the decode behavior of a MongoDB single document query result.
type singleResult interface {
	Decode(v interface{}) error
}

// collectionOps defines the MongoDB collection operations used by mongoOwnerStore.
type collectionOps interface {
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error)
	FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
}

// ownerCollection wraps a MongoDB Collection to implement collectionOps.
type ownerCollection struct {
	*mongodriver.Collection
}

func (c *ownerCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	return c.Collection.InsertOne(ctx, document, opts...)
}

func (c *ownerCollection) FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *ownerCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

// newOwnerCollection creates an ownerCollection from a MongoDB client.
var newOwnerCollection = func(client *mongodriver.Client, db string, coll string) collectionOps {
	return &ownerCollection{Collection: client.Database(db).Collection(coll)}
}

// mongoOwnerStore stores AgentOwner entities in MongoDB.
type mongoOwnerStore struct {
	collection collectionOps
}

// NewMongoOwnerStore creates a MongoDB-backed OwnerStore.
func NewMongoOwnerStore(client *mongodriver.Client) domain.OwnerStore {
	return &mongoOwnerStore{
		collection: newOwnerCollection(client, databaseName, collectionName),
	}
}

// Create stores a new agent owner record in MongoDB.
func (s *mongoOwnerStore) Create(ctx context.Context, owner *domain.AgentOwner) error {
	// Check for existing record before inserting.
	existing := new(agentOwnerDocument)
	if err := s.collection.FindOne(ctx, ownerFilter{TemplateID: owner.TemplateID, SessionID: owner.SessionID}).Decode(existing); err == nil {
		return domain.ErrOwnerAlreadyExists
	} else if !errors.Is(err, mongodriver.ErrNoDocuments) {
		return err
	}

	doc := agentOwnerDocumentFromDomain(owner)

	if _, err := s.collection.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrOwnerAlreadyExists
		}
		return err
	}

	return nil
}

// Get retrieves an agent owner by its (templateID, sessionID) composite key.
func (s *mongoOwnerStore) Get(ctx context.Context, templateID, sessionID string) (*domain.AgentOwner, error) {
	result := new(agentOwnerDocument)
	if err := s.collection.FindOne(ctx, ownerFilter{TemplateID: templateID, SessionID: sessionID}).Decode(result); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrOwnerNotFound
		}
		return nil, err
	}

	return result.toDomain(), nil
}

// Delete removes an agent owner by its (templateID, sessionID) composite key.
func (s *mongoOwnerStore) Delete(ctx context.Context, templateID, sessionID string) error {
	result, err := s.collection.DeleteOne(ctx, ownerFilter{TemplateID: templateID, SessionID: sessionID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrOwnerNotFound
	}

	return nil
}
