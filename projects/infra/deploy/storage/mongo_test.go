package storage

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"dominion/projects/infra/deploy/domain"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestRepositoryContract_MongoRepository(t *testing.T) {
	ctx := context.Background()
	repo, _ := newMongoRepositoryForTest()

	created := newMongoSaveTestEnv(t, "dev", "alpha", "created environment", "image:v1", "etag-create")
	if err := repo.Save(ctx, created); err != nil {
		t.Fatalf("Save() create unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, created.Name())
	if err != nil {
		t.Fatalf("Get() after create unexpected error: %v", err)
	}
	assertEnvironmentEqual(t, got, created)

	listed, nextToken, err := repo.ListByScope(ctx, "dev", 10, "")
	if err != nil {
		t.Fatalf("ListByScope() after create unexpected error: %v", err)
	}
	assertEnvironmentNames(t, listed, []string{"alpha"})
	if nextToken != "" {
		t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
	}

	updated := newMongoSaveTestEnv(t, "dev", "alpha", "updated environment", "image:v2", "etag-update")
	if err := repo.Save(ctx, updated); err != nil {
		t.Fatalf("Save() overwrite unexpected error: %v", err)
	}

	got, err = repo.Get(ctx, updated.Name())
	if err != nil {
		t.Fatalf("Get() after overwrite unexpected error: %v", err)
	}
	assertEnvironmentEqual(t, got, updated)

	missingName, err := domain.NewEnvironmentName("dev", "ghost")
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}
	_, err = repo.Get(ctx, missingName)
	assertNotFoundError(t, err, "Get() missing")

	if err := repo.Delete(ctx, updated.Name()); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	_, err = repo.Get(ctx, updated.Name())
	assertNotFoundError(t, err, "Get() after delete")

	err = repo.Delete(ctx, updated.Name())
	assertNotFoundError(t, err, "Delete() missing")
}

func TestRepositoryContract_MongoRepository_InvalidPageToken(t *testing.T) {
	ctx := context.Background()
	repo, _ := newMongoRepositoryForTest()

	results, nextToken, err := repo.ListByScope(ctx, "dev", 2, "not-base64")
	if err == nil {
		t.Fatal("ListByScope() error = nil, want non-nil")
	}
	if err.Error() != "invalid page token: invalid page token" {
		t.Fatalf("ListByScope() error = %q, want %q", err.Error(), "invalid page token: invalid page token")
	}
	if results != nil {
		t.Fatalf("ListByScope() results = %#v, want nil", results)
	}
	if nextToken != "" {
		t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
	}
}

func TestRepositoryContract_MongoRepository_Pagination(t *testing.T) {
	ctx := context.Background()

	t.Run("pages through a scoped list", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		envs := []*domain.Environment{
			newMongoSaveTestEnv(t, "dev", "cc", "env cc", "image:v3", "etag-cc"),
			newMongoSaveTestEnv(t, "dev", "aa", "env aa", "image:v1", "etag-aa"),
			newMongoSaveTestEnv(t, "dev", "ee", "env ee", "image:v5", "etag-ee"),
			newMongoSaveTestEnv(t, "dev", "bb", "env bb", "image:v2", "etag-bb"),
			newMongoSaveTestEnv(t, "dev", "dd", "env dd", "image:v4", "etag-dd"),
			newMongoSaveTestEnv(t, "prod", "zz", "env zz", "image:v9", "etag-zz"),
		}
		for _, env := range envs {
			if err := repo.Save(ctx, env); err != nil {
				t.Fatalf("Save() unexpected error: %v", err)
			}
		}

		// when
		page1, nextToken1, err := repo.ListByScope(ctx, "dev", 2, "")
		if err != nil {
			t.Fatalf("ListByScope() page1 error = %v", err)
		}
		page2, nextToken2, err := repo.ListByScope(ctx, "dev", 2, nextToken1)
		if err != nil {
			t.Fatalf("ListByScope() page2 error = %v", err)
		}
		page3, nextToken3, err := repo.ListByScope(ctx, "dev", 2, nextToken2)
		if err != nil {
			t.Fatalf("ListByScope() page3 error = %v", err)
		}

		// then
		assertEnvironmentNames(t, page1, []string{"aa", "bb"})
		if nextToken1 != domain.EncodePageToken(2) {
			t.Fatalf("page1 nextToken = %q, want %q", nextToken1, domain.EncodePageToken(2))
		}
		assertEnvironmentNames(t, page2, []string{"cc", "dd"})
		if nextToken2 != domain.EncodePageToken(4) {
			t.Fatalf("page2 nextToken = %q, want %q", nextToken2, domain.EncodePageToken(4))
		}
		assertEnvironmentNames(t, page3, []string{"ee"})
		if nextToken3 != "" {
			t.Fatalf("page3 nextToken = %q, want empty", nextToken3)
		}

		assertBSONMapEqual(t, collection.lastCountFilter, bson.M{"scope": "dev"}, "CountDocuments() filter")
		assertBSONMapEqual(t, collection.lastFindFilter, bson.M{"scope": "dev"}, "Find() filter")
		assertFindOptions(t, collection.lastFindOptions, 4, 2, bson.D{{Key: "name", Value: 1}})
		if collection.findCalls != 3 {
			t.Fatalf("Find() calls = %d, want 3", collection.findCalls)
		}
	})

	t.Run("uses default page size when zero", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		if err := repo.Save(ctx, newMongoSaveTestEnv(t, "dev", "env1", "env1", "image:v1", "etag-1")); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		results, nextToken, err := repo.ListByScope(ctx, "dev", 0, "")

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		assertEnvironmentNames(t, results, []string{"env1"})
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
		assertFindOptions(t, collection.lastFindOptions, 0, mongoDefaultPageSize, bson.D{{Key: "name", Value: 1}})
	})

	t.Run("returns nil page when token is out of range", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		if err := repo.Save(ctx, newMongoSaveTestEnv(t, "dev", "env1", "env1", "image:v1", "etag-1")); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		results, nextToken, err := repo.ListByScope(ctx, "dev", 2, domain.EncodePageToken(3))

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		if results != nil {
			t.Fatalf("ListByScope() results = %#v, want nil", results)
		}
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
		if collection.findCalls != 0 {
			t.Fatalf("Find() calls = %d, want 0", collection.findCalls)
		}
	})
}

func TestRepositoryContract_MongoRepository_Ordering(t *testing.T) {
	ctx := context.Background()
	repo, collection := newMongoRepositoryForTest()

	envs := []*domain.Environment{
		newMongoSaveTestEnv(t, "dev", "charlie", "env charlie", "image:v3", "etag-charlie"),
		newMongoSaveTestEnv(t, "dev", "alpha", "env alpha", "image:v1", "etag-alpha"),
		newMongoSaveTestEnv(t, "dev", "bravo", "env bravo", "image:v2", "etag-bravo"),
		newMongoSaveTestEnv(t, "prod", "aardvark", "env aardvark", "image:v9", "etag-prod"),
	}
	for _, env := range envs {
		if err := repo.Save(ctx, env); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}
	}

	got, nextToken, err := repo.ListByScope(ctx, "dev", 10, "")
	if err != nil {
		t.Fatalf("ListByScope() unexpected error: %v", err)
	}
	assertEnvironmentNames(t, got, []string{"alpha", "bravo", "charlie"})
	if nextToken != "" {
		t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
	}
	assertBSONMapEqual(t, collection.lastFindFilter, bson.M{"scope": "dev"}, "Find() filter")
	assertFindOptions(t, collection.lastFindOptions, 0, 10, bson.D{{Key: "name", Value: 1}})
}

func TestArtifactSpecs_WorkloadKindPersistence(t *testing.T) {
	tests := []struct {
		name       string
		source     *domain.ArtifactSpec
		mongo      mongoArtifactSpec
		wantKind   domain.WorkloadKind
		checkRound bool
	}{
		{
			name: "stateless round trip",
			source: &domain.ArtifactSpec{
				Name:         "svc",
				App:          "app",
				Image:        "image:v1",
				Replicas:     1,
				WorkloadKind: domain.WorkloadKindStateless,
			},
			wantKind:   domain.WorkloadKindStateless,
			checkRound: true,
		},
		{
			name: "stateful round trip",
			source: &domain.ArtifactSpec{
				Name:         "svc",
				App:          "app",
				Image:        "image:v1",
				Replicas:     1,
				WorkloadKind: domain.WorkloadKindStateful,
			},
			wantKind:   domain.WorkloadKindStateful,
			checkRound: true,
		},
		{
			name: "old mongo document defaults to stateless",
			mongo: mongoArtifactSpec{
				Name:     "svc",
				App:      "app",
				Image:    "image:v1",
				Replicas: 1,
			},
			wantKind: domain.WorkloadKindStateless,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			var mongoSpecs []mongoArtifactSpec
			if tt.source != nil {
				// when
				mongoSpecs = artifactSpecsToMongo([]*domain.ArtifactSpec{tt.source})

				// then
				if len(mongoSpecs) != 1 {
					t.Fatalf("artifactSpecsToMongo() len = %d, want 1", len(mongoSpecs))
				}
				if mongoSpecs[0].WorkloadKind != int(tt.wantKind) {
					t.Fatalf("artifactSpecsToMongo() workload_kind = %d, want %d", mongoSpecs[0].WorkloadKind, tt.wantKind)
				}
			} else {
				// when
				mongoSpecs = []mongoArtifactSpec{tt.mongo}
			}

			got := artifactSpecsFromMongo(mongoSpecs)

			// then
			if len(got) != 1 {
				t.Fatalf("artifactSpecsFromMongo() len = %d, want 1", len(got))
			}
			if got[0].WorkloadKind != tt.wantKind {
				t.Fatalf("artifactSpecsFromMongo() workload kind = %d, want %d", got[0].WorkloadKind, tt.wantKind)
			}
			if tt.checkRound && got[0].WorkloadKind != tt.source.WorkloadKind {
				t.Fatalf("round trip workload kind = %d, want %d", got[0].WorkloadKind, tt.source.WorkloadKind)
			}
		})
	}
}

func TestMongoRepository_Save(t *testing.T) {
	ctx := context.Background()

	t.Run("create new environment", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		env := newMongoSaveTestEnv(t, "dev", "env1", "created environment", "image:v1", "etag-create")

		// when
		err := repo.Save(ctx, env)

		// then
		if err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}
		if collection.lastUpdateFilter == nil {
			t.Fatal("Save() did not call UpdateOne")
		}
		wantFilter := bson.M{"name": env.Name().String(), "generation": bson.M{"$lte": env.Generation()}}
		if !reflect.DeepEqual(collection.lastUpdateFilter, wantFilter) {
			t.Fatalf("UpdateOne() filter = %#v, want %#v", collection.lastUpdateFilter, wantFilter)
		}
		if collection.lastUpdateUpsert == nil || !*collection.lastUpdateUpsert {
			t.Fatalf("UpdateOne() upsert = %v, want true", collection.lastUpdateUpsert)
		}
		assertSaveUpdateDocument(t, collection.lastUpdateUpdate, env)

		got, err := repo.Get(ctx, env.Name())
		if err != nil {
			t.Fatalf("Get() after Save() unexpected error: %v", err)
		}
		assertEnvironmentEqual(t, got, env)
	})

	t.Run("overwrite existing environment", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		original := newMongoSaveTestEnv(t, "dev", "env1", "original environment", "image:v1", "etag-original")
		if err := repo.Save(ctx, original); err != nil {
			t.Fatalf("Save() original unexpected error: %v", err)
		}
		updated := newMongoSaveTestEnv(t, "dev", "env1", "updated environment", "image:v2", "etag-updated")

		// when
		err := repo.Save(ctx, updated)

		// then
		if err != nil {
			t.Fatalf("Save() overwrite unexpected error: %v", err)
		}
		wantFilter := bson.M{"name": updated.Name().String(), "generation": bson.M{"$lte": updated.Generation()}}
		if !reflect.DeepEqual(collection.lastUpdateFilter, wantFilter) {
			t.Fatalf("UpdateOne() filter = %#v, want %#v", collection.lastUpdateFilter, wantFilter)
		}
		assertSaveUpdateDocument(t, collection.lastUpdateUpdate, updated)
		got, err := repo.Get(ctx, updated.Name())
		if err != nil {
			t.Fatalf("Get() after overwrite unexpected error: %v", err)
		}
		assertEnvironmentEqual(t, got, updated)
	})

	t.Run("duplicate key generation conflict is ignored", func(t *testing.T) {
		// given
		repo := &MongoRepository{
			collection: &fakeCollectionOps{
				updateErr: mongodriver.WriteException{
					WriteErrors: []mongodriver.WriteError{{Code: 11000, Message: "duplicate key"}},
				},
			},
		}
		env := newMongoSaveTestEnv(t, "dev", "env1", "duplicate environment", "image:v1", "etag-duplicate")

		// when
		err := repo.Save(ctx, env)

		// then
		if err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}
	})

	t.Run("stale generation update is ignored", func(t *testing.T) {
		// given
		repo, _ := newMongoRepositoryForTest()
		current := newMongoSaveTestEnv(t, "dev", "env1", "current environment", "image:v2", "etag-current")
		if err := repo.Save(ctx, current); err != nil {
			t.Fatalf("Save() current unexpected error: %v", err)
		}
		stale := cloneEnvironmentWithGeneration(t, current, current.Generation()-1)

		// when
		err := repo.Save(ctx, stale)

		// then
		if err != nil {
			t.Fatalf("Save() stale unexpected error: %v", err)
		}
		got, getErr := repo.Get(ctx, current.Name())
		if getErr != nil {
			t.Fatalf("Get() error = %v", getErr)
		}
		assertEnvironmentEqual(t, got, current)
	})
}

func TestMongoRepository_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("delete existing environment", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		env := newMongoSaveTestEnv(t, "dev", "env1", "delete environment", "image:v1", "etag-delete")
		if err := repo.Save(ctx, env); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		err := repo.Delete(ctx, env.Name())

		// then
		if err != nil {
			t.Fatalf("Delete() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(collection.lastDeleteFilter, bson.M{"name": env.Name().String()}) {
			t.Fatalf("DeleteOne() filter = %#v, want %#v", collection.lastDeleteFilter, bson.M{"name": env.Name().String()})
		}
		_, getErr := repo.Get(ctx, env.Name())
		if !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("Get() after Delete() error = %v, want %v", getErr, domain.ErrNotFound)
		}
	})

	t.Run("delete missing environment", func(t *testing.T) {
		// given
		repo, _ := newMongoRepositoryForTest()
		name, err := domain.NewEnvironmentName("dev", "missing")
		if err != nil {
			t.Fatalf("NewEnvironmentName() error = %v", err)
		}

		// when
		err = repo.Delete(ctx, name)

		// then
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Delete() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestMongoRepository_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("get existing environment", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		env := newMongoSaveTestEnv(t, "dev", "env1", "existing environment", "image:v1", "etag-get")
		if err := repo.Save(ctx, env); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		got, err := repo.Get(ctx, env.Name())

		// then
		if err != nil {
			t.Fatalf("Get() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(collection.lastFindOneFilter, bson.M{"name": env.Name().String()}) {
			t.Fatalf("FindOne() filter = %#v, want %#v", collection.lastFindOneFilter, bson.M{"name": env.Name().String()})
		}
		assertEnvironmentEqual(t, got, env)
	})

	t.Run("get missing environment", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		name, err := domain.NewEnvironmentName("dev", "missing")
		if err != nil {
			t.Fatalf("NewEnvironmentName() error = %v", err)
		}

		// when
		_, err = repo.Get(ctx, name)

		// then
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Get() error = %v, want %v", err, domain.ErrNotFound)
		}
		if !reflect.DeepEqual(collection.lastFindOneFilter, bson.M{"name": name.String()}) {
			t.Fatalf("FindOne() filter = %#v, want %#v", collection.lastFindOneFilter, bson.M{"name": name.String()})
		}
	})
}

func TestMongoRepository_ListByScope(t *testing.T) {
	ctx := context.Background()

	t.Run("pagination across multiple pages", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		envs := []*domain.Environment{
			newMongoSaveTestEnv(t, "dev", "cc", "env cc", "image:v3", "etag-cc"),
			newMongoSaveTestEnv(t, "dev", "aa", "env aa", "image:v1", "etag-aa"),
			newMongoSaveTestEnv(t, "dev", "ee", "env ee", "image:v5", "etag-ee"),
			newMongoSaveTestEnv(t, "dev", "bb", "env bb", "image:v2", "etag-bb"),
			newMongoSaveTestEnv(t, "dev", "dd", "env dd", "image:v4", "etag-dd"),
			newMongoSaveTestEnv(t, "prod", "zz", "env zz", "image:v9", "etag-zz"),
		}
		for _, env := range envs {
			if err := repo.Save(ctx, env); err != nil {
				t.Fatalf("Save() unexpected error: %v", err)
			}
		}

		// when
		page1, nextToken1, err := repo.ListByScope(ctx, "dev", 2, "")
		if err != nil {
			t.Fatalf("ListByScope() page1 error = %v", err)
		}
		page2, nextToken2, err := repo.ListByScope(ctx, "dev", 2, nextToken1)
		if err != nil {
			t.Fatalf("ListByScope() page2 error = %v", err)
		}
		page3, nextToken3, err := repo.ListByScope(ctx, "dev", 2, nextToken2)
		if err != nil {
			t.Fatalf("ListByScope() page3 error = %v", err)
		}

		// then
		assertEnvironmentNames(t, page1, []string{"aa", "bb"})
		if nextToken1 != domain.EncodePageToken(2) {
			t.Fatalf("page1 nextToken = %q, want %q", nextToken1, domain.EncodePageToken(2))
		}
		assertEnvironmentNames(t, page2, []string{"cc", "dd"})
		if nextToken2 != domain.EncodePageToken(4) {
			t.Fatalf("page2 nextToken = %q, want %q", nextToken2, domain.EncodePageToken(4))
		}
		assertEnvironmentNames(t, page3, []string{"ee"})
		if nextToken3 != "" {
			t.Fatalf("page3 nextToken = %q, want empty", nextToken3)
		}

		assertBSONMapEqual(t, collection.lastCountFilter, bson.M{"scope": "dev"}, "CountDocuments() filter")
		assertBSONMapEqual(t, collection.lastFindFilter, bson.M{"scope": "dev"}, "Find() filter")
		assertFindOptions(t, collection.lastFindOptions, 4, 2, bson.D{{Key: "name", Value: 1}})
		if collection.findCalls != 3 {
			t.Fatalf("Find() calls = %d, want 3", collection.findCalls)
		}
	})

	t.Run("empty scope returns nil", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		if err := repo.Save(ctx, newMongoSaveTestEnv(t, "dev", "env1", "env1", "image:v1", "etag-1")); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		results, nextToken, err := repo.ListByScope(ctx, "prod", 10, "")

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		if results != nil {
			t.Fatalf("ListByScope() results = %#v, want nil", results)
		}
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
		if collection.findCalls != 0 {
			t.Fatalf("Find() calls = %d, want 0", collection.findCalls)
		}
	})

	t.Run("default page size when zero", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		if err := repo.Save(ctx, newMongoSaveTestEnv(t, "dev", "env1", "env1", "image:v1", "etag-1")); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		results, nextToken, err := repo.ListByScope(ctx, "dev", 0, "")

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		assertEnvironmentNames(t, results, []string{"env1"})
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
		assertFindOptions(t, collection.lastFindOptions, 0, mongoDefaultPageSize, bson.D{{Key: "name", Value: 1}})
	})

	t.Run("out of range token returns nil page", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		if err := repo.Save(ctx, newMongoSaveTestEnv(t, "dev", "env1", "env1", "image:v1", "etag-1")); err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}

		// when
		results, nextToken, err := repo.ListByScope(ctx, "dev", 2, domain.EncodePageToken(3))

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		if results != nil {
			t.Fatalf("ListByScope() results = %#v, want nil", results)
		}
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
		if collection.findCalls != 0 {
			t.Fatalf("Find() calls = %d, want 0", collection.findCalls)
		}
	})

	t.Run("invalid page token", func(t *testing.T) {
		// given
		repo, _ := newMongoRepositoryForTest()

		// when
		_, _, err := repo.ListByScope(ctx, "dev", 2, "not-base64")

		// then
		if err == nil {
			t.Fatal("ListByScope() error = nil, want non-nil")
		}
		if err.Error() != "invalid page token: invalid page token" {
			t.Fatalf("ListByScope() error = %q, want %q", err.Error(), "invalid page token: invalid page token")
		}
	})
}

func TestMongoRepository_ListByStates(t *testing.T) {
	ctx := context.Background()

	t.Run("filters by any requested state", func(t *testing.T) {
		// given
		repo, collection := newMongoRepositoryForTest()
		envs := []*domain.Environment{
			newMongoStateTestEnv(t, "dev", "alpha", "env alpha", domain.StateReady, "etag-alpha"),
			newMongoStateTestEnv(t, "dev", "charlie", "env charlie", domain.StateReconciling, "etag-charlie"),
			newMongoStateTestEnv(t, "prod", "bravo", "env bravo", domain.StateFailed, "etag-bravo"),
			newMongoStateTestEnv(t, "prod", "delta", "env delta", domain.StateReady, "etag-delta"),
		}
		for _, env := range envs {
			if err := repo.Save(ctx, env); err != nil {
				t.Fatalf("Save() unexpected error: %v", err)
			}
		}

		// when
		got, err := repo.ListByStates(ctx, domain.StateFailed, domain.StateReady)

		// then
		if err != nil {
			t.Fatalf("ListByStates() unexpected error: %v", err)
		}
		assertEnvironmentNames(t, got, []string{"alpha", "bravo", "delta"})
		assertBSONMapEqual(t, collection.lastFindFilter, bson.M{"status.state": bson.M{"$in": []int{int(domain.StateFailed), int(domain.StateReady)}}}, "Find() filter")
		assertFindSort(t, collection.lastFindOptions, bson.D{{Key: "name", Value: 1}})
	})
}

func TestMongoRepository_ListNeedingReconcile(t *testing.T) {
	ctx := context.Background()
	baseTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name               string
		envName            string
		desired            domain.EnvironmentDesired
		state              domain.EnvironmentState
		observedGeneration int64
		generation         int64
		wantNames          []string
	}{
		{
			name:               "returns env with desired=Present and observed_generation < generation",
			envName:            "alpha",
			desired:            domain.DesiredPresent,
			state:              domain.StatePending,
			observedGeneration: 0,
			generation:         2,
			wantNames:          []string{"alpha"},
		},
		{
			name:               "returns env with desired=Present and state=Failed and observed_generation == generation",
			envName:            "bravo",
			desired:            domain.DesiredPresent,
			state:              domain.StateFailed,
			observedGeneration: 1,
			generation:         1,
			wantNames:          []string{"bravo"},
		},
		{
			name:               "returns env with desired=Absent",
			envName:            "charlie",
			desired:            domain.DesiredAbsent,
			state:              domain.StateDeleting,
			observedGeneration: 1,
			generation:         2,
			wantNames:          []string{"charlie"},
		},
		{
			name:               "excludes env with desired=Present and state=Ready and observed_generation == generation",
			envName:            "delta",
			desired:            domain.DesiredPresent,
			state:              domain.StateReady,
			observedGeneration: 1,
			generation:         1,
			wantNames:          nil,
		},
		{
			name:               "returns env with desired=Present and state=Failed and observed_generation < generation (condition 1 covers it)",
			envName:            "echo",
			desired:            domain.DesiredPresent,
			state:              domain.StateFailed,
			observedGeneration: 1,
			generation:         3,
			wantNames:          []string{"echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo, _ := newMongoRepositoryForTest()
			env := newReconcileTestEnv(t, tt.envName, tt.desired, tt.state, tt.observedGeneration, tt.generation, baseTime)
			if err := repo.Save(ctx, env); err != nil {
				t.Fatalf("Save() unexpected error: %v", err)
			}

			// when
			got, err := repo.ListNeedingReconcile(ctx)

			// then
			if err != nil {
				t.Fatalf("ListNeedingReconcile() unexpected error: %v", err)
			}
			assertEnvironmentNames(t, got, tt.wantNames)
		})
	}
}

func newReconcileTestEnv(t *testing.T, envName string, desired domain.EnvironmentDesired, state domain.EnvironmentState, observedGeneration, generation int64, baseTime time.Time) *domain.Environment {
	t.Helper()

	name, err := domain.NewEnvironmentName("dev", envName)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}
	env, err := domain.NewEnvironment(name, domain.EnvironmentTypeProd, envName+" env", &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{Name: "svc1", App: "app1", Image: "image:v1", Replicas: 1}},
	})
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	rehydrated, err := domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
		Name:         name,
		EnvType:      domain.EnvironmentTypeProd,
		Description:  envName + " env",
		DesiredState: env.DesiredState(),
		Status: &domain.EnvironmentStatus{
			Desired:            desired,
			State:              state,
			ObservedGeneration: observedGeneration,
			Message:            "state",
		},
		Generation: generation,
		CreateTime: baseTime,
		UpdateTime: baseTime,
		ETag:       "etag-" + envName,
	})
	if err != nil {
		t.Fatalf("RehydrateEnvironment() error = %v", err)
	}

	return rehydrated
}

func TestMongoRepository_NewMongoRepository_UsesDeployCollection(t *testing.T) {
	originalNewCollection := newCollection
	t.Cleanup(func() {
		newCollection = originalNewCollection
	})

	fakeCollection := &fakeCollectionOps{}
	var gotDB string
	var gotCollection string
	newCollection = func(_ *mongodriver.Client, db string, coll string) collectionOps {
		gotDB = db
		gotCollection = coll
		return fakeCollection
	}

	repo, err := NewMongoRepository(nil)
	if err != nil {
		t.Fatalf("NewMongoRepository() error = %v", err)
	}
	if repo == nil {
		t.Fatal("NewMongoRepository() returned nil repository")
	}
	if gotDB != DatabaseName {
		t.Fatalf("database = %q, want %q", gotDB, DatabaseName)
	}
	if gotCollection != CollectionName {
		t.Fatalf("collection = %q, want %q", gotCollection, CollectionName)
	}
	if len(fakeCollection.indexes.models) != 2 {
		t.Fatalf("index count = %d, want 2", len(fakeCollection.indexes.models))
	}

	assertIndexModel(t, fakeCollection.indexes.models[0], bson.D{{Key: "name", Value: 1}}, true)
	assertIndexModel(t, fakeCollection.indexes.models[1], bson.D{{Key: "scope", Value: 1}, {Key: "name", Value: 1}}, false)
}

func TestMongoRepository_NewMongoRepository_ReturnsIndexError(t *testing.T) {
	originalNewCollection := newCollection
	t.Cleanup(func() {
		newCollection = originalNewCollection
	})

	newCollection = func(_ *mongodriver.Client, _, _ string) collectionOps {
		return &fakeCollectionOps{
			indexes: fakeIndexViewOps{err: errors.New("boom")},
		}
	}

	repo, err := NewMongoRepository(nil)
	if err == nil {
		t.Fatal("NewMongoRepository() error = nil, want non-nil")
	}
	if repo != nil {
		t.Fatalf("NewMongoRepository() repository = %#v, want nil", repo)
	}
	if err.Error() != "create environment indexes: boom" {
		t.Fatalf("NewMongoRepository() error = %q, want %q", err.Error(), "create environment indexes: boom")
	}
}

func Test_envTypeToString(t *testing.T) {
	tests := []struct {
		name  string
		input domain.EnvironmentType
		want  EnvironmentType
	}{
		{name: "prod", input: domain.EnvironmentTypeProd, want: "prod"},
		{name: "test", input: domain.EnvironmentTypeTest, want: "test"},
		{name: "dev", input: domain.EnvironmentTypeDev, want: "dev"},
		{name: "unknown", input: domain.EnvironmentTypeUnspecified, want: "unspecified"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envTypeToEnum(tt.input)
			if got != tt.want {
				t.Fatalf("envTypeToString(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func Test_envTypeFromString(t *testing.T) {
	tests := []struct {
		name  string
		input EnvironmentType
		want  domain.EnvironmentType
	}{
		{name: "prod", input: "prod", want: domain.EnvironmentTypeProd},
		{name: "test", input: "test", want: domain.EnvironmentTypeTest},
		{name: "dev", input: "dev", want: domain.EnvironmentTypeDev},
		{name: "empty", input: "", want: domain.EnvironmentTypeUnspecified},
		{name: "unknown", input: "unknown", want: domain.EnvironmentTypeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envTypeFromEnum(tt.input)
			if got != tt.want {
				t.Fatalf("envTypeFromString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func assertIndexModel(t *testing.T, model mongodriver.IndexModel, wantKeys bson.D, wantUnique bool) {
	t.Helper()

	gotKeys, ok := model.Keys.(bson.D)
	if !ok {
		t.Fatalf("index keys type = %T, want bson.D", model.Keys)
	}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("index keys length = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("index key[%d] = %#v, want %#v", i, gotKeys[i], wantKeys[i])
		}
	}

	gotUnique := false
	if model.Options != nil && model.Options.Unique != nil {
		gotUnique = *model.Options.Unique
	}
	if gotUnique != wantUnique {
		t.Fatalf("index unique = %v, want %v", gotUnique, wantUnique)
	}
}

type fakeCollectionOps struct {
	indexes           fakeIndexViewOps
	docs              map[string]*mongoEnvironment
	updateErr         error
	deleteErr         error
	lastFindOneFilter any
	lastFindFilter    any
	lastFindOptions   *options.FindOptions
	lastCountFilter   any
	lastUpdateFilter  any
	lastUpdateUpdate  any
	lastUpdateUpsert  *bool
	lastDeleteFilter  any
	findCalls         int
}

func (f *fakeCollectionOps) FindOne(_ context.Context, filter any, _ ...*options.FindOneOptions) singleResult {
	f.lastFindOneFilter = filter
	filterDoc, _ := anyToBSONMap(filter)
	name, _ := filterDoc["name"].(string)
	if f.docs != nil {
		if doc, ok := f.docs[name]; ok {
			return fakeSingleResult{doc: doc}
		}
	}

	return fakeSingleResult{err: mongodriver.ErrNoDocuments}
}

func (f *fakeCollectionOps) Find(_ context.Context, filter any, opts ...*options.FindOptions) (cursor, error) {
	f.findCalls++
	f.lastFindFilter = filter
	if len(opts) > 0 {
		f.lastFindOptions = opts[0]
	}

	filterDoc, err := anyToBSONMap(filter)
	if err != nil {
		return nil, err
	}

	docs := make([]*mongoEnvironment, 0)
	for _, doc := range f.docs {
		if matchesFilter(doc, filterDoc) {
			copy := *doc
			docs = append(docs, &copy)
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Name < docs[j].Name
	})

	skip := 0
	limit := len(docs)
	if f.lastFindOptions != nil {
		if f.lastFindOptions.Skip != nil {
			skip = int(*f.lastFindOptions.Skip)
		}
		if f.lastFindOptions.Limit != nil {
			limit = int(*f.lastFindOptions.Limit)
		}
	}
	if skip >= len(docs) {
		return fakeCursor{docs: nil}, nil
	}
	end := min(skip+limit, len(docs))

	return fakeCursor{docs: docs[skip:end]}, nil
}

func matchesFilter(doc *mongoEnvironment, filter bson.M) bool {
	for key, value := range filter {
		switch key {
		case "scope":
			if doc.Scope != value.(string) {
				return false
			}
		case "status.state":
			if !matchesStateFilter(doc, value) {
				return false
			}
		case "$or":
			if !matchesOrFilter(doc, value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func matchesStateFilter(doc *mongoEnvironment, value any) bool {
	stateFilterDoc, ok := value.(bson.M)
	if !ok {
		return false
	}
	inVals, ok := stateFilterDoc["$in"]
	if !ok {
		return false
	}
	rv := reflect.ValueOf(inVals)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return false
	}
	if doc.Status == nil {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if toInt(rv.Index(i).Interface()) == doc.Status.State {
			return true
		}
	}
	return false
}

func matchesOrFilter(doc *mongoEnvironment, value any) bool {
	conditions, ok := value.(bson.A)
	if !ok {
		return false
	}
	for _, cond := range conditions {
		condMap, ok := cond.(bson.M)
		if !ok {
			continue
		}
		if matchesCondition(doc, condMap) {
			return true
		}
	}
	return false
}

func matchesCondition(doc *mongoEnvironment, cond bson.M) bool {
	for key, value := range cond {
		switch key {
		case "status.desired":
			if doc.Status == nil || doc.Status.Desired != toInt(value) {
				return false
			}
		case "status.state":
			if doc.Status == nil || doc.Status.State != toInt(value) {
				return false
			}
		case "$expr":
			if !matchesExpr(doc, value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func matchesExpr(doc *mongoEnvironment, expr any) bool {
	exprMap, ok := expr.(bson.M)
	if !ok {
		return false
	}
	for op, args := range exprMap {
		argsArr, ok := args.(bson.A)
		if !ok || len(argsArr) != 2 {
			return false
		}
		left := resolveFieldPath(doc, toString(argsArr[0]))
		right := resolveFieldPath(doc, toString(argsArr[1]))
		switch op {
		case "$lt":
			if toInt64(left) >= toInt64(right) {
				return false
			}
		case "$eq":
			if toInt64(left) != toInt64(right) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func resolveFieldPath(doc *mongoEnvironment, path string) any {
	if len(path) > 0 && path[0] == '$' {
		path = path[1:]
	}
	switch path {
	case "generation":
		return doc.Generation
	case "status.observed_generation":
		if doc.Status != nil {
			return doc.Status.ObservedGeneration
		}
		return int64(0)
	case "status.desired":
		if doc.Status != nil {
			return doc.Status.Desired
		}
		return 0
	case "status.state":
		if doc.Status != nil {
			return doc.Status.State
		}
		return 0
	default:
		return nil
	}
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	default:
		return 0
	}
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		return ""
	}
}

func (f *fakeCollectionOps) UpdateOne(_ context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	f.lastUpdateFilter = filter
	f.lastUpdateUpdate = update
	if len(opts) > 0 && opts[0] != nil {
		f.lastUpdateUpsert = opts[0].Upsert
	}
	if f.updateErr != nil {
		return nil, f.updateErr
	}

	filterDoc, err := anyToBSONMap(filter)
	if err != nil {
		return nil, err
	}
	key, _ := filterDoc["name"].(string)
	if generationFilter, ok := filterDoc[mongoFieldGeneration]; ok {
		allowed, allowedOK := extractMaxGeneration(generationFilter)
		if allowedOK {
			if existing, exists := f.docs[key]; exists && existing.Generation > allowed {
				return nil, mongodriver.WriteException{
					WriteErrors: []mongodriver.WriteError{{Code: 11000, Message: "duplicate key"}},
				}
			}
		}
	}
	updateDoc, err := anyToBSONMap(update)
	if err != nil {
		return nil, err
	}
	setDoc, err := anyToBSONMap(updateDoc["$set"])
	if err != nil {
		return nil, err
	}
	setOnInsertDoc, err := anyToBSONMap(updateDoc["$setOnInsert"])
	if err != nil {
		return nil, err
	}

	stored := new(mongoEnvironment)
	if existing, ok := f.docs[key]; ok {
		copy := *existing
		stored = &copy
	} else if setOnInsertDoc != nil {
		if err := decodeBSONDocument(setOnInsertDoc, stored); err != nil {
			return nil, err
		}
	}
	if err := decodeBSONDocument(setDoc, stored); err != nil {
		return nil, err
	}
	if f.docs == nil {
		f.docs = make(map[string]*mongoEnvironment)
	}
	if existing, ok := f.docs[key]; ok {
		stored.ID = existing.ID
	} else {
		stored.ID = primitive.NewObjectID()
	}
	matchedCount := int64(0)
	modifiedCount := int64(0)
	upsertedCount := int64(0)
	if _, ok := f.docs[key]; ok {
		matchedCount = 1
		modifiedCount = 1
	} else {
		upsertedCount = 1
	}
	stored.Name = key
	copy := *stored
	f.docs[key] = &copy

	return &mongodriver.UpdateResult{
		MatchedCount:  matchedCount,
		ModifiedCount: modifiedCount,
		UpsertedCount: upsertedCount,
		UpsertedID:    stored.ID,
	}, nil
}

func extractMaxGeneration(v any) (int64, bool) {
	generationDoc, err := anyToBSONMap(v)
	if err != nil {
		return 0, false
	}
	allowed, ok := generationDoc["$lte"]
	if !ok {
		return 0, false
	}
	return toInt64(allowed), true
}

func (f *fakeCollectionOps) DeleteOne(_ context.Context, filter any, _ ...*options.DeleteOptions) (*mongodriver.DeleteResult, error) {
	f.lastDeleteFilter = filter
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}

	filterDoc, err := anyToBSONMap(filter)
	if err != nil {
		return nil, err
	}
	key, _ := filterDoc["name"].(string)
	if f.docs == nil {
		return &mongodriver.DeleteResult{}, nil
	}
	if _, ok := f.docs[key]; !ok {
		return &mongodriver.DeleteResult{}, nil
	}
	delete(f.docs, key)

	return &mongodriver.DeleteResult{DeletedCount: 1}, nil
}

func (f *fakeCollectionOps) CountDocuments(_ context.Context, filter any, _ ...*options.CountOptions) (int64, error) {
	f.lastCountFilter = filter
	filterDoc, err := anyToBSONMap(filter)
	if err != nil {
		return 0, err
	}
	scope, _ := filterDoc["scope"].(string)

	var count int64
	for _, doc := range f.docs {
		if doc.Scope == scope {
			count++
		}
	}

	return count, nil
}

func (f *fakeCollectionOps) Indexes() indexViewOps {
	return &f.indexes
}

type fakeIndexViewOps struct {
	models []mongodriver.IndexModel
	err    error
}

func (f *fakeIndexViewOps) CreateMany(_ context.Context, models []mongodriver.IndexModel, _ ...*options.CreateIndexesOptions) ([]string, error) {
	f.models = append([]mongodriver.IndexModel(nil), models...)
	if f.err != nil {
		return nil, f.err
	}

	return []string{"idx1", "idx2"}, nil
}

type fakeSingleResult struct {
	doc any
	err error
}

func (r fakeSingleResult) Decode(v any) error {
	if r.err != nil {
		return r.err
	}

	return decodeBSONDocument(r.doc, v)
}

type fakeCursor struct {
	docs []*mongoEnvironment
}

func (c fakeCursor) All(_ context.Context, results any) error {
	typedResults, ok := results.(*[]*mongoEnvironment)
	if !ok {
		return errors.New("fakeCursor.All only supports []*mongoEnvironment")
	}
	if c.docs == nil {
		*typedResults = nil
		return nil
	}

	cloned := make([]*mongoEnvironment, 0, len(c.docs))
	for _, doc := range c.docs {
		copy := *doc
		cloned = append(cloned, &copy)
	}
	*typedResults = cloned

	return nil
}

func (fakeCursor) Close(context.Context) error {
	return nil
}

func newMongoRepositoryForTest() (*MongoRepository, *fakeCollectionOps) {
	collection := &fakeCollectionOps{
		docs: make(map[string]*mongoEnvironment),
	}

	return &MongoRepository{collection: collection}, collection
}

func newMongoSaveTestEnv(t *testing.T, scope, envName, description, image, etag string) *domain.Environment {
	t.Helper()

	name, err := domain.NewEnvironmentName(scope, envName)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}
	baseTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	env, err := domain.NewEnvironment(name, domain.EnvironmentTypeProd, description, &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{
			{
				Name:     "svc1",
				App:      "app1",
				Image:    image,
				Replicas: 2,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	rehydrated, err := domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
		Name:         name,
		EnvType:      domain.EnvironmentTypeProd,
		Description:  description,
		DesiredState: env.DesiredState(),
		Status: &domain.EnvironmentStatus{
			Desired:            domain.DesiredPresent,
			State:              domain.StateReady,
			ObservedGeneration: 1,
			Message:            "ready",
			LastReconcileTime:  baseTime.Add(2 * time.Minute),
			LastSuccessTime:    baseTime.Add(3 * time.Minute),
		},
		Generation: 1,
		CreateTime: baseTime,
		UpdateTime: baseTime.Add(5 * time.Minute),
		ETag:       etag,
	})
	if err != nil {
		t.Fatalf("RehydrateEnvironment() error = %v", err)
	}

	return rehydrated
}

func newMongoStateTestEnv(t *testing.T, scope, envName, description string, state domain.EnvironmentState, etag string) *domain.Environment {
	t.Helper()

	name, err := domain.NewEnvironmentName(scope, envName)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}
	baseTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	env, err := domain.NewEnvironment(name, domain.EnvironmentTypeProd, description, &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{
			{
				Name:     "svc1",
				App:      "app1",
				Image:    "image:v1",
				Replicas: 2,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	rehydrated, err := domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
		Name:         name,
		EnvType:      domain.EnvironmentTypeProd,
		Description:  description,
		DesiredState: env.DesiredState(),
		Status: &domain.EnvironmentStatus{
			Desired:            domain.DesiredPresent,
			State:              state,
			ObservedGeneration: 1,
			Message:            "state",
			LastReconcileTime:  baseTime.Add(2 * time.Minute),
			LastSuccessTime:    baseTime.Add(3 * time.Minute),
		},
		Generation: 1,
		CreateTime: baseTime,
		UpdateTime: baseTime.Add(5 * time.Minute),
		ETag:       etag,
	})
	if err != nil {
		t.Fatalf("RehydrateEnvironment() error = %v", err)
	}

	return rehydrated
}

func cloneEnvironmentWithGeneration(t *testing.T, env *domain.Environment, generation int64) *domain.Environment {
	t.Helper()

	rehydrated, err := domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
		Name:         env.Name(),
		EnvType:      env.Type(),
		Description:  env.Description(),
		DesiredState: env.DesiredState(),
		Status: &domain.EnvironmentStatus{
			Desired:            env.Status().Desired,
			State:              env.Status().State,
			ObservedGeneration: env.Status().ObservedGeneration,
			Message:            env.Status().Message,
			LastReconcileTime:  env.Status().LastReconcileTime,
			LastSuccessTime:    env.Status().LastSuccessTime,
		},
		Generation: generation,
		CreateTime: env.CreateTime(),
		UpdateTime: env.UpdateTime(),
		ETag:       env.ETag(),
	})
	if err != nil {
		t.Fatalf("RehydrateEnvironment() error = %v", err)
	}

	return rehydrated
}

func assertEnvironmentEqual(t *testing.T, got *domain.Environment, want *domain.Environment) {
	t.Helper()

	if got.Name().String() != want.Name().String() {
		t.Fatalf("Name() = %q, want %q", got.Name().String(), want.Name().String())
	}
	if got.Type() != want.Type() {
		t.Fatalf("Type() = %v, want %v", got.Type(), want.Type())
	}
	if got.Description() != want.Description() {
		t.Fatalf("Description() = %q, want %q", got.Description(), want.Description())
	}
	if !reflect.DeepEqual(got.DesiredState(), want.DesiredState()) {
		t.Fatalf("DesiredState() = %#v, want %#v", got.DesiredState(), want.DesiredState())
	}
	if !reflect.DeepEqual(got.Status(), want.Status()) {
		t.Fatalf("Status() = %#v, want %#v", got.Status(), want.Status())
	}
	if got.Generation() != want.Generation() {
		t.Fatalf("Generation() = %d, want %d", got.Generation(), want.Generation())
	}
	if !got.CreateTime().Equal(want.CreateTime()) {
		t.Fatalf("CreateTime() = %v, want %v", got.CreateTime(), want.CreateTime())
	}
	if !got.UpdateTime().Equal(want.UpdateTime()) {
		t.Fatalf("UpdateTime() = %v, want %v", got.UpdateTime(), want.UpdateTime())
	}
	if got.ETag() != want.ETag() {
		t.Fatalf("ETag() = %q, want %q", got.ETag(), want.ETag())
	}
}

func assertNotFoundError(t *testing.T, err error, label string) {
	t.Helper()

	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%s error = %v, want %v", label, err, domain.ErrNotFound)
	}
}

func assertEnvironmentNames(t *testing.T, envs []*domain.Environment, want []string) {
	t.Helper()

	if len(envs) != len(want) {
		t.Fatalf("environment count = %d, want %d", len(envs), len(want))
	}
	for i, env := range envs {
		if env.Name().EnvName() != want[i] {
			t.Fatalf("env[%d] = %q, want %q", i, env.Name().EnvName(), want[i])
		}
	}
}

func assertBSONMapEqual(t *testing.T, got any, want bson.M, label string) {
	t.Helper()

	gotDoc, err := anyToBSONMap(got)
	if err != nil {
		t.Fatalf("%s decode error = %v", label, err)
	}
	if !reflect.DeepEqual(gotDoc, want) {
		t.Fatalf("%s = %#v, want %#v", label, gotDoc, want)
	}
}

func assertFindOptions(t *testing.T, opts *options.FindOptions, wantSkip int64, wantLimit int64, wantSort bson.D) {
	t.Helper()

	if opts == nil {
		t.Fatal("Find() options = nil, want non-nil")
	}
	if opts.Skip == nil || *opts.Skip != wantSkip {
		t.Fatalf("Find() skip = %v, want %d", opts.Skip, wantSkip)
	}
	if opts.Limit == nil || *opts.Limit != wantLimit {
		t.Fatalf("Find() limit = %v, want %d", opts.Limit, wantLimit)
	}
	gotSort, ok := opts.Sort.(bson.D)
	if !ok {
		t.Fatalf("Find() sort type = %T, want bson.D", opts.Sort)
	}
	if !reflect.DeepEqual(gotSort, wantSort) {
		t.Fatalf("Find() sort = %#v, want %#v", gotSort, wantSort)
	}
}

func assertFindSort(t *testing.T, opts *options.FindOptions, wantSort bson.D) {
	t.Helper()

	if opts == nil {
		t.Fatal("Find() options = nil, want non-nil")
	}
	gotSort, ok := opts.Sort.(bson.D)
	if !ok {
		t.Fatalf("Find() sort type = %T, want bson.D", opts.Sort)
	}
	if !reflect.DeepEqual(gotSort, wantSort) {
		t.Fatalf("Find() sort = %#v, want %#v", gotSort, wantSort)
	}
}

func anyToBSONMap(value any) (bson.M, error) {
	if value == nil {
		return nil, nil
	}

	if doc, ok := value.(bson.M); ok {
		return doc, nil
	}

	raw, err := bson.Marshal(value)
	if err != nil {
		return nil, err
	}

	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	return doc, nil
}

func decodeBSONDocument(doc any, out any) error {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return err
	}

	return bson.Unmarshal(raw, out)
}

func assertSaveUpdateDocument(t *testing.T, got any, env *domain.Environment) {
	t.Helper()

	doc, err := mongoEnvironmentFromDomain(env)
	if err != nil {
		t.Fatalf("mongoEnvironmentFromDomain() error = %v", err)
	}
	want := bson.M{
		"$set":         doc.updateDocument(),
		"$setOnInsert": doc.insertDocument(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpdateOne() update = %#v, want %#v", got, want)
	}
}
