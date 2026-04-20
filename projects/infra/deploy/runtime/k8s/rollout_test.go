package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func Test_isDeploymentReady(t *testing.T) {
	tests := []struct {
		name string
		dep  *appsv1.Deployment
		want bool
	}{
		{
			name: "all conditions met",
			dep: newRolloutTestDeployment(
				"ready",
				2,
				1,
				appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 2, AvailableReplicas: 2},
			),
			want: true,
		},
		{
			name: "replicas zero only requires generation observed",
			dep: newRolloutTestDeployment(
				"scaled-down",
				0,
				2,
				appsv1.DeploymentStatus{ObservedGeneration: 2},
			),
			want: true,
		},
		{
			name: "observed generation stale",
			dep: newRolloutTestDeployment(
				"stale-generation",
				2,
				3,
				appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 2, AvailableReplicas: 2},
			),
			want: false,
		},
		{
			name: "updated replicas insufficient",
			dep: newRolloutTestDeployment(
				"updating",
				3,
				1,
				appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 2, AvailableReplicas: 3},
			),
			want: false,
		},
		{
			name: "available replicas insufficient",
			dep: newRolloutTestDeployment(
				"unavailable",
				3,
				1,
				appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 3, AvailableReplicas: 2},
			),
			want: false,
		},
		{
			name: "unavailable replicas greater than zero",
			dep: newRolloutTestDeployment(
				"still-pending",
				3,
				1,
				appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 3, AvailableReplicas: 3, UnavailableReplicas: 1},
			),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDeploymentReady(tt.dep)
			if got != tt.want {
				t.Fatalf("isDeploymentReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isDeploymentFailed(t *testing.T) {
	tests := []struct {
		name       string
		dep        *appsv1.Deployment
		wantFailed bool
		wantReason string
	}{
		{
			name: "progress deadline exceeded",
			dep: newRolloutTestDeployment(
				"deadline",
				2,
				1,
				appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{{
					Type:    appsv1.DeploymentProgressing,
					Reason:  "ProgressDeadlineExceeded",
					Message: "deployment exceeded its progress deadline",
				}}},
			),
			wantFailed: true,
			wantReason: "deployment exceeded its progress deadline",
		},
		{
			name: "replica failure",
			dep: newRolloutTestDeployment(
				"replica-failure",
				2,
				1,
				appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{{
					Type:    appsv1.DeploymentReplicaFailure,
					Status:  corev1.ConditionTrue,
					Reason:  "FailedCreate",
					Message: "pods are forbidden",
				}}},
			),
			wantFailed: true,
			wantReason: "pods are forbidden",
		},
		{
			name: "no failure condition",
			dep: newRolloutTestDeployment(
				"healthy",
				2,
				1,
				appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "NewReplicaSetAvailable",
				}}},
			),
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFailed, gotReason := isDeploymentFailed(tt.dep)
			if gotFailed != tt.wantFailed {
				t.Fatalf("isDeploymentFailed() failed = %v, want %v", gotFailed, tt.wantFailed)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("isDeploymentFailed() reason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}

func Test_waitForRollout_AllReady(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestDeployment(t, client, newRolloutTestDeployment(
		"api",
		2,
		1,
		appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 2, AvailableReplicas: 2},
	))
	seedRolloutTestDeployment(t, client, newRolloutTestDeployment(
		"worker",
		1,
		1,
		appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	))

	err := waitForRollout(context.Background(), client, testRolloutNamespace, []string{"api", "worker"}, nil, nil)
	if err != nil {
		t.Fatalf("waitForRollout() failed: %v", err)
	}
}

func Test_waitForRollout_EmptyList(t *testing.T) {
	err := waitForRollout(context.Background(), kubernetesfake.NewSimpleClientset(), testRolloutNamespace, nil, nil, nil)
	if err != nil {
		t.Fatalf("waitForRollout() failed: %v", err)
	}
}

func Test_waitForRollout_DeploymentNotFound(t *testing.T) {
	err := waitForRollout(context.Background(), kubernetesfake.NewSimpleClientset(), testRolloutNamespace, []string{"missing"}, nil, nil)
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
	if !strings.Contains(err.Error(), "获取 Deployment") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("waitForRollout() error = %q, want deployment get failure", err)
	}
}

func Test_waitForRollout_DeploymentFailed(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestDeployment(t, client, newRolloutTestDeployment(
		"api",
		2,
		1,
		appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{{
			Type:    appsv1.DeploymentProgressing,
			Reason:  "ProgressDeadlineExceeded",
			Message: "deployment exceeded its progress deadline",
		}}},
	))

	err := waitForRollout(context.Background(), client, testRolloutNamespace, []string{"api"}, nil, nil)
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
	if !strings.Contains(err.Error(), "发布失败") || !strings.Contains(err.Error(), "progress deadline") {
		t.Fatalf("waitForRollout() error = %q, want rollout failure", err)
	}
}

func Test_waitForRollout_Timeout(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestDeployment(t, client, newRolloutTestDeployment(
		"api",
		2,
		1,
		appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 2, AvailableReplicas: 2, UnavailableReplicas: 1},
	))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := waitForRollout(ctx, client, testRolloutNamespace, []string{"api"}, nil, nil)
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("waitForRollout() error = %q, want timeout", err)
	}
}

func Test_waitForRollout_ContextCancelled(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestDeployment(t, client, newRolloutTestDeployment(
		"api",
		2,
		1,
		appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 2, AvailableReplicas: 2, UnavailableReplicas: 1},
	))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForRollout(ctx, client, testRolloutNamespace, []string{"api"}, nil, nil)
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("waitForRollout() error = %q, want canceled", err)
	}
}

func Test_waitForRollout_ProgressCallback(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestDeployment(t, client, newRolloutTestDeployment(
		"api",
		2,
		1,
		appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 2, AvailableReplicas: 2, UnavailableReplicas: 1},
	))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var messages []string
	err := waitForRollout(ctx, client, testRolloutNamespace, []string{"api"}, nil, func(message string) {
		messages = append(messages, message)
	})
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
	if len(messages) == 0 {
		t.Fatalf("progress callback was not called")
	}
	if !strings.Contains(messages[0], "不可用副本") {
		t.Fatalf("progress callback message = %q, want blocking reason", messages[0])
	}
}

func Test_isStatefulSetReady(t *testing.T) {
	tests := []struct {
		name string
		sts  *appsv1.StatefulSet
		want bool
	}{
		{
			name: "all conditions met",
			sts: newRolloutTestStatefulSet(
				"ready",
				3,
				1,
				appsv1.StatefulSetStatus{ObservedGeneration: 1, ReadyReplicas: 3},
			),
			want: true,
		},
		{
			name: "replicas zero only requires generation observed",
			sts: newRolloutTestStatefulSet(
				"scaled-down",
				0,
				2,
				appsv1.StatefulSetStatus{ObservedGeneration: 2},
			),
			want: true,
		},
		{
			name: "observed generation stale",
			sts: newRolloutTestStatefulSet(
				"stale-generation",
				3,
				2,
				appsv1.StatefulSetStatus{ObservedGeneration: 1, ReadyReplicas: 3},
			),
			want: false,
		},
		{
			name: "ready replicas insufficient",
			sts: newRolloutTestStatefulSet(
				"not-ready",
				3,
				1,
				appsv1.StatefulSetStatus{ObservedGeneration: 1, ReadyReplicas: 2},
			),
			want: false,
		},
		{
			name: "nil statefulset",
			sts:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStatefulSetReady(tt.sts)
			if got != tt.want {
				t.Fatalf("isStatefulSetReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_waitForRollout_StatefulSetReady(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestStatefulSet(t, client, newRolloutTestStatefulSet(
		"cache",
		3,
		1,
		appsv1.StatefulSetStatus{ObservedGeneration: 1, ReadyReplicas: 3},
	))

	err := waitForRollout(context.Background(), client, testRolloutNamespace, nil, []string{"cache"}, nil)
	if err != nil {
		t.Fatalf("waitForRollout() failed: %v", err)
	}
}

func Test_waitForRollout_StatefulSetNotFound(t *testing.T) {
	err := waitForRollout(context.Background(), kubernetesfake.NewSimpleClientset(), testRolloutNamespace, nil, []string{"missing"}, nil)
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
	if !strings.Contains(err.Error(), "获取 StatefulSet") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("waitForRollout() error = %q, want statefulset get failure", err)
	}
}

func Test_waitForRollout_StatefulSetNotReady(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedRolloutTestStatefulSet(t, client, newRolloutTestStatefulSet(
		"cache",
		3,
		1,
		appsv1.StatefulSetStatus{ObservedGeneration: 1, ReadyReplicas: 2},
	))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := waitForRollout(ctx, client, testRolloutNamespace, nil, []string{"cache"}, nil)
	if err == nil {
		t.Fatalf("waitForRollout() expected error")
	}
}

func newRolloutTestStatefulSet(name string, replicas int32, generation int64, status appsv1.StatefulSetStatus) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testRolloutNamespace,
			Generation: generation,
		},
		Spec:   appsv1.StatefulSetSpec{},
		Status: status,
	}
	sts.Spec.Replicas = &replicas

	return sts
}

func seedRolloutTestStatefulSet(t *testing.T, client *kubernetesfake.Clientset, sts *appsv1.StatefulSet) {
	t.Helper()
	if err := client.Tracker().Add(sts); err != nil {
		t.Fatalf("seed statefulset failed: %v", err)
	}
}

const testRolloutNamespace = "test-ns"

func newRolloutTestDeployment(name string, replicas int32, generation int64, status appsv1.DeploymentStatus) *appsv1.Deployment {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testRolloutNamespace,
			Generation: generation,
		},
		Spec: appsv1.DeploymentSpec{},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration:  status.ObservedGeneration,
			Replicas:            status.Replicas,
			UpdatedReplicas:     status.UpdatedReplicas,
			ReadyReplicas:       status.ReadyReplicas,
			AvailableReplicas:   status.AvailableReplicas,
			UnavailableReplicas: status.UnavailableReplicas,
			Conditions:          status.Conditions,
		},
	}
	dep.Spec.Replicas = &replicas

	return dep
}

func seedRolloutTestDeployment(t *testing.T, client *kubernetesfake.Clientset, dep *appsv1.Deployment) {
	t.Helper()
	if err := client.Tracker().Add(dep); err != nil {
		t.Fatalf("seed deployment failed: %v", err)
	}
}
