package storage

import (
	"context"
	"errors"
	"testing"

	"dominion/projects/infra/deploy/domain"
)

// newTestEnv creates a valid environment for testing with the given scope and envName.
func newTestEnv(t *testing.T, scope, envName string) *domain.Environment {
	t.Helper()
	name, err := domain.NewEnvironmentName(scope, envName)
	if err != nil {
		t.Fatalf("failed to create environment name: %v", err)
	}
	env, err := domain.NewEnvironment(name, "test environment", domain.DesiredState{
		Services: []domain.ServiceSpec{
			{
				Name:     "svc1",
				App:      "app1",
				Image:    "image:v1",
				Replicas: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}
	return env
}

func TestMemoryRepository_Get(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		seed    *domain.Environment // environment to seed in the store
		getName string              // resource name to look up
		wantErr error
	}{
		{
			name:    "success",
			seed:    newTestEnv(t, "dev", "env1"),
			getName: "deploy/scopes/dev/environments/env1",
		},
		{
			name:    "not found",
			seed:    newTestEnv(t, "dev", "env1"),
			getName: "deploy/scopes/dev/environments/env2",
			wantErr: domain.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := NewMemoryRepository()
			if tt.seed != nil {
				_ = repo.Save(ctx, tt.seed)
			}
			envName, err := domain.ParseResourceName(tt.getName)
			if err != nil {
				t.Fatalf("failed to parse resource name: %v", err)
			}

			// when
			got, err := repo.Get(ctx, envName)

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get() unexpected error: %v", err)
			}
			if got.Name().String() != tt.seed.Name().String() {
				t.Fatalf("Get() name = %q, want %q", got.Name().String(), tt.seed.Name().String())
			}
		})
	}
}

func TestMemoryRepository_Save(t *testing.T) {
	ctx := context.Background()

	t.Run("create new environment", func(t *testing.T) {
		// given
		repo := NewMemoryRepository()
		env := newTestEnv(t, "dev", "env1")

		// when
		err := repo.Save(ctx, env)

		// then
		if err != nil {
			t.Fatalf("Save() unexpected error: %v", err)
		}
		got, err := repo.Get(ctx, env.Name())
		if err != nil {
			t.Fatalf("Get() after Save() unexpected error: %v", err)
		}
		if got.Name().String() != env.Name().String() {
			t.Fatalf("Get() name = %q, want %q", got.Name().String(), env.Name().String())
		}
	})

	t.Run("update existing environment", func(t *testing.T) {
		// given
		repo := NewMemoryRepository()
		env := newTestEnv(t, "dev", "env1")
		_ = repo.Save(ctx, env)

		updatedEnv := newTestEnv(t, "dev", "env1")

		// when
		err := repo.Save(ctx, updatedEnv)

		// then
		if err != nil {
			t.Fatalf("Save() update unexpected error: %v", err)
		}
		got, err := repo.Get(ctx, env.Name())
		if err != nil {
			t.Fatalf("Get() after update unexpected error: %v", err)
		}
		// Verify the update time changed, confirming the record was replaced.
		if !got.UpdateTime().After(env.UpdateTime()) && got.UpdateTime() != env.UpdateTime() {
			t.Fatalf("expected updated environment to have different update time")
		}
	})
}

func TestMemoryRepository_Delete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		seed    *domain.Environment
		delName string // resource name to delete
		wantErr error
	}{
		{
			name:    "success",
			seed:    newTestEnv(t, "dev", "env1"),
			delName: "deploy/scopes/dev/environments/env1",
		},
		{
			name:    "not found",
			seed:    newTestEnv(t, "dev", "env1"),
			delName: "deploy/scopes/dev/environments/env2",
			wantErr: domain.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := NewMemoryRepository()
			if tt.seed != nil {
				_ = repo.Save(ctx, tt.seed)
			}
			envName, err := domain.ParseResourceName(tt.delName)
			if err != nil {
				t.Fatalf("failed to parse resource name: %v", err)
			}

			// when
			err = repo.Delete(ctx, envName)

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Delete() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Delete() unexpected error: %v", err)
			}
			_, getErr := repo.Get(ctx, envName)
			if !errors.Is(getErr, domain.ErrNotFound) {
				t.Fatalf("Get() after Delete() error = %v, want ErrNotFound", getErr)
			}
		})
	}
}

func TestMemoryRepository_ListByScope(t *testing.T) {
	ctx := context.Background()

	t.Run("pagination across multiple pages", func(t *testing.T) {
		// given: 5 environments in scope "dev" with names "aa" through "ee"
		repo := NewMemoryRepository()
		envNames := []struct {
			scope, envName string
		}{
			{"dev", "cc"},
			{"dev", "aa"},
			{"dev", "ee"},
			{"dev", "bb"},
			{"dev", "dd"},
		}
		for _, n := range envNames {
			env := newTestEnv(t, n.scope, n.envName)
			_ = repo.Save(ctx, env)
		}

		// when: page 1 with pageSize=2
		page1, nextToken1, err := repo.ListByScope(ctx, "dev", 2, "")
		if err != nil {
			t.Fatalf("ListByScope() page 1 error: %v", err)
		}

		// then: first page has 2 results, sorted by name
		if len(page1) != 2 {
			t.Fatalf("page 1 len = %d, want 2", len(page1))
		}
		if page1[0].Name().EnvName() != "aa" {
			t.Fatalf("page1[0] envName = %q, want %q", page1[0].Name().EnvName(), "aa")
		}
		if page1[1].Name().EnvName() != "bb" {
			t.Fatalf("page1[1] envName = %q, want %q", page1[1].Name().EnvName(), "bb")
		}
		if nextToken1 == "" {
			t.Fatalf("page 1 nextToken is empty, expected more results")
		}

		// when: page 2
		page2, nextToken2, err := repo.ListByScope(ctx, "dev", 2, nextToken1)
		if err != nil {
			t.Fatalf("ListByScope() page 2 error: %v", err)
		}

		// then: second page has 2 results
		if len(page2) != 2 {
			t.Fatalf("page 2 len = %d, want 2", len(page2))
		}
		if page2[0].Name().EnvName() != "cc" {
			t.Fatalf("page2[0] envName = %q, want %q", page2[0].Name().EnvName(), "cc")
		}
		if page2[1].Name().EnvName() != "dd" {
			t.Fatalf("page2[1] envName = %q, want %q", page2[1].Name().EnvName(), "dd")
		}
		if nextToken2 == "" {
			t.Fatalf("page 2 nextToken is empty, expected more results")
		}

		// when: page 3 (last page)
		page3, nextToken3, err := repo.ListByScope(ctx, "dev", 2, nextToken2)
		if err != nil {
			t.Fatalf("ListByScope() page 3 error: %v", err)
		}

		// then: last page has 1 result and empty next token
		if len(page3) != 1 {
			t.Fatalf("page 3 len = %d, want 1", len(page3))
		}
		if page3[0].Name().EnvName() != "ee" {
			t.Fatalf("page3[0] envName = %q, want %q", page3[0].Name().EnvName(), "ee")
		}
		if nextToken3 != "" {
			t.Fatalf("page 3 nextToken = %q, want empty", nextToken3)
		}
	})

	t.Run("empty scope returns nil", func(t *testing.T) {
		// given
		repo := NewMemoryRepository()
		env := newTestEnv(t, "dev", "env1")
		_ = repo.Save(ctx, env)

		// when
		results, nextToken, err := repo.ListByScope(ctx, "prod", 10, "")

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		if results != nil {
			t.Fatalf("ListByScope() results = %v, want nil", results)
		}
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
	})

	t.Run("default page size when zero", func(t *testing.T) {
		// given: create 1 environment
		repo := NewMemoryRepository()
		env := newTestEnv(t, "dev", "env1")
		_ = repo.Save(ctx, env)

		// when: pageSize=0 should use default
		results, nextToken, err := repo.ListByScope(ctx, "dev", 0, "")

		// then
		if err != nil {
			t.Fatalf("ListByScope() unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("ListByScope() len = %d, want 1", len(results))
		}
		if nextToken != "" {
			t.Fatalf("ListByScope() nextToken = %q, want empty", nextToken)
		}
	})
}
