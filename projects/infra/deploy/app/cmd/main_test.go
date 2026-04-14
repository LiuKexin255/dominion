package main

import (
	"context"
	"errors"
	"testing"

	"dominion/pkg/mongo"
	"dominion/projects/infra/deploy/domain"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

func TestNewRepository_UsesDeployMongoTarget(t *testing.T) {
	wantRepo := &stubRepository{}
	client := new(mongodriver.Client)
	var gotTarget string

	got, err := newRepository(
		func(target string, _ ...mongo.ClientOption) (*mongodriver.Client, error) {
			gotTarget = target
			return client, nil
		},
		func(gotClient *mongodriver.Client) (domain.Repository, error) {
			if gotClient != client {
				t.Fatalf("newRepository() client = %p, want %p", gotClient, client)
			}
			return wantRepo, nil
		},
	)
	if err != nil {
		t.Fatalf("newRepository() error = %v", err)
	}
	if gotTarget != deployMongoTarget {
		t.Fatalf("newRepository() target = %q, want %q", gotTarget, deployMongoTarget)
	}
	if got != domain.Repository(wantRepo) {
		t.Fatalf("newRepository() repository = %T, want %T", got, wantRepo)
	}
}

func TestNewRepository_FailsFastWhenMongoClientErrors(t *testing.T) {
	wantErr := errors.New("mongo unavailable")

	_, err := newRepository(
		func(string, ...mongo.ClientOption) (*mongodriver.Client, error) {
			return nil, wantErr
		},
		func(*mongodriver.Client) (domain.Repository, error) {
			t.Fatal("newRepository() should not construct a repository when client creation fails")
			return nil, nil
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("newRepository() error = %v, want %v", err, wantErr)
	}
}

type stubRepository struct{}

func (r *stubRepository) Get(context.Context, domain.EnvironmentName) (*domain.Environment, error) {
	return nil, nil
}

func (r *stubRepository) ListByStates(context.Context, ...domain.EnvironmentState) ([]*domain.Environment, error) {
	return nil, nil
}

func (r *stubRepository) ListByScope(context.Context, string, int32, string) ([]*domain.Environment, string, error) {
	return nil, "", nil
}

func (r *stubRepository) Save(context.Context, *domain.Environment) error {
	return nil
}

func (r *stubRepository) Delete(context.Context, domain.EnvironmentName) error {
	return nil
}
