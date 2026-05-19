package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dominion/projects/infra/deploy/domain"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	rolloutWaitTimeout  = 5 * time.Minute
	rolloutPollInterval = 5 * time.Second
)

func waitForRollout(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	deploymentNames []string,
	statefulSetNames []string,
	progress func(string),
) error {
	if len(deploymentNames) == 0 && len(statefulSetNames) == 0 {
		return nil
	}
	if client == nil {
		return fmt.Errorf("kubernetes client 为空")
	}

	rolloutCtx, cancel := context.WithTimeout(ctx, rolloutWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(rolloutPollInterval)
	defer ticker.Stop()

	for {
		allReady := true
		for _, deploymentName := range deploymentNames {
			dep, err := client.AppsV1().Deployments(namespace).Get(rolloutCtx, deploymentName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("获取 Deployment %s/%s 失败: %w", namespace, deploymentName, err)
			}

			if failed, reason := isDeploymentFailed(dep); failed {
				return fmt.Errorf("Deployment %s/%s 发布失败: %s", dep.Namespace, dep.Name, reason)
			}

			if isDeploymentReady(dep) {
				continue
			}

			allReady = false
			if progress != nil {
				progress(deploymentNotReadyMessage(dep))
			}
		}
		for _, statefulSetName := range statefulSetNames {
			sts, err := client.AppsV1().StatefulSets(namespace).Get(rolloutCtx, statefulSetName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("获取 StatefulSet %s/%s 失败: %w", namespace, statefulSetName, err)
			}

			if isStatefulSetReady(sts) {
				continue
			}

			allReady = false
			if progress != nil {
				progress(statefulSetNotReadyMessage(sts))
			}
		}
		if allReady {
			return nil
		}

		select {
		case <-rolloutCtx.Done():
			return fmt.Errorf("等待 Deployment rollout 失败: %w", rolloutCtx.Err())
		case <-ticker.C:
		}
	}
}

func isDeploymentReady(dep *appsv1.Deployment) bool {
	if dep == nil {
		return false
	}
	if dep.Status.ObservedGeneration < dep.Generation {
		return false
	}

	replicas := deploymentSpecReplicas(dep)
	if replicas == 0 {
		return true
	}
	if dep.Status.UpdatedReplicas != replicas {
		return false
	}
	if dep.Status.AvailableReplicas != replicas {
		return false
	}
	if dep.Status.UnavailableReplicas != 0 {
		return false
	}

	return true
}

func isDeploymentFailed(dep *appsv1.Deployment) (bool, string) {
	if dep == nil {
		return false, ""
	}

	for _, condition := range dep.Status.Conditions {
		switch {
		case condition.Type == appsv1.DeploymentProgressing && condition.Reason == "ProgressDeadlineExceeded":
			return true, deploymentFailureMessage(condition, "Deployment rollout 超过进度截止时间")
		case condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue:
			return true, deploymentFailureMessage(condition, "Deployment 副本创建失败")
		}
	}

	return false, ""
}

func deploymentNotReadyMessage(dep *appsv1.Deployment) string {
	if dep == nil {
		return "Deployment 为空"
	}
	if dep.Status.ObservedGeneration < dep.Generation {
		return fmt.Sprintf(
			"Deployment %s/%s 尚未观察到最新 generation（当前: %d，期望: %d）",
			dep.Namespace,
			dep.Name,
			dep.Status.ObservedGeneration,
			dep.Generation,
		)
	}

	replicas := deploymentSpecReplicas(dep)
	if replicas == 0 {
		return fmt.Sprintf("Deployment %s/%s 等待控制器观察到最新 generation", dep.Namespace, dep.Name)
	}
	if dep.Status.UpdatedReplicas != replicas {
		return fmt.Sprintf(
			"Deployment %s/%s 更新副本未完成（updated: %d/%d）",
			dep.Namespace,
			dep.Name,
			dep.Status.UpdatedReplicas,
			replicas,
		)
	}
	if dep.Status.AvailableReplicas != replicas {
		return fmt.Sprintf(
			"Deployment %s/%s 可用副本不足（available: %d/%d）",
			dep.Namespace,
			dep.Name,
			dep.Status.AvailableReplicas,
			replicas,
		)
	}
	if dep.Status.UnavailableReplicas != 0 {
		return fmt.Sprintf(
			"Deployment %s/%s 仍有不可用副本（unavailable: %d）",
			dep.Namespace,
			dep.Name,
			dep.Status.UnavailableReplicas,
		)
	}

	return fmt.Sprintf("Deployment %s/%s 尚未就绪", dep.Namespace, dep.Name)
}

func deploymentSpecReplicas(dep *appsv1.Deployment) int32 {
	if dep == nil || dep.Spec.Replicas == nil {
		return 1
	}

	return *dep.Spec.Replicas
}

func isStatefulSetReady(sts *appsv1.StatefulSet) bool {
	if sts == nil {
		return false
	}
	if sts.Status.ObservedGeneration < sts.Generation {
		return false
	}

	replicas := statefulSetSpecReplicas(sts)
	if replicas == 0 {
		return true
	}
	if sts.Status.ReadyReplicas != replicas {
		return false
	}

	return true
}

func statefulSetSpecReplicas(sts *appsv1.StatefulSet) int32 {
	if sts == nil || sts.Spec.Replicas == nil {
		return 1
	}

	return *sts.Spec.Replicas
}

func statefulSetNotReadyMessage(sts *appsv1.StatefulSet) string {
	if sts == nil {
		return "StatefulSet 为空"
	}
	if sts.Status.ObservedGeneration < sts.Generation {
		return fmt.Sprintf(
			"StatefulSet %s/%s 尚未观察到最新 generation（当前: %d，期望: %d）",
			sts.Namespace,
			sts.Name,
			sts.Status.ObservedGeneration,
			sts.Generation,
		)
	}

	replicas := statefulSetSpecReplicas(sts)
	if replicas == 0 {
		return fmt.Sprintf("StatefulSet %s/%s 等待控制器观察到最新 generation", sts.Namespace, sts.Name)
	}
	if sts.Status.ReadyReplicas != replicas {
		return fmt.Sprintf(
			"StatefulSet %s/%s 就绪副本不足（ready: %d/%d）",
			sts.Namespace,
			sts.Name,
			sts.Status.ReadyReplicas,
			replicas,
		)
	}

	return fmt.Sprintf("StatefulSet %s/%s 尚未就绪", sts.Namespace, sts.Name)
}

func deploymentFailureMessage(condition appsv1.DeploymentCondition, fallback string) string {
	if condition.Message != "" {
		return condition.Message
	}
	if condition.Reason != "" {
		return condition.Reason
	}

	return fallback
}

// CheckRollout queries Kubernetes for the rollout status of all workloads in the environment.
//
// It returns a tri-state result: Ready (all workloads rolled out), Waiting (still in progress),
// or Failed (a deployment has an explicit failure condition). StatefulSets can only be Ready or
// Waiting because they lack reliable failure signals.
func (r *K8sRuntime) CheckRollout(ctx context.Context, env *domain.Environment) (*domain.RolloutStatus, error) {
	objects, err := ConvertToWorkloads(env, r.client.K8sConfig)
	if err != nil {
		return nil, fmt.Errorf("转换 environment 为 workloads 失败: %w", err)
	}

	var deploymentNames []string
	for _, workload := range objects.Deployments {
		if workload == nil {
			continue
		}
		deploymentNames = append(deploymentNames, workload.WorkloadName())
	}
	for _, workload := range objects.MongoDBWorkloads {
		if workload == nil {
			continue
		}
		deploymentNames = append(deploymentNames, workload.ResourceName())
	}

	var statefulSetNames []string
	for _, workload := range objects.StatefulWorkloads {
		if workload == nil {
			continue
		}
		statefulSetNames = append(statefulSetNames, workload.WorkloadName())
	}

	if len(deploymentNames) == 0 && len(statefulSetNames) == 0 {
		return &domain.RolloutStatus{State: domain.RolloutReady}, nil
	}

	namespace := r.client.K8sConfig.Namespace

	var waitingMessages []string

	for _, name := range deploymentNames {
		dep, err := r.client.TypedClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("获取 Deployment %s/%s 失败: %w", namespace, name, err)
		}

		if failed, reason := isDeploymentFailed(dep); failed {
			return &domain.RolloutStatus{State: domain.RolloutFailed, Message: reason}, nil
		}

		if isDeploymentReady(dep) {
			continue
		}

		waitingMessages = append(waitingMessages, deploymentNotReadyMessage(dep))
	}

	for _, name := range statefulSetNames {
		sts, err := r.client.TypedClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("获取 StatefulSet %s/%s 失败: %w", namespace, name, err)
		}

		if isStatefulSetReady(sts) {
			continue
		}

		waitingMessages = append(waitingMessages, statefulSetNotReadyMessage(sts))
	}

	if len(waitingMessages) == 0 {
		return &domain.RolloutStatus{State: domain.RolloutReady}, nil
	}

	return &domain.RolloutStatus{
		State:   domain.RolloutWaiting,
		Message: strings.Join(waitingMessages, "; "),
	}, nil
}
