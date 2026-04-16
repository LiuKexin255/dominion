package k8s

import (
	"context"
	"fmt"
	"time"

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
	progress func(string),
) error {
	if len(deploymentNames) == 0 {
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

func deploymentFailureMessage(condition appsv1.DeploymentCondition, fallback string) string {
	if condition.Message != "" {
		return condition.Message
	}
	if condition.Reason != "" {
		return condition.Reason
	}

	return fallback
}
