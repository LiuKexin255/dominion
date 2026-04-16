package domain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWorkerProcess_ReconcilingApplySuccess(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 1 {
		t.Fatalf("Apply() calls = %d, want 1", len(runtime.applyEnvs))
	}
	if len(runtime.deleteNames) != 0 {
		t.Fatalf("Delete() calls = %d, want 0", len(runtime.deleteNames))
	}
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReady)
	}
	if got.Status().Message != "" {
		t.Fatalf("message = %q, want empty", got.Status().Message)
	}
	if got.Status().LastSuccessTime.IsZero() {
		t.Fatal("LastSuccessTime is zero, want non-zero")
	}
}

func TestWorkerProcess_ContextCancelledDuringApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, _ *Environment, _ func(msg string)) error {
			cancel()
			return fmt.Errorf("apply canceled: %w", context.Canceled)
		},
	}
	worker := NewWorker(repo, NewQueue(), runtime)

	err := worker.process(ctx, env.Name())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("process() error = %v, want wrapped %v", err, context.Canceled)
	}

	got, getErr := repo.Get(context.Background(), env.Name())
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if got.Status().State != StateReconciling {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReconciling)
	}
	if got.Status().Message != "" {
		t.Fatalf("message = %q, want empty", got.Status().Message)
	}
	if len(repo.savedEnvs) != 0 {
		t.Fatalf("Save() calls = %d, want 0", len(repo.savedEnvs))
	}
}

func TestWorkerProcess_ReconcilingApplyFailure(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{applyErr: errors.New("apply failed")}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateFailed {
		t.Fatalf("state = %v, want %v", got.Status().State, StateFailed)
	}
	if got.Status().Message != "apply failed" {
		t.Fatalf("message = %q, want %q", got.Status().Message, "apply failed")
	}
}

func TestWorkerProcess_ProgressCallbackUpdatesMessage(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, _ *Environment, progress func(msg string)) error {
			if progress == nil {
				t.Fatal("progress callback = nil, want non-nil")
			}
			progress("applying deployment")
			return nil
		},
	}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if runtime.applyProgress == nil {
		t.Fatal("Apply() progress callback = nil, want non-nil")
	}
	if len(repo.savedEnvs) < 2 {
		t.Fatalf("Save() calls = %d, want at least 2", len(repo.savedEnvs))
	}
	progressSaved := repo.savedEnvs[0]
	if progressSaved.Status().State != StateReconciling {
		t.Fatalf("progress save state = %v, want %v", progressSaved.Status().State, StateReconciling)
	}
	if progressSaved.Status().Message != "applying deployment" {
		t.Fatalf("progress save message = %q, want %q", progressSaved.Status().Message, "applying deployment")
	}
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReady)
	}
	if got.Status().Message != "" {
		t.Fatalf("message = %q, want empty", got.Status().Message)
	}
}

func TestWorkerProcess_ProgressCallbackSaveError(t *testing.T) {
	ctx := context.Background()
	env := mustReconcilingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	repo.saveFn = func(env *Environment) error {
		if env.Status().State == StateReconciling && env.Status().Message == "applying deployment" {
			return errors.New("save failed")
		}
		return nil
	}
	runtime := &fakeEnvironmentRuntime{
		applyFn: func(_ context.Context, _ *Environment, progress func(msg string)) error {
			if progress == nil {
				t.Fatal("progress callback = nil, want non-nil")
			}
			progress("applying deployment")
			return nil
		},
	}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(repo.savedEnvs) != 1 {
		t.Fatalf("successful Save() calls = %d, want 1", len(repo.savedEnvs))
	}
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status().State != StateReady {
		t.Fatalf("state = %v, want %v", got.Status().State, StateReady)
	}
}

func TestWorkerProcess_DeletingDeleteSuccess(t *testing.T) {
	ctx := context.Background()
	env := mustDeletingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.deleteNames) != 1 {
		t.Fatalf("Delete() calls = %d, want 1", len(runtime.deleteNames))
	}
	if len(repo.deletedNames) != 1 {
		t.Fatalf("repo.Delete() calls = %d, want 1", len(repo.deletedNames))
	}
	if _, err := repo.Get(ctx, env.Name()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotFound)
	}
}

func TestWorkerProcess_DeletingDeleteFailure(t *testing.T) {
	ctx := context.Background()
	env := mustDeletingEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{deleteErr: errors.New("delete failed")}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
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

func TestWorkerProcess_MissingEnvironmentIsSkipped(t *testing.T) {
	ctx := context.Background()
	envName := mustWorkerEnvName(t, "dev", "alpha")
	repo := newFakeWorkerRepository()
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, envName); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 0 {
		t.Fatalf("Apply() calls = %d, want 0", len(runtime.applyEnvs))
	}
	if len(runtime.deleteNames) != 0 {
		t.Fatalf("Delete() calls = %d, want 0", len(runtime.deleteNames))
	}
	if len(repo.savedEnvs) != 0 {
		t.Fatalf("Save() calls = %d, want 0", len(repo.savedEnvs))
	}
}

func TestWorkerProcess_SkipsUnsupportedState(t *testing.T) {
	ctx := context.Background()
	env := mustReadyEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(env)
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, NewQueue(), runtime)

	if err := worker.process(ctx, env.Name()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	if len(runtime.applyEnvs) != 0 {
		t.Fatalf("Apply() calls = %d, want 0", len(runtime.applyEnvs))
	}
	if len(runtime.deleteNames) != 0 {
		t.Fatalf("Delete() calls = %d, want 0", len(runtime.deleteNames))
	}
	if len(repo.savedEnvs) != 0 {
		t.Fatalf("Save() calls = %d, want 0", len(repo.savedEnvs))
	}
}

func TestWorkerRun_UsesLatestSnapshotFromRepository(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	queued := mustReconcilingEnvironment(t, "dev", "alpha")
	latest := mustReadyEnvironment(t, "dev", "alpha")
	repo := newFakeWorkerRepository(latest)
	repo.onGet = func() { cancel() }
	queue := NewQueue()
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer queue.Stop()
	if err := queue.Enqueue(ctx, queued.Name()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	runtime := &fakeEnvironmentRuntime{}
	worker := NewWorker(repo, queue, runtime)

	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return")
	}

	if len(runtime.applyEnvs) != 0 {
		t.Fatalf("Apply() calls = %d, want 0", len(runtime.applyEnvs))
	}
}

type fakeWorkerRepository struct {
	mu           sync.Mutex
	envs         map[string]*Environment
	savedEnvs    []*Environment
	deletedNames []EnvironmentName
	onGet        func()
	saveFn       func(env *Environment) error
	getErr       error
	saveErr      error
	deleteErr    error
}

func newFakeWorkerRepository(seed ...*Environment) *fakeWorkerRepository {
	repo := &fakeWorkerRepository{envs: make(map[string]*Environment, len(seed))}
	for _, env := range seed {
		repo.envs[env.Name().String()] = cloneEnvironmentOrPanic(env)
	}
	return repo
}

func (r *fakeWorkerRepository) Get(_ context.Context, name EnvironmentName) (*Environment, error) {
	if r.onGet != nil {
		r.onGet()
	}
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
	applyProgress func(msg string)
	deleteNames   []EnvironmentName
	applyErr      error
	deleteErr     error
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
	environment, err := NewEnvironment(name, env, &DesiredState{
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
	if err := environment.MarkReady(); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	return environment
}

func mustDeletingEnvironment(t *testing.T, scope, env string) *Environment {
	t.Helper()
	environment := mustNewWorkerEnvironment(t, scope, env)
	if err := environment.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() error = %v", err)
	}
	return environment
}

func cloneEnvironmentOrPanic(env *Environment) *Environment {
	cloned, err := RehydrateEnvironment(EnvironmentSnapshot{
		Name:         env.Name(),
		Description:  env.Description(),
		DesiredState: env.DesiredState(),
		Status: &EnvironmentStatus{
			State:             env.Status().State,
			Message:           env.Status().Message,
			LastReconcileTime: env.Status().LastReconcileTime,
			LastSuccessTime:   env.Status().LastSuccessTime,
		},
		CreateTime: env.CreateTime(),
		UpdateTime: env.UpdateTime(),
		ETag:       env.ETag(),
	})
	if err != nil {
		panic(fmt.Sprintf("clone environment: %v", err))
	}
	return cloned
}
