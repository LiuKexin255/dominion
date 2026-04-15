// Package storage provides repository implementations for the deploy service.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dominion/projects/infra/deploy/domain"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// DatabaseName is the MongoDB database used by the deploy service.
	DatabaseName = "deploy"
	// CollectionName is the MongoDB collection used for environments.
	CollectionName = "environments"
	// mongoDefaultPageSize is the default number of environments returned by ListByScope.
	mongoDefaultPageSize   = 100
	mongoFieldName         = "name"
	mongoFieldScope        = "scope"
	mongoFieldEnvName      = "env_name"
	mongoFieldDescription  = "description"
	mongoFieldDesiredState = "desired_state"
	mongoFieldStatus       = "status"
	mongoFieldStatusState  = "status.state"
	mongoFieldCreateTime   = "create_time"
	mongoFieldUpdateTime   = "update_time"
	mongoFieldETag         = "etag"
)

// singleResult wraps the decode behavior of a MongoDB single document query result.
type singleResult interface {
	Decode(v any) error
}

// cursor wraps the iteration behavior of a MongoDB cursor.
type cursor interface {
	All(ctx context.Context, results any) error
	Close(ctx context.Context) error
}

// indexViewOps wraps the index operations used by MongoRepository.
type indexViewOps interface {
	CreateMany(ctx context.Context, models []mongodriver.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error)
}

// collectionOps defines the MongoDB collection operations used by MongoRepository.
type collectionOps interface {
	FindOne(ctx context.Context, filter any, opts ...*options.FindOneOptions) singleResult
	Find(ctx context.Context, filter any, opts ...*options.FindOptions) (cursor, error)
	UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error)
	DeleteOne(ctx context.Context, filter any, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error)
	CountDocuments(ctx context.Context, filter any, opts ...*options.CountOptions) (int64, error)
	Indexes() indexViewOps
}

var newCollection = func(client *mongodriver.Client, db string, coll string) collectionOps {
	return &mongoCollection{Collection: client.Database(db).Collection(coll)}
}

type mongoCollection struct {
	*mongodriver.Collection
}

func (c *mongoCollection) FindOne(ctx context.Context, filter any, opts ...*options.FindOneOptions) singleResult {
	return c.Collection.FindOne(ctx, filter, opts...)
}

func (c *mongoCollection) Find(ctx context.Context, filter any, opts ...*options.FindOptions) (cursor, error) {
	return c.Collection.Find(ctx, filter, opts...)
}

func (c *mongoCollection) UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	return c.Collection.UpdateOne(ctx, filter, update, opts...)
}

func (c *mongoCollection) DeleteOne(ctx context.Context, filter any, opts ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	return c.Collection.DeleteOne(ctx, filter, opts...)
}

func (c *mongoCollection) CountDocuments(ctx context.Context, filter any, opts ...*options.CountOptions) (int64, error) {
	return c.Collection.CountDocuments(ctx, filter, opts...)
}

func (c *mongoCollection) Indexes() indexViewOps {
	return c.Collection.Indexes()
}

// mongoEnvironment is the BSON document representation of a deploy environment.
type mongoEnvironment struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Name         string             `bson:"name"`
	Scope        string             `bson:"scope"`
	EnvName      string             `bson:"env_name"`
	Description  string             `bson:"description"`
	DesiredState *mongoDesiredState `bson:"desired_state"`
	Status       *mongoStatus       `bson:"status"`
	CreateTime   time.Time          `bson:"create_time"`
	UpdateTime   time.Time          `bson:"update_time"`
	ETag         string             `bson:"etag"`
}

// mongoDesiredState is the BSON representation of domain.DesiredState.
type mongoDesiredState struct {
	Services   []mongoServiceSpec   `bson:"services"`
	Infras     []mongoInfraSpec     `bson:"infras"`
	HTTPRoutes []mongoHTTPRouteSpec `bson:"http_routes"`
}

// mongoServicePortSpec is the BSON representation of domain.ServicePortSpec.
type mongoServicePortSpec struct {
	Name string `bson:"name"`
	Port int32  `bson:"port"`
}

// mongoServiceSpec is the BSON representation of domain.ServiceSpec.
type mongoServiceSpec struct {
	Name       string                 `bson:"name"`
	App        string                 `bson:"app"`
	Image      string                 `bson:"image"`
	Ports      []mongoServicePortSpec `bson:"ports"`
	Replicas   int32                  `bson:"replicas"`
	TLSEnabled bool                   `bson:"tls_enabled"`
}

// mongoInfraSpec is the BSON representation of domain.InfraSpec.
type mongoInfraSpec struct {
	Resource           string `bson:"resource"`
	Profile            string `bson:"profile"`
	Name               string `bson:"name"`
	App                string `bson:"app"`
	PersistenceEnabled bool   `bson:"persistence_enabled"`
}

// mongoHTTPPathRule is the BSON representation of domain.HTTPPathRule.
type mongoHTTPPathRule struct {
	Type  int    `bson:"type"`
	Value string `bson:"value"`
}

// mongoHTTPRouteRule is the BSON representation of domain.HTTPRouteRule.
type mongoHTTPRouteRule struct {
	Backend string            `bson:"backend"`
	Path    mongoHTTPPathRule `bson:"path"`
}

// mongoHTTPRouteSpec is the BSON representation of domain.HTTPRouteSpec.
type mongoHTTPRouteSpec struct {
	ServiceName string               `bson:"service_name"`
	Hostnames   []string             `bson:"hostnames"`
	Rules       []mongoHTTPRouteRule `bson:"rules"`
}

// mongoStatus is the BSON representation of domain.EnvironmentStatus.
type mongoStatus struct {
	State             int       `bson:"state"`
	Message           string    `bson:"message"`
	LastReconcileTime time.Time `bson:"last_reconcile_time"`
	LastSuccessTime   time.Time `bson:"last_success_time"`
}

// MongoRepository stores deploy environments in MongoDB.
type MongoRepository struct {
	collection collectionOps
}

// NewMongoRepository creates a MongoDB-backed repository and ensures indexes eagerly.
func NewMongoRepository(client *mongodriver.Client) (domain.Repository, error) {
	repo := &MongoRepository{
		collection: newCollection(client, DatabaseName, CollectionName),
	}

	if err := repo.ensureIndexes(context.Background()); err != nil {
		return nil, err
	}

	return repo, nil
}

func (r *MongoRepository) ensureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongodriver.IndexModel{
		{
			Keys:    bson.D{{Key: mongoFieldName, Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: mongoFieldScope, Value: 1}, {Key: mongoFieldName, Value: 1}},
			Options: options.Index(),
		},
	})
	if err != nil {
		return fmt.Errorf("create environment indexes: %w", err)
	}

	return nil
}

// Get retrieves an environment by name.
func (r *MongoRepository) Get(ctx context.Context, name domain.EnvironmentName) (*domain.Environment, error) {
	filter := bson.M{mongoFieldName: name.String()}
	doc := new(mongoEnvironment)
	if err := r.collection.FindOne(ctx, filter).Decode(doc); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return doc.toDomain()
}

// ListByStates lists environments matching any of the provided states.
func (r *MongoRepository) ListByStates(ctx context.Context, states ...domain.EnvironmentState) ([]*domain.Environment, error) {
	stateValues := make([]int, len(states))
	for i, state := range states {
		stateValues[i] = int(state)
	}

	filter := bson.M{mongoFieldStatusState: bson.M{"$in": stateValues}}
	findOpts := options.Find().SetSort(bson.D{{Key: mongoFieldName, Value: 1}})
	cur, err := r.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []*mongoEnvironment
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}

	envs := make([]*domain.Environment, 0, len(docs))
	for _, doc := range docs {
		env, err := doc.toDomain()
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}

	return envs, nil
}

// ListByScope lists environments in a scope with stable name ordering.
func (r *MongoRepository) ListByScope(ctx context.Context, scope string, pageSize int32, pageToken string) ([]*domain.Environment, string, error) {
	if pageSize <= 0 {
		pageSize = mongoDefaultPageSize
	}

	skip, err := domain.DecodePageToken(pageToken)
	if err != nil {
		return nil, "", fmt.Errorf("invalid page token: %w", err)
	}

	filter := bson.M{mongoFieldScope: scope}
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, "", err
	}
	if int64(skip) >= total {
		return nil, "", nil
	}

	findOpts := options.Find().SetSkip(int64(skip)).SetLimit(int64(pageSize)).SetSort(bson.D{{Key: mongoFieldName, Value: 1}})
	cur, err := r.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []*mongoEnvironment
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	envs := make([]*domain.Environment, 0, len(docs))
	for _, doc := range docs {
		env, err := doc.toDomain()
		if err != nil {
			return nil, "", err
		}
		envs = append(envs, env)
	}

	nextSkip := skip + len(envs)
	if int64(nextSkip) >= total {
		return envs, "", nil
	}

	return envs, domain.EncodePageToken(nextSkip), nil
}

// Save upserts an environment document by resource name.
func (r *MongoRepository) Save(ctx context.Context, env *domain.Environment) error {
	doc, err := mongoEnvironmentFromDomain(env)
	if err != nil {
		return err
	}

	_, err = r.collection.UpdateOne(
		ctx,
		bson.M{mongoFieldName: doc.Name},
		bson.M{
			"$set":         doc.updateDocument(),
			"$setOnInsert": doc.insertDocument(),
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}

	return nil
}

// Delete removes an environment by name.
func (r *MongoRepository) Delete(ctx context.Context, name domain.EnvironmentName) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{mongoFieldName: name.String()})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func mongoEnvironmentFromDomain(env *domain.Environment) (*mongoEnvironment, error) {
	return &mongoEnvironment{
		Name:         env.Name().String(),
		Scope:        env.Name().Scope(),
		EnvName:      env.Name().EnvName(),
		Description:  env.Description(),
		DesiredState: desiredStateToMongo(env.DesiredState()),
		Status:       statusToMongo(env.Status()),
		CreateTime:   env.CreateTime(),
		UpdateTime:   env.UpdateTime(),
		ETag:         env.ETag(),
	}, nil
}

func desiredStateToMongo(ds *domain.DesiredState) *mongoDesiredState {
	if ds == nil {
		return nil
	}
	return &mongoDesiredState{
		Services:   serviceSpecsToMongo(ds.Services),
		Infras:     infraSpecsToMongo(ds.Infras),
		HTTPRoutes: httpRouteSpecsToMongo(ds.HTTPRoutes),
	}
}

func serviceSpecsToMongo(specs []*domain.ServiceSpec) []mongoServiceSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]mongoServiceSpec, len(specs))
	for i, s := range specs {
		result[i] = mongoServiceSpec{
			Name:       s.Name,
			App:        s.App,
			Image:      s.Image,
			Ports:      servicePortSpecsToMongo(s.Ports),
			Replicas:   s.Replicas,
			TLSEnabled: s.TLSEnabled,
		}
	}
	return result
}

func servicePortSpecsToMongo(specs []domain.ServicePortSpec) []mongoServicePortSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]mongoServicePortSpec, len(specs))
	for i, p := range specs {
		result[i] = mongoServicePortSpec{Name: p.Name, Port: p.Port}
	}
	return result
}

func infraSpecsToMongo(specs []*domain.InfraSpec) []mongoInfraSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]mongoInfraSpec, len(specs))
	for i, s := range specs {
		result[i] = mongoInfraSpec{
			Resource:           s.Resource,
			Profile:            s.Profile,
			Name:               s.Name,
			App:                s.App,
			PersistenceEnabled: s.PersistenceEnabled,
		}
	}
	return result
}

func httpRouteSpecsToMongo(specs []*domain.HTTPRouteSpec) []mongoHTTPRouteSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]mongoHTTPRouteSpec, len(specs))
	for i, s := range specs {
		result[i] = mongoHTTPRouteSpec{
			ServiceName: s.ServiceName,
			Hostnames:   s.Hostnames,
			Rules:       httpRouteRulesToMongo(s.Rules),
		}
	}
	return result
}

func httpRouteRulesToMongo(rules []domain.HTTPRouteRule) []mongoHTTPRouteRule {
	if len(rules) == 0 {
		return nil
	}
	result := make([]mongoHTTPRouteRule, len(rules))
	for i, r := range rules {
		result[i] = mongoHTTPRouteRule{
			Backend: r.Backend,
			Path: mongoHTTPPathRule{
				Type:  int(r.Path.Type),
				Value: r.Path.Value,
			},
		}
	}
	return result
}

func statusToMongo(s *domain.EnvironmentStatus) *mongoStatus {
	if s == nil {
		return nil
	}
	return &mongoStatus{
		State:             int(s.State),
		Message:           s.Message,
		LastReconcileTime: s.LastReconcileTime,
		LastSuccessTime:   s.LastSuccessTime,
	}
}

func (m *mongoEnvironment) updateDocument() bson.M {
	return bson.M{
		mongoFieldDescription:  m.Description,
		mongoFieldDesiredState: m.DesiredState,
		mongoFieldStatus:       m.Status,
		mongoFieldUpdateTime:   m.UpdateTime,
		mongoFieldETag:         m.ETag,
	}
}

func (m *mongoEnvironment) insertDocument() bson.M {
	return bson.M{
		mongoFieldName:       m.Name,
		mongoFieldScope:      m.Scope,
		mongoFieldEnvName:    m.EnvName,
		mongoFieldCreateTime: m.CreateTime,
	}
}

func (m *mongoEnvironment) toDomain() (*domain.Environment, error) {
	name, err := domain.NewEnvironmentName(m.Scope, m.EnvName)
	if err != nil {
		return nil, err
	}

	return domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
		Name:         name,
		Description:  m.Description,
		DesiredState: desiredStateFromMongo(m.DesiredState),
		Status:       statusFromMongo(m.Status),
		CreateTime:   m.CreateTime,
		UpdateTime:   m.UpdateTime,
		ETag:         m.ETag,
	})
}

func desiredStateFromMongo(mds *mongoDesiredState) *domain.DesiredState {
	if mds == nil {
		return nil
	}
	return &domain.DesiredState{
		Services:   serviceSpecsFromMongo(mds.Services),
		Infras:     infraSpecsFromMongo(mds.Infras),
		HTTPRoutes: httpRouteSpecsFromMongo(mds.HTTPRoutes),
	}
}

func serviceSpecsFromMongo(specs []mongoServiceSpec) []*domain.ServiceSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]*domain.ServiceSpec, len(specs))
	for i, s := range specs {
		result[i] = &domain.ServiceSpec{
			Name:       s.Name,
			App:        s.App,
			Image:      s.Image,
			Ports:      servicePortSpecsFromMongo(s.Ports),
			Replicas:   s.Replicas,
			TLSEnabled: s.TLSEnabled,
		}
	}
	return result
}

func servicePortSpecsFromMongo(specs []mongoServicePortSpec) []domain.ServicePortSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]domain.ServicePortSpec, len(specs))
	for i, p := range specs {
		result[i] = domain.ServicePortSpec{Name: p.Name, Port: p.Port}
	}
	return result
}

func infraSpecsFromMongo(specs []mongoInfraSpec) []*domain.InfraSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]*domain.InfraSpec, len(specs))
	for i, s := range specs {
		result[i] = &domain.InfraSpec{
			Resource:           s.Resource,
			Profile:            s.Profile,
			Name:               s.Name,
			App:                s.App,
			PersistenceEnabled: s.PersistenceEnabled,
		}
	}
	return result
}

func httpRouteSpecsFromMongo(specs []mongoHTTPRouteSpec) []*domain.HTTPRouteSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]*domain.HTTPRouteSpec, len(specs))
	for i, s := range specs {
		result[i] = &domain.HTTPRouteSpec{
			ServiceName: s.ServiceName,
			Hostnames:   s.Hostnames,
			Rules:       httpRouteRulesFromMongo(s.Rules),
		}
	}
	return result
}

func httpRouteRulesFromMongo(rules []mongoHTTPRouteRule) []domain.HTTPRouteRule {
	if len(rules) == 0 {
		return nil
	}
	result := make([]domain.HTTPRouteRule, len(rules))
	for i, r := range rules {
		result[i] = domain.HTTPRouteRule{
			Backend: r.Backend,
			Path: domain.HTTPPathRule{
				Type:  domain.HTTPPathRuleType(r.Path.Type),
				Value: r.Path.Value,
			},
		}
	}
	return result
}

func statusFromMongo(ms *mongoStatus) *domain.EnvironmentStatus {
	if ms == nil {
		return nil
	}
	return &domain.EnvironmentStatus{
		State:             domain.EnvironmentState(ms.State),
		Message:           ms.Message,
		LastReconcileTime: ms.LastReconcileTime,
		LastSuccessTime:   ms.LastSuccessTime,
	}
}
