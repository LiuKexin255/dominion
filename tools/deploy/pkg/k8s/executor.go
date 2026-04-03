package k8s

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Executor struct {
	client *RuntimeClient
}

func NewExecutor(client *RuntimeClient) *Executor {
	return &Executor{client: client}
}

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
	}
	for _, workload := range objects.Services {
		if err := e.applyService(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.HTTPRoutes {
		if err := e.applyHTTPRoute(ctx, workload); err != nil {
			return err
		}
	}

	return nil
}

func (e *Executor) applyDeployment(ctx context.Context, workload *DeploymentWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindDeployment, fmt.Errorf("deployment workload 为空"))
	}

	desired, err := BuildDeployment(workload, e.client.K8sConfig)
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

func (e *Executor) applyService(ctx context.Context, workload *ServiceWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to get %s <nil>: %w", resourceKindService, fmt.Errorf("service workload 为空"))
	}

	desired, err := BuildService(workload, e.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("failed to build %s %s: %w", resourceKindService, workload.ResourceName(), err)
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

	desired, err := BuildHTTPRoute(workload, e.client.K8sConfig)
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
