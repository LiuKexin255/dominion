package k8s

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Executor 在 Kubernetes 中执行部署对象的应用与删除。
type Executor struct {
	client *RuntimeClient
}

// NewExecutor 创建一个 Executor。
func NewExecutor(client *RuntimeClient) *Executor {
	return &Executor{client: client}
}

// Apply 将部署对象应用到集群。
func (e *Executor) Apply(ctx context.Context, objects *DeployObjects) error {
	if e == nil || e.client == nil {
		return fmt.Errorf("runtime client 为空")
	}
	if objects == nil {
		return fmt.Errorf("deploy objects 为空")
	}

	for _, workload := range objects.Deployments {
		if err := e.applyDeployment(ctx, workload); err != nil {
			return err
		}
		if err := e.applyService(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.HTTPRoutes {
		if err := e.applyHTTPRoute(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.MongoDBWorkloads {
		if workload.Persistence.Enabled {
			if err := e.applyPVC(ctx, workload); err != nil {
				return err
			}
		}
		if err := e.applySecret(ctx, workload); err != nil {
			return err
		}
		if err := e.applyMongoDBDeployment(ctx, workload); err != nil {
			return err
		}
		if err := e.applyMongoDBService(ctx, workload); err != nil {
			return err
		}
	}

	return nil
}

// Delete 删除指定 app 和 environment 下的资源。
func (e *Executor) Delete(ctx context.Context, app, environment string) error {
	if e == nil || e.client == nil {
		return fmt.Errorf("runtime client 为空")
	}

	namespace := e.client.K8sConfig.Namespace
	matchLabels := buildLabels(
		withDominionApp(app),
		withDominionEnvironment(environment),
		withManagedBy(e.client.K8sConfig.ManagedBy),
	)

	if err := e.deleteHTTPRoutes(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := e.deleteServices(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := e.deleteDeployments(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := e.deleteMongoDBServices(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := e.deleteMongoDBDeployments(ctx, namespace, matchLabels); err != nil {
		return err
	}
	if err := e.deleteSecrets(ctx, namespace, matchLabels); err != nil {
		return err
	}

	return nil
}

func (e *Executor) deleteMongoDBDeployments(ctx context.Context, namespace string, matchLabels labels.Set) error {
	listOptions := metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)}
	deleteOptions := metav1.DeleteOptions{}

	deployments, err := e.client.TypedClient.AppsV1().Deployments(namespace).List(ctx, listOptions)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list %s in %s: %w", resourceKindDeployment, namespace, err)
		}
		return nil
	}

	for _, deployment := range deployments.Items {
		if !hasAllLabels(deployment.Labels, matchLabels) {
			continue
		}
		if err := e.client.TypedClient.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, deleteOptions); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s %s/%s: %w", resourceKindDeployment, namespace, deployment.Name, err)
			}
		}
	}

	return nil
}

func (e *Executor) deleteMongoDBServices(ctx context.Context, namespace string, matchLabels labels.Set) error {
	listOptions := metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)}
	deleteOptions := metav1.DeleteOptions{}

	services, err := e.client.TypedClient.CoreV1().Services(namespace).List(ctx, listOptions)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list %s in %s: %w", resourceKindService, namespace, err)
		}
		return nil
	}

	for _, service := range services.Items {
		if !hasAllLabels(service.Labels, matchLabels) {
			continue
		}
		if err := e.client.TypedClient.CoreV1().Services(namespace).Delete(ctx, service.Name, deleteOptions); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s %s/%s: %w", resourceKindService, namespace, service.Name, err)
			}
		}
	}

	return nil
}

func (e *Executor) deleteSecrets(ctx context.Context, namespace string, matchLabels labels.Set) error {
	listOptions := metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)}
	deleteOptions := metav1.DeleteOptions{}

	secrets, err := e.client.TypedClient.CoreV1().Secrets(namespace).List(ctx, listOptions)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list %s in %s: %w", resourceKindSecret, namespace, err)
		}
		return nil
	}

	for _, secret := range secrets.Items {
		if !hasAllLabels(secret.Labels, matchLabels) {
			continue
		}
		if err := e.client.TypedClient.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, deleteOptions); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s %s/%s: %w", resourceKindSecret, namespace, secret.Name, err)
			}
		}
	}

	return nil
}

func (e *Executor) deleteDeployments(ctx context.Context, namespace string, matchLabels labels.Set) error {
	listOptions := metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)}
	deleteOptions := metav1.DeleteOptions{}

	deployments, err := e.client.TypedClient.AppsV1().Deployments(namespace).List(ctx, listOptions)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list %s in %s: %w", resourceKindDeployment, namespace, err)
		}
		return nil
	}

	for _, deployment := range deployments.Items {
		if !hasAllLabels(deployment.Labels, matchLabels) {
			continue
		}
		if err := e.client.TypedClient.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, deleteOptions); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s %s/%s: %w", resourceKindDeployment, namespace, deployment.Name, err)
			}
		}
	}

	return nil
}

func (e *Executor) deleteServices(ctx context.Context, namespace string, matchLabels labels.Set) error {
	listOptions := metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)}
	deleteOptions := metav1.DeleteOptions{}

	services, err := e.client.TypedClient.CoreV1().Services(namespace).List(ctx, listOptions)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list %s in %s: %w", resourceKindService, namespace, err)
		}
		return nil
	}

	for _, service := range services.Items {
		if !hasAllLabels(service.Labels, matchLabels) {
			continue
		}
		if err := e.client.TypedClient.CoreV1().Services(namespace).Delete(ctx, service.Name, deleteOptions); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s %s/%s: %w", resourceKindService, namespace, service.Name, err)
			}
		}
	}

	return nil
}

func (e *Executor) deleteHTTPRoutes(ctx context.Context, namespace string, matchLabels labels.Set) error {
	listOptions := metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)}
	deleteOptions := metav1.DeleteOptions{}

	routes, err := e.client.DynamicClient.Resource(httpRouteGVR()).Namespace(namespace).List(ctx, listOptions)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list %s in %s: %w", resourceKindHTTPRoute, namespace, err)
		}
		return nil
	}

	for _, route := range routes.Items {
		if !hasAllLabels(route.GetLabels(), matchLabels) {
			continue
		}
		if err := e.client.DynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Delete(ctx, route.GetName(), deleteOptions); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s %s/%s: %w", resourceKindHTTPRoute, namespace, route.GetName(), err)
			}
		}
	}

	return nil
}

func hasAllLabels(current map[string]string, want labels.Set) bool {
	for key, value := range want {
		if current[key] != value {
			return false
		}
	}

	return true
}

func buildLabelSelector(matchLabels labels.Set) string {
	selectorLabels := labels.Set{}
	for key, value := range matchLabels {
		if !isValidLabelValue(value) {
			continue
		}
		selectorLabels[key] = value
	}

	return selectorLabels.String()
}

func isValidLabelValue(value string) bool {
	if value == "" {
		return true
	}

	for i, r := range value {
		if !isValidLabelValueChar(r) {
			return false
		}
		if (i == 0 || i == len(value)-1) && !isASCIIAlphaNumeric(r) {
			return false
		}
	}

	return true
}

func isValidLabelValueChar(r rune) bool {
	return isASCIIAlphaNumeric(r) || r == '-' || r == '_' || r == '.'
}

func isASCIIAlphaNumeric(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

func (e *Executor) applyDeployment(ctx context.Context, workload *DeploymentWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindDeployment, fmt.Errorf("deployment workload 为空"))
	}

	desired, err := BuildDeployment(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindDeployment, workload.WorkloadName(), err)
	}

	current, err := e.client.TypedClient.AppsV1().Deployments(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.TypedClient.AppsV1().Deployments(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindDeployment, desired.Namespace, desired.Name, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindDeployment, desired.Namespace, desired.Name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := e.client.TypedClient.AppsV1().Deployments(desired.Namespace).Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s %s/%s: %w", resourceKindDeployment, desired.Namespace, desired.Name, err)
	}
	return nil
}

func (e *Executor) applyService(ctx context.Context, workload *DeploymentWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindService, fmt.Errorf("deployment workload 为空"))
	}

	desired, err := BuildService(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindService, workload.ServiceResourceName(), err)
	}

	current, err := e.client.TypedClient.CoreV1().Services(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.TypedClient.CoreV1().Services(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindService, desired.Namespace, desired.Name, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindService, desired.Namespace, desired.Name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := e.client.TypedClient.CoreV1().Services(desired.Namespace).Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s %s/%s: %w", resourceKindService, desired.Namespace, desired.Name, err)
	}
	return nil
}

func (e *Executor) applyHTTPRoute(ctx context.Context, workload *HTTPRouteWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindHTTPRoute, fmt.Errorf("httproute workload 为空"))
	}

	desired, err := BuildHTTPRoute(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindHTTPRoute, workload.ResourceName(), err)
	}

	current, err := e.client.DynamicClient.Resource(httpRouteGVR()).Namespace(desired.GetNamespace()).Get(ctx, desired.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.DynamicClient.Resource(httpRouteGVR()).Namespace(desired.GetNamespace()).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
	}

	desired.SetResourceVersion(current.GetResourceVersion())
	if _, err := e.client.DynamicClient.Resource(httpRouteGVR()).Namespace(desired.GetNamespace()).Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s %s/%s: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
	}
	return nil
}

func (e *Executor) applyPVC(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindPVC, fmt.Errorf("pvc workload 为空"))
	}

	desired, err := BuildMongoDBPVC(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindPVC, workload.PVCResourceName(), err)
	}

	current, err := e.client.TypedClient.CoreV1().PersistentVolumeClaims(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.TypedClient.CoreV1().PersistentVolumeClaims(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindPVC, desired.Namespace, desired.Name, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindPVC, desired.Namespace, desired.Name, err)
	}

	if err := CheckPVCCompatibility(current, workload); err != nil {
		return fmt.Errorf("failed to validate %s %s/%s: %w", resourceKindPVC, desired.Namespace, desired.Name, err)
	}

	fmt.Println("pvc already exist, skip apply")
	return nil
}

func (e *Executor) applySecret(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindSecret, fmt.Errorf("mongo workload 为空"))
	}

	desired, err := BuildMongoDBSecret(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindSecret, workload.SecretResourceName(), err)
	}

	current, err := e.client.TypedClient.CoreV1().Secrets(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.TypedClient.CoreV1().Secrets(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindSecret, desired.Namespace, desired.Name, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindSecret, desired.Namespace, desired.Name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := e.client.TypedClient.CoreV1().Secrets(desired.Namespace).Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s %s/%s: %w", resourceKindSecret, desired.Namespace, desired.Name, err)
	}
	return nil
}

func (e *Executor) applyMongoDBDeployment(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindDeployment, fmt.Errorf("mongo workload 为空"))
	}

	desired, err := BuildMongoDBDeployment(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindDeployment, workload.ResourceName(), err)
	}

	current, err := e.client.TypedClient.AppsV1().Deployments(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.TypedClient.AppsV1().Deployments(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindDeployment, desired.Namespace, desired.Name, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindDeployment, desired.Namespace, desired.Name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := e.client.TypedClient.AppsV1().Deployments(desired.Namespace).Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s %s/%s: %w", resourceKindDeployment, desired.Namespace, desired.Name, err)
	}
	return nil
}

func (e *Executor) applyMongoDBService(ctx context.Context, workload *MongoDBWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindService, fmt.Errorf("mongo workload 为空"))
	}

	desired, err := BuildMongoDBService(workload)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindService, workload.ServiceResourceName(), err)
	}

	current, err := e.client.TypedClient.CoreV1().Services(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := e.client.TypedClient.CoreV1().Services(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s %s/%s: %w", resourceKindService, desired.Namespace, desired.Name, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s: %w", resourceKindService, desired.Namespace, desired.Name, err)
	}

	desired.ResourceVersion = current.ResourceVersion
	if _, err := e.client.TypedClient.CoreV1().Services(desired.Namespace).Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s %s/%s: %w", resourceKindService, desired.Namespace, desired.Name, err)
	}
	return nil
}
