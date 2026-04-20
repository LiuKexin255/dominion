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

type EnvironmentType string

const (
	// DatabaseName is the MongoDB database used by the deploy service.
	DatabaseName = "deploy"
	// CollectionName is the MongoDB collection used for environments.
	CollectionName = "environments"
	// mongoDefaultPageSize is the default number of environments returned by ListByScope.
	mongoDefaultPageSize        = 100
	mongoFieldName              = "name"
	mongoFieldScope             = "scope"
	mongoFieldEnvName           = "env_name"
	mongoFieldEnvType           = "env_type"
	mongoFieldDescription       = "description"
	mongoFieldDesiredState      = "desired_state"
	mongoFieldStatus            = "status"
	mongoFieldStatusState       = "status.state"
	mongoFieldStatusDesired     = "status.desired"
	mongoFieldStatusObservedGen = "status.observed_generation"
	mongoFieldGeneration        = "generation"
	mongoFieldCreateTime        = "create_time"
	mongoFieldUpdateTime        = "update_time"
	mongoFieldETag              = "etag"

	EnvironmentTypeProd        = "prod"
	EnvironmentTypeDev         = "dev"
	EnvironmentTypeTest        = "test"
	EnvironmentTypeUnspecified = "unspecified"
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
	EnvType      EnvironmentType    `bson:"env_type"`
	Description  string             `bson:"description"`
	DesiredState *mongoDesiredState `bson:"desired_state"`
	Status       *mongoStatus       `bson:"status"`
	Generation   int64              `bson:"generation"`
	CreateTime   time.Time          `bson:"create_time"`
	UpdateTime   time.Time          `bson:"update_time"`
	ETag         string             `bson:"etag"`
}

// mongoDesiredState is the BSON representation of domain.DesiredState.
type mongoDesiredState struct {
	Artifacts []mongoArtifactSpec `bson:"artifacts"`
	Infras    []mongoInfraSpec    `bson:"infras"`
}

// mongoArtifactPortSpec is the BSON representation of domain.ArtifactPortSpec.
type mongoArtifactPortSpec struct {
	Name string `bson:"name"`
	Port int32  `bson:"port"`
}

// mongoArtifactSpec is the BSON representation of domain.ArtifactSpec.
type mongoArtifactSpec struct {
	Name         string                  `bson:"name"`
	App          string                  `bson:"app"`
	Image        string                  `bson:"image"`
	Ports        []mongoArtifactPortSpec `bson:"ports"`
	Replicas     int32                   `bson:"replicas"`
	TLSEnabled   bool                    `bson:"tls_enabled"`
	WorkloadKind int                     `bson:"workload_kind"`
	HTTP         *mongoArtifactHTTPSpec  `bson:"http,omitempty"`
}

// mongoInfraSpec is the BSON representation of domain.InfraSpec.
type mongoInfraSpec struct {
	Resource    string                    `bson:"resource"`
	Profile     string                    `bson:"profile"`
	Name        string                    `bson:"name"`
	App         string                    `bson:"app"`
	Persistence mongoInfraPersistenceSpec `bson:"persistence"`
}

// mongoInfraPersistenceSpec is the BSON representation of domain.InfraPersistenceSpec.
type mongoInfraPersistenceSpec struct {
	Enabled bool `bson:"enabled"`
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

// mongoArtifactHTTPSpec is the BSON representation of domain.ArtifactHTTPSpec.
type mongoArtifactHTTPSpec struct {
	Hostnames []string             `bson:"hostnames"`
	Matches   []mongoHTTPRouteRule `bson:"matches"`
}

// mongoStatus is the BSON representation of domain.EnvironmentStatus.
type mongoStatus struct {
	Desired            int       `bson:"desired"`
	State              int       `bson:"state"`
	ObservedGeneration int64     `bson:"observed_generation"`
	Message            string    `bson:"message"`
	LastReconcileTime  time.Time `bson:"last_reconcile_time"`
	LastSuccessTime    time.Time `bson:"last_success_time"`
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

// ListNeedingReconcile lists environments that still need reconciliation.
func (r *MongoRepository) ListNeedingReconcile(ctx context.Context) ([]*domain.Environment, error) {
	filter := bson.D{
		{
			Key: "$or",
			Value: bson.A{
				// Condition 1: desired == Present && observed_generation < generation
				bson.D{
					{Key: mongoFieldStatusDesired, Value: int(domain.DesiredPresent)},
					{Key: "$expr", Value: bson.D{
						{Key: "$lt", Value: bson.A{"$" + mongoFieldStatusObservedGen, "$" + mongoFieldGeneration}},
					}},
				},
				// Condition 2: desired == Present && state == Failed && observed_generation == generation
				bson.D{
					{Key: mongoFieldStatusDesired, Value: int(domain.DesiredPresent)},
					{Key: mongoFieldStatusState, Value: int(domain.StateFailed)},
					{Key: "$expr", Value: bson.D{
						{Key: "$eq", Value: bson.A{"$" + mongoFieldStatusObservedGen, "$" + mongoFieldGeneration}},
					}},
				},
				// Condition 3: desired == Absent
				bson.D{
					{Key: mongoFieldStatusDesired, Value: int(domain.DesiredAbsent)},
				},
			},
		},
	}

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

	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{mongoFieldName: doc.Name, mongoFieldGeneration: bson.M{"$lte": doc.Generation}},
		bson.M{
			"$set":         doc.updateDocument(),
			"$setOnInsert": doc.insertDocument(),
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	if result.MatchedCount == 0 {
		return nil
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

func envTypeToEnum(t domain.EnvironmentType) EnvironmentType {
	switch t {
	case domain.EnvironmentTypeProd:
		return EnvironmentTypeProd
	case domain.EnvironmentTypeTest:
		return EnvironmentTypeTest
	case domain.EnvironmentTypeDev:
		return EnvironmentTypeDev
	default:
		return EnvironmentTypeUnspecified
	}
}

func envTypeFromEnum(s EnvironmentType) domain.EnvironmentType {
	switch s {
	case EnvironmentTypeProd:
		return domain.EnvironmentTypeProd
	case EnvironmentTypeTest:
		return domain.EnvironmentTypeTest
	case EnvironmentTypeDev:
		return domain.EnvironmentTypeDev
	default:
		return domain.EnvironmentTypeUnspecified
	}
}

func mongoEnvironmentFromDomain(env *domain.Environment) (*mongoEnvironment, error) {
	return &mongoEnvironment{
		Name:         env.Name().String(),
		Scope:        env.Name().Scope(),
		EnvName:      env.Name().EnvName(),
		EnvType:      envTypeToEnum(env.Type()),
		Description:  env.Description(),
		DesiredState: desiredStateToMongo(env.DesiredState()),
		Status:       statusToMongo(env.Status()),
		Generation:   env.Generation(),
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
		Artifacts: artifactSpecsToMongo(ds.Artifacts),
		Infras:    infraSpecsToMongo(ds.Infras),
	}
}

func artifactSpecsToMongo(specs []*domain.ArtifactSpec) []mongoArtifactSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]mongoArtifactSpec, len(specs))
	for i, s := range specs {
		result[i] = mongoArtifactSpec{
			Name:         s.Name,
			App:          s.App,
			Image:        s.Image,
			Ports:        artifactPortSpecsToMongo(s.Ports),
			Replicas:     s.Replicas,
			TLSEnabled:   s.TLSEnabled,
			WorkloadKind: int(s.WorkloadKind),
			HTTP:         artifactHTTPSpecToMongo(s.HTTP),
		}
	}
	return result
}

func artifactPortSpecsToMongo(specs []domain.ArtifactPortSpec) []mongoArtifactPortSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]mongoArtifactPortSpec, len(specs))
	for i, p := range specs {
		result[i] = mongoArtifactPortSpec{Name: p.Name, Port: p.Port}
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
			Resource:    s.Resource,
			Profile:     s.Profile,
			Name:        s.Name,
			App:         s.App,
			Persistence: mongoInfraPersistenceSpec{Enabled: s.Persistence.Enabled},
		}
	}
	return result
}

func artifactHTTPSpecToMongo(s *domain.ArtifactHTTPSpec) *mongoArtifactHTTPSpec {
	if s == nil {
		return nil
	}
	return &mongoArtifactHTTPSpec{
		Hostnames: s.Hostnames,
		Matches:   httpRouteRulesToMongo(s.Matches),
	}
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
		Desired:            int(s.Desired),
		State:              int(s.State),
		ObservedGeneration: s.ObservedGeneration,
		Message:            s.Message,
		LastReconcileTime:  s.LastReconcileTime,
		LastSuccessTime:    s.LastSuccessTime,
	}
}

func (m *mongoEnvironment) updateDocument() bson.M {
	return bson.M{
		mongoFieldDescription:  m.Description,
		mongoFieldDesiredState: m.DesiredState,
		mongoFieldStatus:       m.Status,
		mongoFieldGeneration:   m.Generation,
		mongoFieldUpdateTime:   m.UpdateTime,
		mongoFieldETag:         m.ETag,
	}
}

func (m *mongoEnvironment) insertDocument() bson.M {
	return bson.M{
		mongoFieldName:       m.Name,
		mongoFieldScope:      m.Scope,
		mongoFieldEnvName:    m.EnvName,
		mongoFieldEnvType:    m.EnvType,
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
		EnvType:      envTypeFromEnum(m.EnvType),
		Description:  m.Description,
		DesiredState: desiredStateFromMongo(m.DesiredState),
		Status:       statusFromMongo(m.Status),
		Generation:   m.Generation,
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
		Artifacts: artifactSpecsFromMongo(mds.Artifacts),
		Infras:    infraSpecsFromMongo(mds.Infras),
	}
}

func artifactSpecsFromMongo(specs []mongoArtifactSpec) []*domain.ArtifactSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]*domain.ArtifactSpec, len(specs))
	for i, s := range specs {
		result[i] = &domain.ArtifactSpec{
			Name:         s.Name,
			App:          s.App,
			Image:        s.Image,
			Ports:        artifactPortSpecsFromMongo(s.Ports),
			Replicas:     s.Replicas,
			TLSEnabled:   s.TLSEnabled,
			WorkloadKind: domain.WorkloadKind(s.WorkloadKind),
			HTTP:         artifactHTTPSpecFromMongo(s.HTTP),
		}
	}
	return result
}

func artifactPortSpecsFromMongo(specs []mongoArtifactPortSpec) []domain.ArtifactPortSpec {
	if len(specs) == 0 {
		return nil
	}
	result := make([]domain.ArtifactPortSpec, len(specs))
	for i, p := range specs {
		result[i] = domain.ArtifactPortSpec{Name: p.Name, Port: p.Port}
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
			Resource:    s.Resource,
			Profile:     s.Profile,
			Name:        s.Name,
			App:         s.App,
			Persistence: domain.InfraPersistenceSpec{Enabled: s.Persistence.Enabled},
		}
	}
	return result
}

func artifactHTTPSpecFromMongo(m *mongoArtifactHTTPSpec) *domain.ArtifactHTTPSpec {
	if m == nil {
		return nil
	}
	return &domain.ArtifactHTTPSpec{
		Hostnames: m.Hostnames,
		Matches:   httpRouteRulesFromMongo(m.Matches),
	}
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
		Desired:            domain.EnvironmentDesired(ms.Desired),
		State:              domain.EnvironmentState(ms.State),
		ObservedGeneration: ms.ObservedGeneration,
		Message:            ms.Message,
		LastReconcileTime:  ms.LastReconcileTime,
		LastSuccessTime:    ms.LastSuccessTime,
	}
}
