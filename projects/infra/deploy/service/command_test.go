package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"dominion/projects/infra/deploy/domain"
)

// ---------------------------------------------------------------------------
// Fake implementations for command tests
// ---------------------------------------------------------------------------

// fakeCommandRepository is an in-memory domain.Repository for command tests.
type fakeCommandRepository struct {
	mu   sync.Mutex
	envs map[string]*domain.Environment

	getErr          error
	createFn        func(env *domain.Environment) error
	updateDesiredFn func(name domain.EnvironmentName, expectedGeneration int64, desiredState *domain.DesiredState, desired domain.EnvironmentDesired) error
}

func newFakeCommandRepository(seed ...*domain.Environment) *fakeCommandRepository {
	r := &fakeCommandRepository{envs: make(map[string]*domain.Environment, len(seed))}
	for _, env := range seed {
		r.envs[env.Name().String()] = cloneCommandEnv(env)
	}
	return r
}

func (r *fakeCommandRepository) Get(_ context.Context, name domain.EnvironmentName) (*domain.Environment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envs[name.String()]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneCommandEnv(env), nil
}

func (r *fakeCommandRepository) Create(_ context.Context, env *domain.Environment) error {
	if r.createFn != nil {
		return r.createFn(env)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.envs[env.Name().String()] = cloneCommandEnv(env)
	return nil
}

func (r *fakeCommandRepository) UpdateDesired(_ context.Context, name domain.EnvironmentName, expectedGeneration int64, desiredState *domain.DesiredState, desired domain.EnvironmentDesired) error {
	if r.updateDesiredFn != nil {
		return r.updateDesiredFn(name, expectedGeneration, desiredState, desired)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envs[name.String()]
	if !ok {
		return domain.ErrNotFound
	}
	effectiveDesiredState := desiredState
	if effectiveDesiredState == nil {
		effectiveDesiredState = env.DesiredState()
	}
	updated, err := domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
		Name:         env.Name(),
		EnvType:      env.Type(),
		Description:  env.Description(),
		DesiredState: effectiveDesiredState,
		Status: &domain.EnvironmentStatus{
			Desired:            desired,
			State:              domain.StatePending,
			ObservedGeneration: env.Status().ObservedGeneration,
			Message:            "",
			LastReconcileTime:  env.Status().LastReconcileTime,
			LastSuccessTime:    env.Status().LastSuccessTime,
		},
		Generation: env.Generation() + 1,
		CreateTime: env.CreateTime(),
		UpdateTime: env.UpdateTime(),
		ETag:       env.ETag(),
	})
	if err != nil {
		return err
	}
	r.envs[name.String()] = updated
	return nil
}

func (r *fakeCommandRepository) Delete(_ context.Context, _ domain.EnvironmentName) error {
	return nil
}

func (r *fakeCommandRepository) ListByStates(_ context.Context, _ ...domain.EnvironmentState) ([]*domain.Environment, error) {
	return nil, nil
}

func (r *fakeCommandRepository) ListNeedingReconcile(_ context.Context) ([]*domain.Environment, error) {
	return nil, nil
}

func (r *fakeCommandRepository) ListByScope(_ context.Context, _ string, _ int32, _ string) ([]*domain.Environment, string, error) {
	return nil, "", nil
}

func (r *fakeCommandRepository) TransitionStatus(_ context.Context, _ domain.EnvironmentName, _ int64, _ domain.EnvironmentState, _ *domain.EnvironmentStatus) error {
	return nil
}

// fakeCommandEnqueuer records enqueue calls.
type fakeCommandEnqueuer struct {
	mu         sync.Mutex
	calls      []domain.EnvironmentName
	enqueueErr error
}

func (q *fakeCommandEnqueuer) Enqueue(_ context.Context, envName domain.EnvironmentName) error {
	if q.enqueueErr != nil {
		return q.enqueueErr
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, envName)
	return nil
}

func (q *fakeCommandEnqueuer) enqueueCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.calls)
}

// fakeCommandRuntime stubs domain.EnvironmentRuntime for command tests.
type fakeCommandRuntime struct {
	reservedEnvVars []string
	reservedErr     error
}

func (f *fakeCommandRuntime) ApplyResources(_ context.Context, _ *domain.Environment) error {
	return nil
}

func (f *fakeCommandRuntime) CheckRollout(_ context.Context, _ *domain.Environment) (*domain.RolloutStatus, error) {
	return nil, nil
}

func (f *fakeCommandRuntime) Delete(_ context.Context, _ domain.EnvironmentName) error {
	return nil
}

func (f *fakeCommandRuntime) QueryServiceEndpoints(_ context.Context, _, _, _ string) (*domain.ServiceQueryResult, error) {
	return nil, nil
}

func (f *fakeCommandRuntime) QueryStatefulServiceEndpoints(_ context.Context, _, _, _ string) (*domain.ServiceQueryResult, error) {
	return nil, nil
}

func (f *fakeCommandRuntime) ReservedEnvironmentVariableNames(_ context.Context) ([]string, error) {
	return f.reservedEnvVars, f.reservedErr
}

func cloneCommandEnv(env *domain.Environment) *domain.Environment {
	cloned, err := domain.RehydrateEnvironment(domain.EnvironmentSnapshot{
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
		Generation: env.Generation(),
		CreateTime: env.CreateTime(),
		UpdateTime: env.UpdateTime(),
		ETag:       env.ETag(),
	})
	if err != nil {
		panic("cloneCommandEnv: " + err.Error())
	}
	return cloned
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func mustCommandEnvName(t *testing.T, scope, name string) domain.EnvironmentName {
	t.Helper()
	// scope and name must match ^[a-z][a-z0-9]{0,7}$
	n, err := domain.NewEnvironmentName(scope, name)
	if err != nil {
		t.Fatalf("NewEnvironmentName(%q, %q) error = %v", scope, name, err)
	}
	return n
}

func validDesiredState() *domain.DesiredState {
	return &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:     "api",
			App:      "gateway",
			Image:    "example.com/gateway:v1",
			Replicas: 1,
		}},
	}
}

func validDesiredStateWithEnv(env map[string]string) *domain.DesiredState {
	return &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:     "api",
			App:      "gateway",
			Image:    "example.com/gateway:v1",
			Replicas: 1,
			Env:      env,
		}},
	}
}

func newTestCommandService(repo *fakeCommandRepository, queue *fakeCommandEnqueuer, runtime *fakeCommandRuntime) *EnvironmentCommandService {
	return NewEnvironmentCommandService(repo, queue, runtime)
}

// ---------------------------------------------------------------------------
// Create tests
// ---------------------------------------------------------------------------

func TestEnvironmentCommandService_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		repo := newFakeCommandRepository()
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		env, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", validDesiredState())
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if env.Name() != envName {
			t.Fatalf("env.Name() = %q, want %q", env.Name(), envName)
		}
		if env.Type() != domain.EnvironmentTypeProd {
			t.Fatalf("env.Type() = %v, want %v", env.Type(), domain.EnvironmentTypeProd)
		}
		if env.Generation() != 1 {
			t.Fatalf("env.Generation() = %d, want 1", env.Generation())
		}
		if queue.enqueueCount() != 1 {
			t.Fatalf("enqueue count = %d, want 1", queue.enqueueCount())
		}
		got, err := repo.Get(context.Background(), envName)
		if err != nil {
			t.Fatalf("repo.Get() error = %v", err)
		}
		if got.Name() != envName {
			t.Fatalf("persisted env name = %q, want %q", got.Name(), envName)
		}
	})

	t.Run("already exists", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", validDesiredState())
		if !errors.Is(err, domain.ErrAlreadyExists) {
			t.Fatalf("Create() error = %v, want ErrAlreadyExists", err)
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0", queue.enqueueCount())
		}
	})

	t.Run("reserved env var conflict", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		repo := newFakeCommandRepository()
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{
			reservedEnvVars: []string{"RESERVED_KEY"},
		}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", validDesiredStateWithEnv(map[string]string{"RESERVED_KEY": "value"}))
		if !errors.Is(err, domain.ErrInvalidSpec) {
			t.Fatalf("Create() error = %v, want ErrInvalidSpec", err)
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0", queue.enqueueCount())
		}
	})

	t.Run("invalid spec nil desired state", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		repo := newFakeCommandRepository()
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", nil)
		if !errors.Is(err, domain.ErrInvalidSpec) {
			t.Fatalf("Create() error = %v, want ErrInvalidSpec", err)
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0", queue.enqueueCount())
		}
	})

	t.Run("repo get error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		repo := newFakeCommandRepository()
		repo.getErr = errors.New("db connection lost")
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", validDesiredState())
		if err == nil {
			t.Fatalf("Create() expected error, got nil")
		}
		if !errors.Is(err, repo.getErr) {
			t.Fatalf("Create() error = %v, want wrapped db error", err)
		}
	})

	t.Run("enqueue error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		repo := newFakeCommandRepository()
		queue := &fakeCommandEnqueuer{enqueueErr: errors.New("queue full")}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", validDesiredState())
		if err == nil {
			t.Fatalf("Create() expected error, got nil")
		}
	})

	t.Run("create error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		repo := newFakeCommandRepository()
		repo.createFn = func(_ *domain.Environment) error {
			return errors.New("write error")
		}
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Create(context.Background(), envName, domain.EnvironmentTypeProd, "desc", validDesiredState())
		if err == nil {
			t.Fatalf("Create() expected error, got nil")
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0 on create error", queue.enqueueCount())
		}
	})
}

// ---------------------------------------------------------------------------
// Update tests
// ---------------------------------------------------------------------------

func TestEnvironmentCommandService_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		initialGen := existingEnv.Generation()
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		newState := validDesiredState()
		newState.Artifacts[0].Image = "example.com/gateway:v2"

		env, err := svc.Update(context.Background(), envName, newState)
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if env.Generation() == initialGen {
			t.Fatalf("generation should have incremented from %d", initialGen)
		}
		if env.Status().State != domain.StatePending {
			t.Fatalf("env.State() = %v, want StatePending", env.Status().State)
		}
		if env.Status().Desired != domain.DesiredPresent {
			t.Fatalf("env.Desired() = %v, want DesiredPresent", env.Status().Desired)
		}
		if queue.enqueueCount() != 1 {
			t.Fatalf("enqueue count = %d, want 1", queue.enqueueCount())
		}
	})

	t.Run("deleting state rejection", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustAbsentDeletingServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Update(context.Background(), envName, validDesiredState())
		if !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("Update() error = %v, want ErrInvalidState", err)
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0", queue.enqueueCount())
		}
	})

	t.Run("env not found", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "nonexist")
		repo := newFakeCommandRepository()
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Update(context.Background(), envName, validDesiredState())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid spec", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		invalidState := &domain.DesiredState{
			Artifacts: []*domain.ArtifactSpec{{
				Name:     "",
				App:      "gateway",
				Image:    "example.com/gateway:v1",
				Replicas: 1,
			}},
		}

		_, err := svc.Update(context.Background(), envName, invalidState)
		if !errors.Is(err, domain.ErrInvalidSpec) {
			t.Fatalf("Update() error = %v, want ErrInvalidSpec", err)
		}
	})

	t.Run("reserved env var conflict", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{
			reservedEnvVars: []string{"RESERVED_KEY"},
		}
		svc := newTestCommandService(repo, queue, runtime)

		newState := validDesiredStateWithEnv(map[string]string{"RESERVED_KEY": "value"})

		_, err := svc.Update(context.Background(), envName, newState)
		if !errors.Is(err, domain.ErrInvalidSpec) {
			t.Fatalf("Update() error = %v, want ErrInvalidSpec", err)
		}
	})

	t.Run("reset from WaitingRollout to Pending", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustWaitingRolloutServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		newState := validDesiredState()
		newState.Artifacts[0].Image = "example.com/gateway:v2"

		env, err := svc.Update(context.Background(), envName, newState)
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if env.Status().State != domain.StatePending {
			t.Fatalf("env.State() = %v, want StatePending after update from WaitingRollout", env.Status().State)
		}
		if env.Status().Desired != domain.DesiredPresent {
			t.Fatalf("env.Desired() = %v, want DesiredPresent", env.Status().Desired)
		}
		if env.Status().Message != "" {
			t.Fatalf("env.Message() = %q, want empty", env.Status().Message)
		}
		if queue.enqueueCount() != 1 {
			t.Fatalf("enqueue count = %d, want 1", queue.enqueueCount())
		}
	})

	t.Run("enqueue error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{enqueueErr: errors.New("queue full")}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Update(context.Background(), envName, validDesiredState())
		if err == nil {
			t.Fatalf("Update() expected error, got nil")
		}
	})

	t.Run("update desired error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		repo.updateDesiredFn = func(_ domain.EnvironmentName, _ int64, _ *domain.DesiredState, _ domain.EnvironmentDesired) error {
			return errors.New("write error")
		}
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		_, err := svc.Update(context.Background(), envName, validDesiredState())
		if err == nil {
			t.Fatalf("Update() expected error, got nil")
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0 on update desired error", queue.enqueueCount())
		}
	})
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestEnvironmentCommandService_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		err := svc.Delete(context.Background(), envName)
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if queue.enqueueCount() != 1 {
			t.Fatalf("enqueue count = %d, want 1", queue.enqueueCount())
		}
		got, err := repo.Get(context.Background(), envName)
		if err != nil {
			t.Fatalf("repo.Get() error = %v", err)
		}
		if got.Status().Desired != domain.DesiredAbsent {
			t.Fatalf("saved env.Desired() = %v, want DesiredAbsent", got.Status().Desired)
		}
		if got.Status().State != domain.StatePending {
			t.Fatalf("saved env.State() = %v, want StatePending", got.Status().State)
		}
	})

	t.Run("env not found", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "nonexist")
		repo := newFakeCommandRepository()
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		err := svc.Delete(context.Background(), envName)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Delete() error = %v, want ErrNotFound", err)
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0", queue.enqueueCount())
		}
	})

	t.Run("enqueue error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		queue := &fakeCommandEnqueuer{enqueueErr: errors.New("queue full")}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		err := svc.Delete(context.Background(), envName)
		if err == nil {
			t.Fatalf("Delete() expected error, got nil")
		}
	})

	t.Run("update desired error", func(t *testing.T) {
		envName := mustCommandEnvName(t, "test", "env1")
		existingEnv := mustNewServiceEnvironment(t, "test", "env1")
		repo := newFakeCommandRepository(existingEnv)
		repo.updateDesiredFn = func(_ domain.EnvironmentName, _ int64, _ *domain.DesiredState, _ domain.EnvironmentDesired) error {
			return errors.New("write error")
		}
		queue := &fakeCommandEnqueuer{}
		runtime := &fakeCommandRuntime{}
		svc := newTestCommandService(repo, queue, runtime)

		err := svc.Delete(context.Background(), envName)
		if err == nil {
			t.Fatalf("Delete() expected error, got nil")
		}
		if queue.enqueueCount() != 0 {
			t.Fatalf("enqueue count = %d, want 0 on update desired error", queue.enqueueCount())
		}
	})
}
