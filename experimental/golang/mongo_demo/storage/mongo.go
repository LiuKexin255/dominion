// Package storage provides a MongoDB-backed MongoRecordStore implementation.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	mongodemo "dominion/experimental/golang/mongo_demo"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// singleResult wraps the decode behavior of a MongoDB single document query result.
type singleResult interface {
	Decode(v interface{}) error
}

// cursor wraps the iteration behavior of a MongoDB cursor.
type cursor interface {
	All(ctx context.Context, results interface{}) error
	Close(ctx context.Context) error
}

// collectionOps defines the MongoDB collection operations used by mongoStore.
type collectionOps interface {
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error)
	FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult
	Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursor, error)
	UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error)
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
	CountDocuments(ctx context.Context, filter interface{}, opts ...*options.CountOptions) (int64, error)
}

var newCollection = func(client *mongodriver.Client, db string, coll string) collectionOps {
	return &mongoCollection{Collection: client.Database(db).Collection(coll)}
}

type mongoCollection struct {
	*mongodriver.Collection
}

func (c *mongoCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	return c.Collection.InsertOne(ctx, document, opts...)
}

func (c *mongoCollection) FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *mongoCollection) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (cursor, error) {
	return c.Collection.Find(ctx, filter, opts...)
}

func (c *mongoCollection) CountDocuments(ctx context.Context, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	return c.Collection.CountDocuments(ctx, filter, opts...)
}

func (c *mongoCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	return c.Collection.UpdateOne(ctx, filter, update, opts...)
}

func (c *mongoCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

// mongoStore stores MongoRecord resources in MongoDB.
type mongoStore struct {
	collection collectionOps
}

// NewMongoStore creates a MongoDB-backed MongoRecordStore.
func NewMongoStore(client *mongodriver.Client) MongoRecordStore {
	return &mongoStore{
		collection: newCollection(client, DatabaseName, CollectionName),
	}
}

// Create stores a new record in MongoDB.
func (s *mongoStore) Create(ctx context.Context, record *mongodemo.MongoRecord) (*mongodemo.MongoRecord, error) {
	mongoDoc := mongoRecordFromProto(record)
	now := time.Now()
	if mongoDoc.CreateTime.IsZero() {
		mongoDoc.CreateTime = now
	}
	mongoDoc.UpdateTime = now

	if _, err := s.collection.InsertOne(ctx, mongoDoc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}

	return mongoDoc.toProto(), nil
}

// Get loads a record by resource name.
func (s *mongoStore) Get(ctx context.Context, name string) (*mongodemo.MongoRecord, error) {
	record := new(mongoRecord)
	if err := s.collection.FindOne(ctx, bson.M{"name": name}).Decode(record); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return record.toProto(), nil
}

// List returns records under the given parent with pagination.
func (s *mongoStore) List(ctx context.Context, parent string, pageSize int32, pageToken string, showArchived bool) ([]*mongodemo.MongoRecord, string, error) {
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}

	skip, err := DecodePageToken(pageToken)
	if err != nil {
		return nil, "", fmt.Errorf("invalid page token: %w", err)
	}

	filter := bson.M{"app": parent}
	if !showArchived {
		filter["archived"] = bson.M{"$ne": true}
	}

	total, err := s.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, "", err
	}
	if int64(skip) >= total {
		return nil, "", nil
	}

	findOpts := options.Find().SetSkip(int64(skip)).SetLimit(int64(pageSize)).SetSort(bson.D{{Key: "name", Value: 1}})
	cur, err := s.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []*mongoRecord
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	records := make([]*mongodemo.MongoRecord, 0, len(docs))
	for _, doc := range docs {
		records = append(records, doc.toProto())
	}

	nextSkip := skip + len(records)
	if int64(nextSkip) >= total {
		return records, "", nil
	}

	return records, EncodePageToken(nextSkip), nil
}

// Update modifies selected fields of an existing record.
func (s *mongoStore) Update(ctx context.Context, record *mongodemo.MongoRecord, updateMask *fieldmaskpb.FieldMask) (*mongodemo.MongoRecord, error) {
	paths := updateMask.GetPaths()
	if len(paths) == 0 {
		return nil, fmt.Errorf("update_mask paths cannot be empty")
	}

	updateDoc := bson.M{}
	profile := record.GetProfile()
	for _, path := range paths {
		switch path {
		case "title":
			updateDoc["title"] = record.GetTitle()
		case "description":
			updateDoc["description"] = record.GetDescription()
		case "labels":
			updateDoc["labels"] = cloneStringMap(record.GetLabels())
		case "tags":
			updateDoc["tags"] = cloneStringSlice(record.GetTags())
		case "archived":
			updateDoc["archived"] = record.GetArchived()
		case "profile":
			updateDoc["profile"] = mongoProfileFromProto(profile)
		case "profile.owner":
			updateDoc["profile.owner"] = ""
			if profile != nil {
				updateDoc["profile.owner"] = profile.GetOwner()
			}
		case "profile.priority":
			updateDoc["profile.priority"] = int32(0)
			if profile != nil {
				updateDoc["profile.priority"] = profile.GetPriority()
			}
		case "profile.watchers":
			updateDoc["profile.watchers"] = []string(nil)
			if profile != nil {
				updateDoc["profile.watchers"] = cloneStringSlice(profile.GetWatchers())
			}
		default:
			return nil, fmt.Errorf("unsupported update path %q", path)
		}
	}
	updateDoc["update_time"] = time.Now()

	result, err := s.collection.UpdateOne(ctx, bson.M{"name": record.GetName()}, bson.M{"$set": updateDoc})
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, ErrNotFound
	}

	return s.Get(ctx, record.GetName())
}

// Delete removes a record by resource name.
func (s *mongoStore) Delete(ctx context.Context, name string) error {
	result, err := s.collection.DeleteOne(ctx, bson.M{"name": name})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrNotFound
	}

	return nil
}

func mongoProfileFromProto(profile *mongodemo.MongoRecordProfile) *mongoProfile {
	if profile == nil {
		return nil
	}

	return &mongoProfile{
		Owner:    profile.GetOwner(),
		Priority: profile.GetPriority(),
		Watchers: cloneStringSlice(profile.GetWatchers()),
	}
}
