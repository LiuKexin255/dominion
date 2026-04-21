package deploy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"dominion/projects/infra/deploy/domain"
)

type fakeRepository struct {
	mu        sync.RWMutex
	envs      map[string]*domain.Environment
	getErr    error
	listErr   error
	saveErr   error
	deleteErr error
}

func newFakeRepository(seed ...*domain.Environment) *fakeRepository {
	r := &fakeRepository{envs: make(map[string]*domain.Environment, len(seed))}
	for _, env := range seed {
		if env == nil {
			continue
		}
		r.envs[env.Name().String()] = env
	}
	return r
}

func (r *fakeRepository) Get(_ context.Context, name domain.EnvironmentName) (*domain.Environment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	env, ok := r.envs[name.String()]
	if !ok {
		return nil, domain.ErrNotFound
	}

	return env, nil
}

func (r *fakeRepository) ListByStates(_ context.Context, states ...domain.EnvironmentState) ([]*domain.Environment, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}

	allowedStates := make(map[domain.EnvironmentState]struct{}, len(states))
	for _, state := range states {
		allowedStates[state] = struct{}{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	filtered := make([]*domain.Environment, 0, len(r.envs))
	for _, env := range r.envs {
		status := env.Status()
		if status == nil {
			continue
		}
		if _, ok := allowedStates[status.State]; ok {
			filtered = append(filtered, env)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name().String() < filtered[j].Name().String()
	})

	return filtered, nil
}

func (r *fakeRepository) ListNeedingReconcile(_ context.Context) ([]*domain.Environment, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	filtered := make([]*domain.Environment, 0, len(r.envs))
	for _, env := range r.envs {
		status := env.Status()
		if status == nil {
			continue
		}

		if needsReconcile(env) {
			filtered = append(filtered, env)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name().String() < filtered[j].Name().String()
	})

	return filtered, nil
}

func (r *fakeRepository) ListByScope(_ context.Context, scope string, pageSize int32, pageToken string) ([]*domain.Environment, string, error) {
	if r.listErr != nil {
		return nil, "", r.listErr
	}

	if pageSize <= 0 {
		pageSize = 100
	}

	skip, err := domain.DecodePageToken(pageToken)
	if err != nil {
		return nil, "", fmt.Errorf("invalid page token: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	prefix := scopePrefix(scope)
	filtered := make([]*domain.Environment, 0, len(r.envs))
	for _, env := range r.envs {
		if strings.HasPrefix(env.Name().String(), prefix) {
			filtered = append(filtered, env)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name().String() < filtered[j].Name().String()
	})

	if skip >= len(filtered) {
		return nil, "", nil
	}

	end := skip + int(pageSize)
	if end > len(filtered) {
		end = len(filtered)
	}

	var nextToken string
	if end < len(filtered) {
		nextToken = domain.EncodePageToken(end)
	}

	return filtered[skip:end], nextToken, nil
}

func (r *fakeRepository) Save(_ context.Context, env *domain.Environment) error {
	if r.saveErr != nil {
		return r.saveErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.envs[env.Name().String()] = env
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, name domain.EnvironmentName) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.envs[name.String()]; !ok {
		return domain.ErrNotFound
	}

	delete(r.envs, name.String())
	return nil
}

func scopePrefix(scope string) string {
	return fmt.Sprintf("deploy/scopes/%s/environments/", scope)
}

func needsReconcile(env *domain.Environment) bool {
	status := env.Status()
	if status == nil {
		return false
	}

	switch {
	case status.Desired == domain.DesiredPresent && status.ObservedGeneration < env.Generation():
		return true
	case status.Desired == domain.DesiredPresent && status.State == domain.StateFailed && status.ObservedGeneration == env.Generation():
		return true
	case status.Desired == domain.DesiredAbsent:
		return true
	default:
		return false
	}
}

func TestFakeRepository_ListNeedingReconcile(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		given func(*testing.T) *fakeRepository
		want  []string
	}{
		{
			name: "returns environments needing reconcile",
			given: func(t *testing.T) *fakeRepository {
				env1 := mustFakeEnvironment(t, "scope1", "env1")
				env2 := mustFakeEnvironment(t, "scope1", "env2")
				if err := env2.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env2.MarkReady(env2.Generation()); err != nil {
					t.Fatalf("MarkReady() unexpected error: %v", err)
				}
				env3 := mustFakeEnvironment(t, "scope1", "env3")
				if err := env3.SetDesiredAbsent(); err != nil {
					t.Fatalf("SetDesiredAbsent() unexpected error: %v", err)
				}
				env4 := mustFakeEnvironment(t, "scope1", "env4")
				if err := env4.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env4.MarkFailed(env4.Generation(), "boom"); err != nil {
					t.Fatalf("MarkFailed() unexpected error: %v", err)
				}

				return newFakeRepository(env1, env2, env3, env4)
			},
			want: []string{"env1", "env3", "env4"},
		},
		{
			name: "returns empty when all environments are converged",
			given: func(t *testing.T) *fakeRepository {
				env1 := mustFakeEnvironment(t, "scope1", "env1")
				if err := env1.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env1.MarkReady(env1.Generation()); err != nil {
					t.Fatalf("MarkReady() unexpected error: %v", err)
				}

				env2 := mustFakeEnvironment(t, "scope1", "env2")
				if err := env2.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env2.MarkReady(env2.Generation()); err != nil {
					t.Fatalf("MarkReady() unexpected error: %v", err)
				}

				return newFakeRepository(env1, env2)
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := tt.given(t)

			// when
			got, err := repo.ListNeedingReconcile(ctx)

			// then
			if err != nil {
				t.Fatalf("ListNeedingReconcile() unexpected error: %v", err)
			}

			if gotNames := envNames(got); !equalStrings(gotNames, tt.want) {
				t.Fatalf("ListNeedingReconcile() names = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func mustFakeEnvironment(t *testing.T, scope, name string) *domain.Environment {
	t.Helper()

	envName, err := domain.NewEnvironmentName(scope, name)
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	env, err := domain.NewEnvironment(envName, domain.EnvironmentTypeProd, "", &domain.DesiredState{})
	if err != nil {
		t.Fatalf("NewEnvironment() unexpected error: %v", err)
	}

	return env
}

func envNames(envs []*domain.Environment) []string {
	if len(envs) == 0 {
		return nil
	}

	names := make([]string, 0, len(envs))
	for _, env := range envs {
		names = append(names, env.Name().EnvName())
	}

	return names
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
