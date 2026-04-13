// Package storage provides in-memory repository implementations for the deploy service.
package storage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"dominion/projects/infra/deploy/domain"
)

const (
	// defaultPageSize is the default number of environments returned by ListByScope.
	defaultPageSize = 100
)

// MemoryRepository is an in-memory implementation of domain.Repository.
type MemoryRepository struct {
	mu   sync.RWMutex
	envs map[string]*domain.Environment // key = EnvironmentName.String()
}

// NewMemoryRepository creates a new MemoryRepository ready for use.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		envs: make(map[string]*domain.Environment),
	}
}

// Get retrieves an environment by name.
// Returns domain.ErrNotFound if the environment does not exist.
func (r *MemoryRepository) Get(_ context.Context, name domain.EnvironmentName) (*domain.Environment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	env, exists := r.envs[name.String()]
	if !exists {
		return nil, domain.ErrNotFound
	}

	return env, nil
}

// ListByScope lists environments under a scope with pagination.
// Results are sorted by name for stable ordering.
func (r *MemoryRepository) ListByScope(_ context.Context, scope string, pageSize int32, pageToken string) ([]*domain.Environment, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Normalize page size.
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	// Decode skip offset from page token.
	skip, err := domain.DecodePageToken(pageToken)
	if err != nil {
		return nil, "", fmt.Errorf("invalid page token: %w", err)
	}

	// Filter environments by scope prefix.
	prefix := scopePrefix(scope)
	var filtered []*domain.Environment
	for _, env := range r.envs {
		if strings.HasPrefix(env.Name().String(), prefix) {
			filtered = append(filtered, env)
		}
	}

	// Sort by name for stable ordering.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name().String() < filtered[j].Name().String()
	})

	// Apply pagination.
	total := len(filtered)
	if skip >= total {
		return nil, "", nil
	}

	end := skip + int(pageSize)
	if end > total {
		end = total
	}

	page := filtered[skip:end]

	// Determine next page token.
	var nextToken string
	if end < total {
		nextToken = domain.EncodePageToken(end)
	}

	return page, nextToken, nil
}

// Save persists an environment.
// For new environments (env.Name() does not exist), it creates the record.
// For existing environments, it updates the record in place.
// Returns domain.ErrAlreadyExists if attempting to create an environment that already exists.
func (r *MemoryRepository) Save(_ context.Context, env *domain.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := env.Name().String()
	_, exists := r.envs[key]

	// Create: must not already exist.
	if !exists {
		r.envs[key] = env
		return nil
	}

	// Update: replace existing record.
	r.envs[key] = env
	return nil
}

// Delete removes an environment by name.
// Returns domain.ErrNotFound if the environment does not exist.
func (r *MemoryRepository) Delete(_ context.Context, name domain.EnvironmentName) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.envs[name.String()]; !exists {
		return domain.ErrNotFound
	}

	delete(r.envs, name.String())
	return nil
}

// scopePrefix returns the key prefix for a given scope.
func scopePrefix(scope string) string {
	return fmt.Sprintf("deploy/scopes/%s/environments/", scope)
}
