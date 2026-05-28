// Package mongo provides the MongoDB-backed OwnerStore implementation.
package mongo

import (
	"context"
	"errors"
	"time"

	"dominion/projects/game/proxy/domain"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ownerFilter is a concrete BSON filter struct for querying by session_id.
type ownerFilter struct {
	SessionID string `bson:"session_id"`
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
	if err := s.collection.FindOne(ctx, ownerFilter{SessionID: owner.SessionID}).Decode(existing); err == nil {
		return domain.ErrOwnerAlreadyExists
	} else if !errors.Is(err, mongodriver.ErrNoDocuments) {
		return err
	}

	doc := agentOwnerDocumentFromDomain(owner)
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}

	if _, err := s.collection.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrOwnerAlreadyExists
		}
		return err
	}

	return nil
}

// Get retrieves an agent owner by session ID.
func (s *mongoOwnerStore) Get(ctx context.Context, sessionID string) (*domain.AgentOwner, error) {
	result := new(agentOwnerDocument)
	if err := s.collection.FindOne(ctx, ownerFilter{SessionID: sessionID}).Decode(result); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrOwnerNotFound
		}
		return nil, err
	}

	return result.toDomain(), nil
}

// Delete removes an agent owner by session ID.
func (s *mongoOwnerStore) Delete(ctx context.Context, sessionID string) error {
	result, err := s.collection.DeleteOne(ctx, ownerFilter{SessionID: sessionID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrOwnerNotFound
	}

	return nil
}
