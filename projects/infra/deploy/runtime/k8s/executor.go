package k8s

import (
	"context"
	"fmt"

	"dominion/projects/infra/deploy/domain"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coretypedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	resourceKindDeployment = "Deployment"
	resourceKindService    = "Service"
	resourceKindHTTPRoute  = "HTTPRoute"
	resourceKindPVC        = "PersistentVolumeClaim"
	resourceKindSecret     = "Secret"
)

// K8sRuntime reconciles deploy environments into Kubernetes resources.
type K8sRuntime struct {
	client *RuntimeClient
}

// NewK8sRuntime creates a Kubernetes environment runtime.
func NewK8sRuntime(client *RuntimeClient) *K8sRuntime {
	return &K8sRuntime{client: client}
}

// Apply converts an environment into workloads and applies all owned resources.
func (r *K8sRuntime) Apply(ctx context.Context, env *domain.Environment) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("runtime client 为空")
	}
	if env == nil {
		return fmt.Errorf("environment 为空")
	}

	objects, err := ConvertToWorkloads(env, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("转换 environment 为 workloads 失败: %w", err)
	}

	for _, workload := range objects.Deployments {
		if err := r.applyDeployment(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.Deployments {
		if err := r.applyService(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.HTTPRoutes {
		if err := r.applyHTTPRoute(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.MongoDBWorkloads {
		if workload.Persistence.Enabled {
			if err := r.applyPVC(ctx, workload); err != nil {
				return err
			}
		}
		if err := r.applySecret(ctx, workload); err != nil {
			return err
		}
		if err := r.applyMongoDBDeployment(ctx, workload); err != nil {
			return err
		}
		if err := r.applyMongoDBService(ctx, workload); err != nil {
			return err
		}
	}

	return nil
}

// Delete removes all owned runtime resources for the target environment.
func (r *K8sRuntime) Delete(ctx context.Context, envName domain.EnvironmentName) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("runtime client 为空")
	}

	fullEnvName := envName.String()
	namespace := r.client.K8sConfig.Namespace
	matchLabels := buildLabels(
		withDominionEnvironment(fullEnvName),
		withManagedBy(r.client.K8sConfig.ManagedBy),
	)

	if err := r.deleteHTTPRoutes(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := r.deleteServices(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := r.deleteDeployments(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := r.deleteSecrets(ctx, namespace, matchLabels); err != nil {
		return err
	}

	return nil
}

func (r *K8sRuntime) applyDeployment(ctx context.Context, workload *DeploymentWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: deployment workload 为空", resourceKindDeployment)
	}

	desired, err := BuildDeployment(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindDeployment, workload.WorkloadName(), err)
	}

	return applyDeploymentResource(ctx, resourceKindDeployment, desired.Name,
		r.client.TypedClient.AppsV1().Deployments(desired.Namespace), desired)
}

func (r *K8sRuntime) applyService(ctx context.Context, workload *DeploymentWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: deployment workload 为空", resourceKindService)
	}

	desired, err := BuildService(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindService, workload.ServiceResourceName(), err)
	}

	return applyTypedService(ctx, desired.Name,
		r.client.TypedClient.CoreV1().Services(desired.Namespace), desired)
}

func (r *K8sRuntime) applyHTTPRoute(ctx context.Context, workload *HTTPRouteWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: http route workload 为空", resourceKindHTTPRoute)
	}

	desired, err := BuildHTTPRoute(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindHTTPRoute, workload.ResourceName(), err)
	}

	client := r.client.DynamicClient.Resource(httpRouteGVR()).Namespace(desired.GetNamespace())
	current, err := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := client.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("创建 %s %s/%s 失败: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
			}
			return nil
		}

		return fmt.Errorf("获取 %s %s/%s 失败: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
	}

	desired.SetResourceVersion(current.GetResourceVersion())
	if _, err := client.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 %s %s/%s 失败: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
	}

	return nil
}

func (r *K8sRuntime) applyPVC(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: mongo workload 为空", resourceKindPVC)
	}

	desired, err := BuildMongoDBPVC(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindPVC, workload.PVCResourceName(), err)
	}

	client := r.client.TypedClient.CoreV1().PersistentVolumeClaims(desired.Namespace)
	current, err := client.Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := client.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("创建 %s %s/%s 失败: %w", resourceKindPVC, desired.Namespace, desired.Name, err)
			}
			return nil
		}

		return fmt.Errorf("获取 %s %s/%s 失败: %w", resourceKindPVC, desired.Namespace, desired.Name, err)
	}

	if err := CheckPVCCompatibility(current, workload, r.client.K8sConfig); err != nil {
		return fmt.Errorf("校验 %s %s/%s 兼容性失败: %w", resourceKindPVC, desired.Namespace, desired.Name, err)
	}

	return nil
}

func (r *K8sRuntime) applySecret(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: mongo workload 为空", resourceKindSecret)
	}

	desired, err := BuildMongoDBSecret(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindSecret, workload.SecretResourceName(), err)
	}

	return applyTypedSecret(ctx, desired.Name,
		r.client.TypedClient.CoreV1().Secrets(desired.Namespace), desired)
}

func (r *K8sRuntime) applyMongoDBDeployment(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: mongo workload 为空", resourceKindDeployment)
	}

	desired, err := BuildMongoDBDeployment(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindDeployment, workload.ResourceName(), err)
	}

	return applyDeploymentResource(ctx, resourceKindDeployment, desired.Name,
		r.client.TypedClient.AppsV1().Deployments(desired.Namespace), desired)
}

func (r *K8sRuntime) applyMongoDBService(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: mongo workload 为空", resourceKindService)
	}

	desired, err := BuildMongoDBService(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindService, workload.ServiceResourceName(), err)
	}

	return applyTypedService(ctx, desired.Name,
		r.client.TypedClient.CoreV1().Services(desired.Namespace), desired)
}

func (r *K8sRuntime) deleteHTTPRoutes(ctx context.Context, namespace string, matchLabels labels.Set) error {
	client := r.client.DynamicClient.Resource(httpRouteGVR()).Namespace(namespace)
	routes, err := client.List(ctx, metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("列出 %s %s 失败: %w", resourceKindHTTPRoute, namespace, err)
	}

	for _, route := range routes.Items {
		if !hasAllLabels(route.GetLabels(), matchLabels) {
			continue
		}
		if err := client.Delete(ctx, route.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindHTTPRoute, namespace, route.GetName(), err)
		}
	}

	return nil
}

func (r *K8sRuntime) deleteServices(ctx context.Context, namespace string, matchLabels labels.Set) error {
	services, err := r.client.TypedClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("列出 %s %s 失败: %w", resourceKindService, namespace, err)
	}

	for _, service := range services.Items {
		if !hasAllLabels(service.Labels, matchLabels) {
			continue
		}
		if err := r.client.TypedClient.CoreV1().Services(namespace).Delete(ctx, service.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindService, namespace, service.Name, err)
		}
	}

	return nil
}

func (r *K8sRuntime) deleteDeployments(ctx context.Context, namespace string, matchLabels labels.Set) error {
	deployments, err := r.client.TypedClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("列出 %s %s 失败: %w", resourceKindDeployment, namespace, err)
	}

	for _, deployment := range deployments.Items {
		if !hasAllLabels(deployment.Labels, matchLabels) {
			continue
		}
		if err := r.client.TypedClient.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindDeployment, namespace, deployment.Name, err)
		}
	}

	return nil
}

func (r *K8sRuntime) deleteSecrets(ctx context.Context, namespace string, matchLabels labels.Set) error {
	secrets, err := r.client.TypedClient.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("列出 %s %s 失败: %w", resourceKindSecret, namespace, err)
	}

	for _, secret := range secrets.Items {
		if !hasAllLabels(secret.Labels, matchLabels) {
			continue
		}
		if err := r.client.TypedClient.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindSecret, namespace, secret.Name, err)
		}
	}

	return nil
}

func applyDeploymentResource(
	ctx context.Context,
	kind string,
	name string,
	client v1.DeploymentInterface,
	desired *appsv1.Deployment,
) error {
	current, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := client.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("创建 %s %s 失败: %w", kind, name, err)
			}
			return nil
		}

		return fmt.Errorf("获取 %s %s 失败: %w", kind, name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := client.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 %s %s 失败: %w", kind, name, err)
	}

	return nil
}

func applyTypedService(ctx context.Context, name string, client coretypedv1.ServiceInterface, desired *corev1.Service) error {
	current, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := client.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("创建 %s %s 失败: %w", resourceKindService, name, err)
			}
			return nil
		}

		return fmt.Errorf("获取 %s %s 失败: %w", resourceKindService, name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	desired.Spec.ClusterIP = current.Spec.ClusterIP
	desired.Spec.ClusterIPs = current.Spec.ClusterIPs
	desired.Spec.IPFamilies = current.Spec.IPFamilies
	desired.Spec.IPFamilyPolicy = current.Spec.IPFamilyPolicy
	desired.Spec.HealthCheckNodePort = current.Spec.HealthCheckNodePort
	desired.Spec.AllocateLoadBalancerNodePorts = current.Spec.AllocateLoadBalancerNodePorts
	if _, err := client.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 %s %s 失败: %w", resourceKindService, name, err)
	}

	return nil
}

func applyTypedSecret(ctx context.Context, name string, client coretypedv1.SecretInterface, desired *corev1.Secret) error {
	current, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := client.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("创建 %s %s 失败: %w", resourceKindSecret, name, err)
			}
			return nil
		}

		return fmt.Errorf("获取 %s %s 失败: %w", resourceKindSecret, name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := client.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 %s %s 失败: %w", resourceKindSecret, name, err)
	}

	return nil
}

func httpRouteGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
}
