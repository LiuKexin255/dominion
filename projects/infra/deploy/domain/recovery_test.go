package domain

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recoveryRepoFake struct {
	called bool
	envs   []*Environment
	err    error
}

func (r *recoveryRepoFake) ListNeedingReconcile(ctx context.Context) ([]*Environment, error) {
	r.called = true
	if r.err != nil {
		return nil, r.err
	}
	return r.envs, nil
}

type recoveryQueueCall struct {
	envName EnvironmentName
}

type recoveryQueueFake struct {
	calls []recoveryQueueCall
	err   error
}

func (q *recoveryQueueFake) Enqueue(ctx context.Context, envName EnvironmentName) error {
	q.calls = append(q.calls, recoveryQueueCall{envName: envName})
	return q.err
}

func TestRecover_RequeuesAllNeedingReconcileEnvironments(t *testing.T) {
	ctx := context.Background()

	reconcilingName, err := NewEnvironmentName("scope1", "recon")
	if err != nil {
		t.Fatalf("NewEnvironmentName reconciling failed: %v", err)
	}
	deletingName, err := NewEnvironmentName("scope1", "delete")
	if err != nil {
		t.Fatalf("NewEnvironmentName deleting failed: %v", err)
	}

	reconcilingEnv, err := NewEnvironment(reconcilingName, EnvironmentTypeProd, "", &DesiredState{})
	if err != nil {
		t.Fatalf("NewEnvironment reconciling failed: %v", err)
	}
	if err := reconcilingEnv.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling failed: %v", err)
	}

	deletingEnv, err := NewEnvironment(deletingName, EnvironmentTypeProd, "", &DesiredState{})
	if err != nil {
		t.Fatalf("NewEnvironment deleting failed: %v", err)
	}
	if err := deletingEnv.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting failed: %v", err)
	}

	repo := &recoveryRepoFake{envs: []*Environment{reconcilingEnv, deletingEnv}}
	queue := &recoveryQueueFake{}

	if err := Recover(ctx, repo, queue); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if !repo.called {
		t.Fatal("ListNeedingReconcile was not called")
	}

	if got, want := queue.calls, []recoveryQueueCall{
		{envName: reconcilingName},
		{envName: deletingName},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queue calls = %#v, want %#v", got, want)
	}

	if got, want := reconcilingEnv.Status().State, StateReconciling; got != want {
		t.Fatalf("reconciling state = %v, want %v", got, want)
	}
	if got, want := deletingEnv.Status().State, StateDeleting; got != want {
		t.Fatalf("deleting state = %v, want %v", got, want)
	}
}

func TestRecover_ReturnsListNeedingReconcileError(t *testing.T) {
	repo := &recoveryRepoFake{err: errors.New("boom")}
	queue := &recoveryQueueFake{}

	if err := Recover(context.Background(), repo, queue); err == nil {
		t.Fatal("Recover returned nil error, want failure")
	}

	if len(queue.calls) != 0 {
		t.Fatalf("queue was called %d times, want 0", len(queue.calls))
	}
}
