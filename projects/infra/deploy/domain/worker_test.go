package domain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWorker_processPresentPendingSuccess(t *testing.T) {
	ctx := context.Background()
	env := mustNewWorkerEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
	if len(repo.savedEnvs) != 2 {
		t.Fatalf("Save() calls = %d, want 2", len(repo.savedEnvs))
	}
	if repo.savedEnvs[0].Status().State != StateReconciling {
		t.Fatalf("first saved state = %v, want %v", repo.savedEnvs[0].Status().State, StateReconciling)
	}
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReady)
	}
	if got.Status().ObservedGeneration != got.Generation() {
		t.Fatalf("ObservedGeneration = %d, want %d", got.Status().ObservedGeneration, got.Generation())
	}
	if got.Status().Message != "" {
		t.Fatalf("message = %q, want empty", got.Status().Message)
	}
	if got.Status().LastSuccessTime.IsZero() {
		t.Fatal("LastSuccessTime is zero, want non-zero")
	}
}

func TestWorker_processPresentReadyCurrentGenerationNoop(t *testing.T) {
	ctx := context.Background()
	env := mustReadyEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 0 {
		t.Fatalf("Apply() calls = %d, want 0", len(runtime.applyEnvs))
	}
	if len(repo.savedEnvs) != 0 {
		t.Fatalf("Save() calls = %d, want 0", len(repo.savedEnvs))
	}
}

func TestWorker_processPresentReadyStaleGenerationReapplies(t *testing.T) {
	ctx := context.Background()
	env := mustEnvironmentWithGeneration(t, mustReadyEnvironment(t, "dev", "alpha"), 2)
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
	if len(repo.savedEnvs) != 2 {
		t.Fatalf("Save() calls = %d, want 2", len(repo.savedEnvs))
	}
	if repo.savedEnvs[0].Status().State != StateReconciling {
		t.Fatalf("first saved state = %v, want %v", repo.savedEnvs[0].Status().State, StateReconciling)
	}
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReady)
	}
	if got.Status().ObservedGeneration != got.Generation() {
		t.Fatalf("ObservedGeneration = %d, want %d", got.Status().ObservedGeneration, got.Generation())
	}
}

func TestWorker_processPresentFailedSameGenerationResetsAndApplies(t *testing.T) {
	ctx := context.Background()
	env := mustFailedEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
	if len(repo.savedEnvs) != 2 {
		t.Fatalf("Save() calls = %d, want 2", len(repo.savedEnvs))
	}
	if repo.savedEnvs[0].Status().State != StateReconciling {
		t.Fatalf("first saved state = %v, want %v", repo.savedEnvs[0].Status().State, StateReconciling)
	}
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReady)
	}
}

func TestWorker_processPresentFailedStaleGenerationReapplies(t *testing.T) {
	ctx := context.Background()
	env := mustEnvironmentWithGeneration(t, mustFailedEnvironment(t, "dev", "alpha"), 2)
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
	if len(repo.savedEnvs) != 2 {
		t.Fatalf("Save() calls = %d, want 2", len(repo.savedEnvs))
	}
	if repo.savedEnvs[0].Status().State != StateReconciling {
		t.Fatalf("first saved state = %v, want %v", repo.savedEnvs[0].Status().State, StateReconciling)
	}
	if repo.savedEnvs[1].Status().State != StateReady {
		t.Fatalf("second saved state = %v, want %v", repo.savedEnvs[1].Status().State, StateReady)
	}
}

func TestWorker_processPresentReconcilingFailureReturnsRetryCounted(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{applyErr: errors.New("apply failed")}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if !errors.Is(err, ErrRetryCounted) {
		t.Fatalf("process() error = %v, want %v", err, ErrRetryCounted)
	}

	got, getErr := repo.Get(ctx, env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != StateFailed {
		t.Fatalf("state = %v, want %v", got.Status().State, StateFailed)
	}
	if got.Status().Message != "apply failed" {
		t.Fatalf("message = %q, want %q", got.Status().Message, "apply failed")
	}
	if got.Status().ObservedGeneration != got.Generation() {
		t.Fatalf("ObservedGeneration = %d, want %d", got.Status().ObservedGeneration, got.Generation())
	}
}

func TestWorker_processContextCanceledDuringApply(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, _ *Environment, _ func(string)) error {
			cancel()
			return fmt.Errorf("apply canceled: %w", context.Canceled)
		},
	}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("process() error = %v, want %v", err, context.Canceled)
	}

	got, getErr := repo.Get(context.Background(), env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != StateReconciling {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReconciling)
	}
	if len(repo.savedEnvs) != 0 {
		t.Fatalf("Save() calls = %d, want 0", len(repo.savedEnvs))
	}
}

func TestWorker_processAbsentPendingDeletes(t *testing.T) {
	ctx := context.Background()
	env := mustAbsentPendingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.deleteNames) != 1 {
		t.Fatalf("Delete() calls = %d, want 1", len(runtime.deleteNames))
	}
	if len(repo.savedEnvs) != 1 {
		t.Fatalf("Save() calls = %d, want 1", len(repo.savedEnvs))
	}
	if repo.savedEnvs[0].Status().State != StateDeleting {
		t.Fatalf("saved state = %v, want %v", repo.savedEnvs[0].Status().State, StateDeleting)
	}
	if _, err := repo.Get(ctx, env.Name()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotFound)
	}
}

func TestWorker_processAbsentDeletingFailureReturnsRetryCounted(t *testing.T) {
	ctx := context.Background()
	env := mustAbsentDeletingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{deleteErr: errors.New("delete failed")}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: env.Name()})
	if !errors.Is(err, ErrRetryCounted) {
		t.Fatalf("process() error = %v, want %v", err, ErrRetryCounted)
	}

	got, getErr := repo.Get(ctx, env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != StateDeleting {
		t.Fatalf("state = %v, want %v", got.Status().State, StateDeleting)
	}
	if got.Status().Message != "delete failed" {
		t.Fatalf("message = %q, want %q", got.Status().Message, "delete failed")
	}
	if len(repo.deletedNames) != 0 {
		t.Fatalf("repo.Delete() calls = %d, want 0", len(repo.deletedNames))
	}
}

func TestWorker_processMissingEnvironmentSkipped(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	repo := newFakeWorkerRepository()
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, &WorkItem{EnvName: envName})
	if err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 0 {
		t.Fatalf("Apply() calls = %d, want 0", len(runtime.applyEnvs))
	}
	if len(runtime.deleteNames) != 0 {
		t.Fatalf("Delete() calls = %d, want 0", len(runtime.deleteNames))
	}
}

func TestWorker_RunCountedRetryBackoffIsNonBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	envA := mustReconcilingEnvironment(t, "dev", "alpha")
	envB := mustReconcilingEnvironment(t, "dev", "beta")
	queue := NewQueue()
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer safeStopQueue(queue)
	if err := queue.Enqueue(ctx, envA.Name()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(ctx, envB.Name()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	repo := newFakeWorkerRepository(envA, envB)
	processedB := make(chan struct{})
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, env *Environment, _ func(string)) error {
			switch env.Name() {
			case envA.Name():
				return errors.New("apply failed")
			case envB.Name():
				close(processedB)
				queue.Stop()
				return nil
			default:
				return nil
			}
		},
	}
	worker := NewWorker(repo, queue, runtime)
	retryWait := make(chan time.Time)
	var mu sync.Mutex
	var delays []time.Duration
	afterCalled := make(chan struct{}, 1)
	worker.after = func(delay time.Duration) <-chan time.Time {
		mu.Lock()
		delays = append(delays, delay)
		mu.Unlock()
		afterCalled <- struct{}{}
		return retryWait
	}

	done := make(chan struct{})
	go func() {
		worker.Run()
		close(done)
	}()

	select {
	case <-processedB:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() blocked on retry backoff")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return")
	}

	select {
	case <-afterCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("retry scheduling did not happen")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(delays) != 1 {
		t.Fatalf("retry delays = %d, want 1", len(delays))
	}
	if delays[0] != time.Second {
		t.Fatalf("retry delay = %v, want %v", delays[0], time.Second)
	}
}

func TestWorker_RunMaxRetriesStopsRetry(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	queue := NewQueue()
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer safeStopQueue(queue)
	if err := queue.EnqueueRetry(ctx, &WorkItem{EnvName: env.Name(), RetryCount: defaultMaxRetries}); err != nil {
		t.Fatalf("EnqueueRetry() error = %v", err)
	}
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, _ *Environment, _ func(string)) error {
			queue.Stop()
			return errors.New("apply failed")
		},
	}
	worker := NewWorker(repo, queue, runtime)
	worker.after = func(time.Duration) <-chan time.Time {
		t.Fatal("after() called, want no retry scheduling")
		return nil
	}

	done := make(chan struct{})
	go func() {
		worker.Run()
		close(done)
	}()

	waitWorkerDone(t, done)
	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
}

func TestWorker_RunContinuesAfterFatalError(t *testing.T) {
	ctx := context.Background()
	envA := mustWorkerEnvName(t, "dev", "alpha")
	envB := mustReconcilingEnvironment(t, "dev", "beta")
	queue := NewQueue()
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer safeStopQueue(queue)
	if err := queue.Enqueue(ctx, envA); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(ctx, envB.Name()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	repo := newFakeWorkerRepository(envB)
	workerRepo := &runFatalRepository{
		fakeWorkerRepository: repo,
		failName:             envA,
		failErr:              errors.New("storage unavailable"),
	}
	processedB := make(chan struct{})
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, env *Environment, _ func(string)) error {
			if env.Name() == envB.Name() {
				close(processedB)
				queue.Stop()
			}
			return nil
		},
	}
	worker := NewWorker(workerRepo, queue, runtime)

	done := make(chan struct{})
	go func() {
		worker.Run()
		close(done)
	}()

	select {
	case <-processedB:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() stopped after fatal iteration error")
	}

	waitWorkerDone(t, done)
	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
}

func TestWorker_RunReturnsAfterQueueStop(t *testing.T) {
	ctx := context.Background()
	queue := NewQueue()
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	worker := NewWorker(newFakeWorkerRepository(), queue, &fakeEnvironmentRuntime{})

	done := make(chan struct{})
	go func() {
		worker.Run()
		close(done)
	}()

	queue.Stop()
	waitWorkerDone(t, done)
}

func TestWorker_RunIterationTimeout(t *testing.T) {
	ctx := context.Background()
	envA := mustReconcilingEnvironment(t, "dev", "alpha")
	envB := mustReconcilingEnvironment(t, "dev", "beta")
	queue := NewQueue()
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer safeStopQueue(queue)
	if err := queue.Enqueue(ctx, envA.Name()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(ctx, envB.Name()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	repo := newFakeWorkerRepository(envA, envB)
	processedB := make(chan struct{})
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(ctx context.Context, env *Environment, _ func(string)) error {
			if env.Name() == envA.Name() {
				<-ctx.Done()
				return ctx.Err()
			}
			close(processedB)
			queue.Stop()
			return nil
		},
	}
	worker := NewWorker(repo, queue, runtime)
	worker.iterTimeout = 20 * time.Millisecond

	done := make(chan struct{})
	go func() {
		worker.Run()
		close(done)
	}()

	select {
	case <-processedB:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() stopped after iteration timeout")
	}

	waitWorkerDone(t, done)
	if len(runtime.applyEnvs) != 2 {
		t.Fatalf("Apply() calls = %d, want 2", len(runtime.applyEnvs))
	}
}

type fakeWorkerRepository struct {
	mu           sync.Mutex
	envs         map[string]*Environment
	savedEnvs    []*Environment
	deletedNames []EnvironmentName
	saveFn       func(env *Environment) error
	getErr       error
	saveErr      error
	deleteErr    error
}

type runFatalRepository struct {
	*fakeWorkerRepository
	failName EnvironmentName
	failErr  error
}

func newFakeWorkerRepository(seed ...*Environment) *fakeWorkerRepository {
	repo := &fakeWorkerRepository{envs: make(map[string]*Environment, len(seed))}
	for _, env := range seed {
		repo.envs[env.Name().String()] = cloneEnvironmentOrPanic(env)
	}
	return repo
}

func (r *runFatalRepository) Get(ctx context.Context, name EnvironmentName) (*Environment, error) {
	if name == r.failName {
		return nil, r.failErr
	}
	return r.fakeWorkerRepository.Get(ctx, name)
}

func (r *fakeWorkerRepository) Get(_ context.Context, name EnvironmentName) (*Environment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	env, ok := r.envs[name.String()]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneEnvironmentOrPanic(env), nil
}

func (r *fakeWorkerRepository) ListByStates(_ context.Context, _ ...EnvironmentState) ([]*Environment, error) {
	return nil, nil
}

func (r *fakeWorkerRepository) ListNeedingReconcile(_ context.Context) ([]*Environment, error) {
	return nil, nil
}

func (r *fakeWorkerRepository) ListByScope(_ context.Context, _ string, _ int32, _ string) ([]*Environment, string, error) {
	return nil, "", nil
}

func (r *fakeWorkerRepository) Save(_ context.Context, env *Environment) error {
	if r.saveFn != nil {
		if err := r.saveFn(env); err != nil {
			return err
		}
	}
	if r.saveErr != nil {
		return r.saveErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cloned := cloneEnvironmentOrPanic(env)
	r.envs[env.Name().String()] = cloned
	r.savedEnvs = append(r.savedEnvs, cloneEnvironmentOrPanic(env))
	return nil
}

func (r *fakeWorkerRepository) Delete(_ context.Context, name EnvironmentName) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.envs, name.String())
	r.deletedNames = append(r.deletedNames, name)
	return nil
}

type fakeEnvironmentRuntime struct {
	mu            sync.Mutex
	applyEnvs     []*Environment
	applyFn       func(ctx context.Context, env *Environment, progress func(msg string)) error
	deleteNames   []EnvironmentName
	applyErr      error
	deleteErr     error
	applyProgress func(msg string)
}

func (r *fakeEnvironmentRuntime) Apply(ctx context.Context, env *Environment, progress func(msg string)) error {
	r.mu.Lock()
	r.applyEnvs = append(r.applyEnvs, cloneEnvironmentOrPanic(env))
	r.applyProgress = progress
	applyFn := r.applyFn
	applyErr := r.applyErr
	r.mu.Unlock()

	if applyFn != nil {
		return applyFn(ctx, env, progress)
	}
	return applyErr
}

func (r *fakeEnvironmentRuntime) Delete(_ context.Context, envName EnvironmentName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteNames = append(r.deleteNames, envName)
	return r.deleteErr
}

func (r *fakeEnvironmentRuntime) QueryServiceEndpoints(_ context.Context, _, _, _ string) (*ServiceQueryResult, error) {
	return nil, nil
}

func (r *fakeEnvironmentRuntime) QueryStatefulServiceEndpoints(_ context.Context, _, _, _ string) (*ServiceQueryResult, error) {
	return nil, nil
}

func (r *fakeEnvironmentRuntime) ReservedEnvironmentVariableNames(_ context.Context) ([]string, error) {
	return nil, nil
}

func mustWorkerEnvName(t *testing.T, scope, env string) EnvironmentName {
	t.Helper()
	name, err := NewEnvironmentName(scope, env)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}
	return name
}

func mustNewWorkerEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	name := mustWorkerEnvName(t, scope, env)
	environment, err := NewEnvironment(name, EnvironmentTypeProd, env, &DesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:     "api",
			App:      "gateway",
			Image:    "example.com/gateway:v1",
			Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas: 1,
			HTTP: &ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}
	return environment
}

func mustReconcilingEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	environment := mustNewWorkerEnvironment(t, scope, env)
	if err := environment.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() error = %v", err)
	}
	return environment
}

func mustReadyEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	environment := mustReconcilingEnvironment(t, scope, env)
	if err := environment.MarkReady(environment.Generation()); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	return environment
}

func mustFailedEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	environment := mustReconcilingEnvironment(t, scope, env)
	if err := environment.MarkFailed(environment.Generation(), "apply failed"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	return environment
}

func mustAbsentPendingEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	environment := mustNewWorkerEnvironment(t, scope, env)
	if err := environment.SetDesiredAbsent(); err != nil {
		t.Fatalf("SetDesiredAbsent() error = %v", err)
	}
	return environment
}

func mustAbsentDeletingEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	environment := mustAbsentPendingEnvironment(t, scope, env)
	if err := environment.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() error = %v", err)
	}
	return environment
}

func mustEnvironmentWithGeneration(t *testing.T, env *Environment, generation int64) *Environment {
	t.Helper()
	return cloneEnvironmentWithGenerationOrPanic(env, generation)
}

func safeStopQueue(queue *Queue) {
	defer func() {
		_ = recover()
	}()
	queue.Stop()
}

func waitWorkerDone(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}
}

func cloneEnvironmentOrPanic(env *Environment) *Environment {
	return cloneEnvironmentWithGenerationOrPanic(env, env.Generation())
}

func cloneEnvironmentWithGenerationOrPanic(env *Environment, generation int64) *Environment {
	cloned, err := RehydrateEnvironment(EnvironmentSnapshot{
		Name:         env.Name(),
		EnvType:      env.Type(),
		Description:  env.Description(),
		DesiredState: env.DesiredState(),
		Status: &EnvironmentStatus{
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
		panic(fmt.Sprintf("clone environment: %v", err))
	}
	return cloned
}
