package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"dominion/projects/infra/deploy/domain"
)

// ---------------------------------------------------------------------------
// Test helpers – environment constructors
// ---------------------------------------------------------------------------

func mustServiceEnvName(t *testing.T, scope, env string) domain.EnvironmentName {
	t.Helper()
	name, err := domain.NewEnvironmentName(scope, env)
	if err != nil {
		t.Fatalf("NewEnvironmentName(%q, %q) error = %v", scope, env, err)
	}
	return name
}

func mustNewServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	name := mustServiceEnvName(t, scope, env)
	environment, err := domain.NewEnvironment(name, domain.EnvironmentTypeProd, env, &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:     "api",
			App:      "gateway",
			Image:    "example.com/gateway:v1",
			Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas: 1,
		}},
	})
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}
	return environment
}

func mustReconcilingServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	environment := mustNewServiceEnvironment(t, scope, env)
	if err := environment.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() error = %v", err)
	}
	return environment
}

func mustWaitingRolloutServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	environment := mustReconcilingServiceEnvironment(t, scope, env)
	if err := environment.MarkWaitingRollout(environment.Generation()); err != nil {
		t.Fatalf("MarkWaitingRollout() error = %v", err)
	}
	return environment
}

func mustReadyServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	environment := mustReconcilingServiceEnvironment(t, scope, env)
	if err := environment.MarkReady(environment.Generation()); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	return environment
}

func mustFailedServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	environment := mustReconcilingServiceEnvironment(t, scope, env)
	if err := environment.MarkFailed(environment.Generation(), "apply failed"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	return environment
}

func mustAbsentPendingServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	environment := mustNewServiceEnvironment(t, scope, env)
	if err := environment.SetDesiredAbsent(); err != nil {
		t.Fatalf("SetDesiredAbsent() error = %v", err)
	}
	return environment
}

func mustAbsentDeletingServiceEnvironment(t *testing.T, scope, env string) *domain.Environment {
	t.Helper()
	environment := mustAbsentPendingServiceEnvironment(t, scope, env)
	if err := environment.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() error = %v", err)
	}
	return environment
}

func mustEnvironmentWithGeneration(t *testing.T, env *domain.Environment, generation int64) *domain.Environment {
	t.Helper()
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
		Generation: generation,
		CreateTime: env.CreateTime(),
		UpdateTime: env.UpdateTime(),
		ETag:       env.ETag(),
	})
	if err != nil {
		t.Fatalf("RehydrateEnvironment() error = %v", err)
	}
	return cloned
}

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakeReconcileRepository struct {
	mu          sync.Mutex
	envs        map[string]*domain.Environment
	transitions []transitionRecord

	getErr       error
	deleteErr    error
	transitionFn func(name domain.EnvironmentName, gen int64, fromState domain.EnvironmentState, toStatus *domain.EnvironmentStatus) error
}

type transitionRecord struct {
	name      domain.EnvironmentName
	gen       int64
	fromState domain.EnvironmentState
	toState   domain.EnvironmentState
	toStatus *domain.EnvironmentStatus
}

func newFakeReconcileRepository(seed ...*domain.Environment) *fakeReconcileRepository {
	repo := &fakeReconcileRepository{envs: make(map[string]*domain.Environment, len(seed))}
	for _, env := range seed {
		repo.envs[env.Name().String()] = cloneServiceEnvironment(env)
	}
	return repo
}

func (r *fakeReconcileRepository) Get(_ context.Context, name domain.EnvironmentName) (*domain.Environment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envs[name.String()]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneServiceEnvironment(env), nil
}

func (r *fakeReconcileRepository) ListByStates(_ context.Context, _ ...domain.EnvironmentState) ([]*domain.Environment, error) {
	return nil, nil
}

func (r *fakeReconcileRepository) ListNeedingReconcile(_ context.Context) ([]*domain.Environment, error) {
	return nil, nil
}

func (r *fakeReconcileRepository) ListByScope(_ context.Context, _ string, _ int32, _ string) ([]*domain.Environment, string, error) {
	return nil, "", nil
}

func (r *fakeReconcileRepository) Create(_ context.Context, _ *domain.Environment) error {
	return nil
}

func (r *fakeReconcileRepository) UpdateDesired(_ context.Context, _ domain.EnvironmentName, _ int64, _ *domain.DesiredState, _ domain.EnvironmentDesired) error {
	return nil
}

func (r *fakeReconcileRepository) Delete(_ context.Context, name domain.EnvironmentName) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.envs, name.String())
	return nil
}

func (r *fakeReconcileRepository) TransitionStatus(_ context.Context, name domain.EnvironmentName, expectedGeneration int64, fromState domain.EnvironmentState, toStatus *domain.EnvironmentStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.transitionFn != nil {
		return r.transitionFn(name, expectedGeneration, fromState, toStatus)
	}

	env, ok := r.envs[name.String()]
	if !ok {
		return domain.ErrNotFound
	}
	if env.Generation() != expectedGeneration {
		return domain.ErrStaleGeneration
	}
	if env.Status().State != fromState {
		return domain.ErrStaleState
	}

	newStatus := &domain.EnvironmentStatus{
		Desired:            toStatus.Desired,
		State:              toStatus.State,
		ObservedGeneration: toStatus.ObservedGeneration,
		Message:            toStatus.Message,
		LastReconcileTime:  toStatus.LastReconcileTime,
		LastSuccessTime:    toStatus.LastSuccessTime,
	}

	envSnap := domain.EnvironmentSnapshot{
		Name:         env.Name(),
		EnvType:      env.Type(),
		Description:  env.Description(),
		DesiredState: env.DesiredState(),
		Status:       newStatus,
		Generation:   env.Generation(),
		CreateTime:   env.CreateTime(),
		UpdateTime:   env.UpdateTime(),
		ETag:         env.ETag(),
	}
	rehydrated, err := domain.RehydrateEnvironment(envSnap)
	if err != nil {
		return err
	}
	r.envs[name.String()] = rehydrated
	r.transitions = append(r.transitions, transitionRecord{
		name:      name,
		gen:       expectedGeneration,
		fromState: fromState,
		toState:   toStatus.State,
		toStatus:  toStatus,
	})
	return nil
}

func (r *fakeReconcileRepository) transitionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.transitions)
}

func cloneServiceEnvironment(env *domain.Environment) *domain.Environment {
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
		panic(fmt.Sprintf("clone environment: %v", err))
	}
	return cloned
}

// ---------------------------------------------------------------------------
// Fake runtime
// ---------------------------------------------------------------------------

type fakeReconcileRuntime struct {
	mu             sync.Mutex
	applyErrs      []error
	applyErrCalls  int
	checkRolloutFn func() (*domain.RolloutStatus, error)
	deleteErrs     []error
	deleteCalls    int
}

func (r *fakeReconcileRuntime) ApplyResources(_ context.Context, _ *domain.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyErrCalls++
	if len(r.applyErrs) > 0 {
		err := r.applyErrs[0]
		if len(r.applyErrs) > 1 {
			r.applyErrs = r.applyErrs[1:]
		}
		return err
	}
	return nil
}

func (r *fakeReconcileRuntime) CheckRollout(_ context.Context, _ *domain.Environment) (*domain.RolloutStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checkRolloutFn != nil {
		return r.checkRolloutFn()
	}
	return &domain.RolloutStatus{State: domain.RolloutReady}, nil
}

func (r *fakeReconcileRuntime) Delete(_ context.Context, _ domain.EnvironmentName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	if len(r.deleteErrs) > 0 {
		err := r.deleteErrs[0]
		if len(r.deleteErrs) > 1 {
			r.deleteErrs = r.deleteErrs[1:]
		}
		return err
	}
	return nil
}

func (r *fakeReconcileRuntime) QueryServiceEndpoints(_ context.Context, _, _, _ string) (*domain.ServiceQueryResult, error) {
	return nil, nil
}

func (r *fakeReconcileRuntime) QueryStatefulServiceEndpoints(_ context.Context, _, _, _ string) (*domain.ServiceQueryResult, error) {
	return nil, nil
}

func (r *fakeReconcileRuntime) ReservedEnvironmentVariableNames(_ context.Context) ([]string, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Tests – DesiredPresent state transitions
// ---------------------------------------------------------------------------

func TestProcessOne_pendingToReconciling(t *testing.T) {
	ctx := context.Background()
	env := mustNewServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if result.Terminal {
		t.Fatal("ProcessResult.Terminal = true, want false")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateReconciling {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateReconciling)
	}
}

func TestProcessOne_readyMatchingGenerationTerminal(t *testing.T) {
	ctx := context.Background()
	env := mustReadyServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false")
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}
	if repo.transitionCount() != 0 {
		t.Fatalf("TransitionStatus calls = %d, want 0", repo.transitionCount())
	}
}

func TestProcessOne_readyStaleGenerationToReconciling(t *testing.T) {
	ctx := context.Background()
	env := mustReadyServiceEnvironment(t, "dev", "alpha")
	env = mustEnvironmentWithGeneration(t, env, env.Generation()+1)
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateReconciling {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateReconciling)
	}
}

func TestProcessOne_failedToReconciling(t *testing.T) {
	ctx := context.Background()
	env := mustFailedServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateReconciling {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateReconciling)
	}
}

func TestProcessOne_reconcilingToWaitingRollout(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if result.Terminal {
		t.Fatal("ProcessResult.Terminal = true, want false")
	}
	if result.RequeueAfter == 0 {
		t.Fatal("ProcessResult.RequeueAfter = 0, want non-zero")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateWaitingRollout {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateWaitingRollout)
	}
	if got.Status().ObservedGeneration != got.Generation() {
		t.Fatalf("ObservedGeneration = %d, want %d", got.Status().ObservedGeneration, got.Generation())
	}
}

func TestProcessOne_reconcilingApplyFailureStaysReconciling(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	applyErr := errors.New("apply failed")
	runtime := &fakeReconcileRuntime{
		applyErrs: []error{applyErr},
	}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
	if !errors.Is(err, domain.ErrRetryCounted) {
		t.Fatalf("error does not wrap ErrRetryCounted: %v", err)
	}
	if result != nil && result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false on error")
	}

	got, getErr := repo.Get(ctx, env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != domain.StateReconciling {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateReconciling)
	}
}

func TestProcessOne_waitingRolloutToReady(t *testing.T) {
	ctx := context.Background()
	env := mustWaitingRolloutServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{
		checkRolloutFn: func() (*domain.RolloutStatus, error) {
			return &domain.RolloutStatus{State: domain.RolloutReady}, nil
		},
	}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateReady)
	}
	if got.Status().Message != "ready" {
		t.Fatalf("message = %q, want \"ready\"", got.Status().Message)
	}
	if got.Status().LastSuccessTime.IsZero() {
		t.Fatal("LastSuccessTime is zero, want non-zero")
	}
}

func TestProcessOne_waitingRolloutToFailed(t *testing.T) {
	ctx := context.Background()
	env := mustWaitingRolloutServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	failMsg := "CrashLoopBackOff"
	runtime := &fakeReconcileRuntime{
		checkRolloutFn: func() (*domain.RolloutStatus, error) {
			return &domain.RolloutStatus{State: domain.RolloutFailed, Message: failMsg}, nil
		},
	}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateFailed {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateFailed)
	}
	if got.Status().Message != failMsg {
		t.Fatalf("message = %q, want %q", got.Status().Message, failMsg)
	}
}

func TestProcessOne_waitingRolloutMessageUnchanged(t *testing.T) {
	ctx := context.Background()
	env := mustWaitingRolloutServiceEnvironment(t, "dev", "alpha")
	if err := env.SetWaitingRolloutMessage("still deploying"); err != nil {
		t.Fatalf("SetWaitingRolloutMessage() error = %v", err)
	}
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{
		checkRolloutFn: func() (*domain.RolloutStatus, error) {
			return &domain.RolloutStatus{State: domain.RolloutWaiting, Message: "still deploying"}, nil
		},
	}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false")
	}
	if result.Terminal {
		t.Fatal("ProcessResult.Terminal = true, want false")
	}
	if result.RequeueAfter == 0 {
		t.Fatal("ProcessResult.RequeueAfter = 0, want non-zero")
	}
	if repo.transitionCount() != 0 {
		t.Fatalf("TransitionStatus calls = %d, want 0 (message unchanged)", repo.transitionCount())
	}
}

func TestProcessOne_waitingRolloutMessageChanged(t *testing.T) {
	ctx := context.Background()
	env := mustWaitingRolloutServiceEnvironment(t, "dev", "alpha")
	if err := env.SetWaitingRolloutMessage("old message"); err != nil {
		t.Fatalf("SetWaitingRolloutMessage() error = %v", err)
	}
	repo := newFakeReconcileRepository(env)
	newMsg := "pods: 2/3 ready"
	runtime := &fakeReconcileRuntime{
		checkRolloutFn: func() (*domain.RolloutStatus, error) {
			return &domain.RolloutStatus{State: domain.RolloutWaiting, Message: newMsg}, nil
		},
	}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false (message-only update)")
	}
	if result.RequeueAfter == 0 {
		t.Fatal("ProcessResult.RequeueAfter = 0, want non-zero")
	}
	if repo.transitionCount() != 1 {
		t.Fatalf("TransitionStatus calls = %d, want 1", repo.transitionCount())
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().Message != newMsg {
		t.Fatalf("message = %q, want %q", got.Status().Message, newMsg)
	}
}

// ---------------------------------------------------------------------------
// Tests – DesiredAbsent state transitions
// ---------------------------------------------------------------------------

func TestProcessOne_absentPendingToDeleting(t *testing.T) {
	ctx := context.Background()
	env := mustAbsentPendingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if result.Terminal {
		t.Fatal("ProcessResult.Terminal = true, want false")
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != domain.StateDeleting {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateDeleting)
	}
}

func TestProcessOne_absentDeletingToDeleted(t *testing.T) {
	ctx := context.Background()
	env := mustAbsentDeletingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}

	_, getErr := repo.Get(ctx, env.Name())
	if !errors.Is(getErr, domain.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", getErr)
	}
}

func TestProcessOne_absentDeleteFailureRetry(t *testing.T) {
	ctx := context.Background()
	env := mustAbsentDeletingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	deleteErr := errors.New("runtime error")
	runtime := &fakeReconcileRuntime{
		deleteErrs: []error{deleteErr},
	}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
	if !errors.Is(err, domain.ErrRetryCounted) {
		t.Fatalf("error does not wrap ErrRetryCounted: %v", err)
	}
	if result != nil && result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false on error")
	}

	got, getErr := repo.Get(ctx, env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != domain.StateDeleting {
		t.Fatalf("state = %v, want %v (state unchanged)", got.Status().State, domain.StateDeleting)
	}
}

// ---------------------------------------------------------------------------
// Tests – MarkRetryExhausted
// ---------------------------------------------------------------------------

func TestReconcileService_MarkRetryExhausted(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.MarkRetryExhausted(ctx, env.Name())
	if err != nil {
		t.Fatalf("MarkRetryExhausted() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}

	got, getErr := repo.Get(ctx, env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != domain.StateFailed {
		t.Fatalf("state = %v, want %v", got.Status().State, domain.StateFailed)
	}
	if got.Status().Message != "retry count exhausted" {
		t.Fatalf("message = %q, want %q", got.Status().Message, "retry count exhausted")
	}
	if got.Status().ObservedGeneration != got.Generation() {
		t.Fatalf("ObservedGeneration = %d, want %d", got.Status().ObservedGeneration, got.Generation())
	}
}

func TestReconcileService_MarkRetryExhaustedNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeReconcileRepository()
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	name, _ := domain.NewEnvironmentName("dev", "nope")
	result, err := svc.MarkRetryExhausted(ctx, name)
	if err != nil {
		t.Fatalf("MarkRetryExhausted() error = %v", err)
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}
}

func TestReconcileService_MarkRetryExhaustedStaleState(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	repo.transitionFn = func(name domain.EnvironmentName, gen int64, fromState domain.EnvironmentState, toStatus *domain.EnvironmentStatus) error {
		return domain.ErrStaleState
	}
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.MarkRetryExhausted(ctx, env.Name())
	if err != nil {
		t.Fatalf("MarkRetryExhausted() error = %v", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false")
	}
	if result.RequeueAfter != 1 {
		t.Fatalf("ProcessResult.RequeueAfter = %d, want 1", result.RequeueAfter)
	}
}

// ---------------------------------------------------------------------------
// Tests – stale state & generation
// ---------------------------------------------------------------------------

func TestProcessOne_staleStateRetryWithFreshEnv(t *testing.T) {
	ctx := context.Background()
	env := mustNewServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)

	// TransitionStatus returns ErrStaleState; ProcessOne should return
	// RequeueAfter instead of recursing.
	repo.transitionFn = func(name domain.EnvironmentName, gen int64, fromState domain.EnvironmentState, toStatus *domain.EnvironmentStatus) error {
		return domain.ErrStaleState
	}
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false")
	}
	if result.RequeueAfter != 1 {
		t.Fatalf("ProcessResult.RequeueAfter = %d, want 1", result.RequeueAfter)
	}
}

func TestProcessOne_staleGenerationRetryWithFreshEnv(t *testing.T) {
	ctx := context.Background()
	env := mustNewServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)

	repo.transitionFn = func(name domain.EnvironmentName, gen int64, fromState domain.EnvironmentState, toStatus *domain.EnvironmentStatus) error {
		return domain.ErrStaleGeneration
	}
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false")
	}
	if result.RequeueAfter != 1 {
		t.Fatalf("ProcessResult.RequeueAfter = %d, want 1", result.RequeueAfter)
	}
}

// ---------------------------------------------------------------------------
// Tests – error handling
// ---------------------------------------------------------------------------

func TestProcessOne_envNotFoundTerminal(t *testing.T) {
	ctx := context.Background()
	repo := newFakeReconcileRepository()
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	name, _ := domain.NewEnvironmentName("dev", "nope")
	result, err := svc.ProcessOne(ctx, name)
	if err != nil {
		t.Fatalf("ProcessOne() error = %v, want nil", err)
	}
	if result.Changed {
		t.Fatal("ProcessResult.Changed = true, want false")
	}
	if !result.Terminal {
		t.Fatal("ProcessResult.Terminal = false, want true")
	}
}

func TestProcessOne_getError(t *testing.T) {
	ctx := context.Background()
	env := mustNewServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	repo.getErr = errors.New("storage error")
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	_, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
}

// ---------------------------------------------------------------------------
// Test – single state advance per call
// ---------------------------------------------------------------------------

func TestProcessOne_advancesAtMostOneStatePerCall(t *testing.T) {
	ctx := context.Background()
	env := mustNewServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	// Call 1: Pending → Reconciling
	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() call 1 error = %v", err)
	}
	if !result.Changed {
		t.Fatal("call 1: ProcessResult.Changed = false, want true")
	}

	got, _ := repo.Get(ctx, env.Name())
	if got.Status().State != domain.StateReconciling {
		t.Fatalf("call 1: state = %v, want Reconciling", got.Status().State)
	}

	// Call 2: Reconciling → WaitingRollout
	result, err = svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() call 2 error = %v", err)
	}
	if !result.Changed {
		t.Fatal("call 2: ProcessResult.Changed = false, want true")
	}

	got, _ = repo.Get(ctx, env.Name())
	if got.Status().State != domain.StateWaitingRollout {
		t.Fatalf("call 2: state = %v, want WaitingRollout", got.Status().State)
	}

	// Call 3: WaitingRollout → Ready
	result, err = svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() call 3 error = %v", err)
	}
	if !result.Changed {
		t.Fatal("call 3: ProcessResult.Changed = false, want true")
	}
	if !result.Terminal {
		t.Fatal("call 3: ProcessResult.Terminal = false, want true")
	}

	got, _ = repo.Get(ctx, env.Name())
	if got.Status().State != domain.StateReady {
		t.Fatalf("call 3: state = %v, want Ready", got.Status().State)
	}
}

// ---------------------------------------------------------------------------
// Test – CheckRollout error propagation
// ---------------------------------------------------------------------------

func TestProcessOne_checkRolloutError(t *testing.T) {
	ctx := context.Background()
	env := mustWaitingRolloutServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	checkErr := errors.New("rollout check failed")
	runtime := &fakeReconcileRuntime{
		checkRolloutFn: func() (*domain.RolloutStatus, error) {
			return nil, checkErr
		},
	}
	svc := NewReconcileService(repo, runtime)

	_, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
}

// ---------------------------------------------------------------------------
// Test – absent transition from other active states
// ---------------------------------------------------------------------------

func TestProcessOne_absentReconcilingToDeleting(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingServiceEnvironment(t, "dev", "alpha")
	// Transition to absent desired state (simulating SetDesiredAbsent was called while reconciling)
	if err := env.SetDesiredAbsent(); err != nil {
		t.Fatalf("SetDesiredAbsent() error = %v", err)
	}
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}

	got, _ := repo.Get(ctx, env.Name())
	if got.Status().State != domain.StateDeleting {
		t.Fatalf("state = %v, want Deleting", got.Status().State)
	}
}

func TestProcessOne_absentWaitingRolloutToDeleting(t *testing.T) {
	ctx := context.Background()
	env := mustWaitingRolloutServiceEnvironment(t, "dev", "alpha")
	if err := env.SetDesiredAbsent(); err != nil {
		t.Fatalf("SetDesiredAbsent() error = %v", err)
	}
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}

	got, _ := repo.Get(ctx, env.Name())
	if got.Status().State != domain.StateDeleting {
		t.Fatalf("state = %v, want Deleting", got.Status().State)
	}
}

func TestProcessOne_absentFailedToDeleting(t *testing.T) {
	ctx := context.Background()
	env := mustFailedServiceEnvironment(t, "dev", "alpha")
	if err := env.SetDesiredAbsent(); err != nil {
		t.Fatalf("SetDesiredAbsent() error = %v", err)
	}
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	result, err := svc.ProcessOne(ctx, env.Name())
	if err != nil {
		t.Fatalf("ProcessOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("ProcessResult.Changed = false, want true")
	}

	got, _ := repo.Get(ctx, env.Name())
	if got.Status().State != domain.StateDeleting {
		t.Fatalf("state = %v, want Deleting", got.Status().State)
	}
}

// ---------------------------------------------------------------------------
// Test – context cancellation propagation
// ---------------------------------------------------------------------------

func TestProcessOne_applyContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	env := mustReconcilingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{
		applyErrs: []error{context.Canceled},
	}
	svc := NewReconcileService(repo, runtime)

	_, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
	if errors.Is(err, domain.ErrRetryCounted) {
		t.Fatal("error should not wrap ErrRetryCounted for context.Canceled")
	}
}

func TestProcessOne_deleteContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	env := mustAbsentDeletingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	runtime := &fakeReconcileRuntime{
		deleteErrs: []error{context.Canceled},
	}
	svc := NewReconcileService(repo, runtime)

	_, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
	if errors.Is(err, domain.ErrRetryCounted) {
		t.Fatal("error should not wrap ErrRetryCounted for context.Canceled")
	}
}

// ---------------------------------------------------------------------------
// Test – repo.Delete failure during absent processing
// ---------------------------------------------------------------------------

func TestProcessOne_absentDeleteRepoError(t *testing.T) {
	ctx := context.Background()
	env := mustAbsentDeletingServiceEnvironment(t, "dev", "alpha")
	repo := newFakeReconcileRepository(env)
	repo.deleteErr = errors.New("repo error")
	runtime := &fakeReconcileRuntime{}
	svc := NewReconcileService(repo, runtime)

	_, err := svc.ProcessOne(ctx, env.Name())
	if err == nil {
		t.Fatal("ProcessOne() error = nil, want error")
	}
}
