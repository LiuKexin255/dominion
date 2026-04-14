package deploy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

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
