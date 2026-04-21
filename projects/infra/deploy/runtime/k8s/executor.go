package k8s

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"dominion/projects/infra/deploy/domain"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coretypedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	resourceKindDeployment  = "Deployment"
	resourceKindService     = "Service"
	resourceKindHTTPRoute   = "HTTPRoute"
	resourceKindPVC         = "PersistentVolumeClaim"
	resourceKindSecret      = "Secret"
	resourceKindStatefulSet = "StatefulSet"
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
func (r *K8sRuntime) Apply(ctx context.Context, env *domain.Environment, progress func(msg string)) error {
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
	for _, workload := range objects.StatefulWorkloads {
		if err := r.applyGoverningService(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.StatefulWorkloads {
		if err := r.applyStatefulSet(ctx, workload); err != nil {
			return err
		}
	}
	for _, workload := range objects.StatefulWorkloads {
		for i := 0; i < int(workload.Replicas); i++ {
			if err := r.applyPerInstanceService(ctx, workload, i); err != nil {
				return err
			}
		}
	}
	instanceRouteIdx := map[string]int{}
	for _, workload := range objects.InstanceRoutes {
		if workload == nil {
			continue
		}
		idx := instanceRouteIdx[workload.ServiceName]
		instanceRouteIdx[workload.ServiceName] = idx + 1
		if err := r.applyPerInstanceHTTPRoute(ctx, workload, idx); err != nil {
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
	if err := r.pruneResources(ctx, env.Name().Label(), objects); err != nil {
		return err
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
		return nil
	}

	if err := waitForRollout(
		ctx,
		r.client.TypedClient,
		r.client.K8sConfig.Namespace,
		deploymentNames,
		statefulSetNames,
		progress,
	); err != nil {
		return err
	}

	return nil
}

func (r *K8sRuntime) pruneResources(ctx context.Context, fullEnvName string, objects *DeployObjects) error {
	namespace := r.client.K8sConfig.Namespace
	matchLabels := buildLabels(
		withDominionEnvironment(fullEnvName),
		withManagedBy(r.client.K8sConfig.ManagedBy),
	)
	expected := buildExpectedApplyResources(objects)

	if err := r.pruneHTTPRoutes(ctx, namespace, matchLabels, expected.httpRoutes); err != nil {
		return err
	}
	if err := r.pruneServices(ctx, namespace, matchLabels, expected.services); err != nil {
		return err
	}
	if err := r.pruneStatefulSets(ctx, namespace, matchLabels, expected.statefulSets); err != nil {
		return err
	}
	if err := r.pruneDeployments(ctx, namespace, matchLabels, expected.deployments); err != nil {
		return err
	}
	if err := r.pruneSecrets(ctx, namespace, matchLabels, expected.secrets); err != nil {
		return err
	}

	return nil
}

type expectedApplyResources struct {
	deployments  map[string]struct{}
	services     map[string]struct{}
	httpRoutes   map[string]struct{}
	secrets      map[string]struct{}
	statefulSets map[string]struct{}
}

func buildExpectedApplyResources(objects *DeployObjects) *expectedApplyResources {
	resources := &expectedApplyResources{
		deployments:  make(map[string]struct{}),
		services:     make(map[string]struct{}),
		httpRoutes:   make(map[string]struct{}),
		secrets:      make(map[string]struct{}),
		statefulSets: make(map[string]struct{}),
	}
	if objects == nil {
		return resources
	}

	for _, workload := range objects.Deployments {
		if workload == nil {
			continue
		}
		resources.deployments[workload.WorkloadName()] = struct{}{}
		resources.services[workload.ServiceResourceName()] = struct{}{}
	}
	for _, workload := range objects.HTTPRoutes {
		if workload == nil {
			continue
		}
		resources.httpRoutes[workload.ResourceName()] = struct{}{}
	}
	for _, workload := range objects.MongoDBWorkloads {
		if workload == nil {
			continue
		}
		resources.deployments[workload.ResourceName()] = struct{}{}
		resources.services[workload.ServiceResourceName()] = struct{}{}
		resources.secrets[workload.SecretResourceName()] = struct{}{}
	}
	for _, workload := range objects.StatefulWorkloads {
		if workload == nil {
			continue
		}
		resources.statefulSets[workload.WorkloadName()] = struct{}{}
		resources.services[workload.ServiceResourceName()] = struct{}{}
		for i := 0; i < int(workload.Replicas); i++ {
			resources.services[newInstanceObjectName(WorkloadKindInstanceService, workload.EnvironmentName, workload.ServiceName, i)] = struct{}{}
		}
	}
	instanceRouteIdx := map[string]int{}
	for _, workload := range objects.InstanceRoutes {
		if workload == nil {
			continue
		}
		idx := instanceRouteIdx[workload.ServiceName]
		instanceRouteIdx[workload.ServiceName] = idx + 1
		resources.httpRoutes[newInstanceObjectName(WorkloadKindInstanceRoute, workload.EnvironmentName, workload.ServiceName, idx)] = struct{}{}
	}

	return resources
}

// Delete removes all owned runtime resources for the target environment.
func (r *K8sRuntime) Delete(ctx context.Context, envName domain.EnvironmentName) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("runtime client 为空")
	}

	fullEnvName := envName.Label()
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
	if err := r.deleteStatefulSets(ctx, namespace, matchLabels); err != nil {
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

// QueryServiceEndpoints queries Kubernetes Services and EndpointSlices
// to resolve service ports and endpoint addresses.
func (r *K8sRuntime) QueryServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*domain.ServiceQueryResult, error) {
	matchLabels := buildLabels(withApp(app), withService(service), withDominionEnvironment(envLabel))
	selector := buildLabelSelector(matchLabels)
	namespace := r.client.K8sConfig.Namespace

	services, err := r.client.TypedClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, domain.ErrServiceNotFound
	}
	if len(services.Items) != 1 {
		return nil, fmt.Errorf("expected exactly one Service, found %d matching labels %s", len(services.Items), selector)
	}

	svc := &services.Items[0]

	ports := make(map[string]int32)
	for _, port := range svc.Spec.Ports {
		if port.Name == "" {
			continue
		}
		ports[port.Name] = port.Port
	}

	if len(ports) == 0 {
		return nil, domain.ErrServicePortMapUnavailable
	}

	endpointSlices, err := r.listServiceEndpointSlices(ctx, namespace, svc.Name)
	if err != nil {
		return nil, err
	}

	return &domain.ServiceQueryResult{
		Ports:     ports,
		Endpoints: expandServiceEndpoints(endpointSlices.Items, ports),
	}, nil
}

// QueryStatefulServiceEndpoints queries governing and per-instance Services plus EndpointSlices
// to resolve stateful service ports and endpoint addresses.
func (r *K8sRuntime) QueryStatefulServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*domain.ServiceQueryResult, error) {
	matchLabels := buildLabels(withApp(app), withService(service), withDominionEnvironment(envLabel))
	selector := buildLabelSelector(matchLabels)
	namespace := r.client.K8sConfig.Namespace

	services, err := r.client.TypedClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, domain.ErrServiceNotFound
	}

	var governingSvc *corev1.Service
	var perInstanceSvcs []*corev1.Service
	for i := range services.Items {
		svc := &services.Items[i]
		if svc.Spec.ClusterIP == corev1.ClusterIPNone || svc.Spec.ClusterIP == "" {
			if governingSvc != nil {
				return nil, fmt.Errorf("expected exactly one governing Service, found multiple matching labels %s", selector)
			}
			governingSvc = svc
			continue
		}
		perInstanceSvcs = append(perInstanceSvcs, svc)
	}
	if governingSvc == nil {
		return nil, fmt.Errorf("expected exactly one governing Service, found none matching labels %s", selector)
	}

	ports := make(map[string]int32)
	for _, port := range governingSvc.Spec.Ports {
		if port.Name == "" {
			continue
		}
		ports[port.Name] = port.Port
	}

	if len(ports) == 0 {
		return nil, domain.ErrServicePortMapUnavailable
	}

	endpointSlices, err := r.listServiceEndpointSlices(ctx, namespace, governingSvc.Name)
	if err != nil {
		return nil, err
	}

	result := &domain.ServiceQueryResult{
		Ports:      ports,
		Endpoints:  expandServiceEndpoints(endpointSlices.Items, ports),
		IsStateful: true,
	}
	for _, svc := range perInstanceSvcs {
		podName, ok := svc.Spec.Selector[statefulSetPodNameLabelKey]
		if !ok {
			continue
		}
		instanceIndex, err := parseStatefulInstanceIndex(podName)
		if err != nil {
			return nil, fmt.Errorf("parse stateful instance index from service %s selector %q: %w", svc.Name, podName, err)
		}

		// TODO: batch EndpointSlice reads if stateful service queries become latency-sensitive.
		instanceSlices, err := r.listServiceEndpointSlices(ctx, namespace, svc.Name)
		if err != nil {
			return nil, err
		}

		instanceEndpoints := expandServiceEndpoints(instanceSlices.Items, ports)

		result.StatefulInstances = append(result.StatefulInstances, &domain.StatefulInstance{
			Index:     instanceIndex,
			Endpoints: instanceEndpoints,
		})
	}

	sort.Slice(result.StatefulInstances, func(i int, j int) bool {
		return result.StatefulInstances[i].Index < result.StatefulInstances[j].Index
	})

	return result, nil
}

// ReservedEnvironmentVariableNames returns environment variable names reserved by the Kubernetes runtime.
func (r *K8sRuntime) ReservedEnvironmentVariableNames(_ context.Context) ([]string, error) {
	return []string{
		reservedEnvNameServiceApp,
		reservedEnvNameDominionEnvironment,
		reservedEnvNamePodNamespace,
		envTLSCertFile,
		envTLSKeyFile,
		envTLSCAFile,
		envTLSDomain,
	}, nil
}

func (r *K8sRuntime) listServiceEndpointSlices(ctx context.Context, namespace string, serviceName string) (*discoveryv1.EndpointSliceList, error) {
	serviceSelector := labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: serviceName}).String()

	endpointSlices, err := r.client.TypedClient.DiscoveryV1().EndpointSlices(namespace).List(
		ctx,
		metav1.ListOptions{LabelSelector: serviceSelector},
	)
	if err != nil {
		return nil, fmt.Errorf("list endpoint slices: %w", err)
	}

	return endpointSlices, nil
}

func parseStatefulInstanceIndex(podName string) (int, error) {
	lastHyphen := strings.LastIndex(podName, "-")
	if lastHyphen < 0 || lastHyphen == len(podName)-1 {
		return 0, fmt.Errorf("missing ordinal suffix")
	}

	index, err := strconv.Atoi(podName[lastHyphen+1:])
	if err != nil {
		return 0, fmt.Errorf("invalid ordinal suffix: %w", err)
	}

	return index, nil
}

func expandServiceEndpoints(endpointSlices []discoveryv1.EndpointSlice, ports map[string]int32) []string {
	if len(endpointSlices) == 0 {
		return nil
	}

	addresses := make(map[string]struct{})
	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			if !includeEndpoint(endpoint) {
				continue
			}
			for _, ip := range endpoint.Addresses {
				for _, port := range ports {
					addresses[net.JoinHostPort(ip, strconv.Itoa(int(port)))] = struct{}{}
				}
			}
		}
	}

	if len(addresses) == 0 {
		return nil
	}

	result := make([]string, 0, len(addresses))
	for addr := range addresses {
		result = append(result, addr)
	}
	sort.Strings(result)

	return result
}

func includeEndpoint(endpoint discoveryv1.Endpoint) bool {
	if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
		return false
	}
	if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
		return false
	}
	return true
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

func (r *K8sRuntime) applyGoverningService(ctx context.Context, workload *StatefulWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: stateful workload 为空", resourceKindService)
	}

	desired, err := BuildGoverningService(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 governing %s %s 失败: %w", resourceKindService, workload.ServiceResourceName(), err)
	}

	return applyTypedService(ctx, desired.Name,
		r.client.TypedClient.CoreV1().Services(desired.Namespace), desired)
}

func (r *K8sRuntime) applyStatefulSet(ctx context.Context, workload *StatefulWorkload) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: stateful workload 为空", resourceKindStatefulSet)
	}

	desired, err := BuildStatefulSet(workload, r.client.K8sConfig)
	if err != nil {
		return fmt.Errorf("构建 %s %s 失败: %w", resourceKindStatefulSet, workload.WorkloadName(), err)
	}

	return applyStatefulSetResource(ctx, resourceKindStatefulSet, desired.Name,
		r.client.TypedClient.AppsV1().StatefulSets(desired.Namespace), desired)
}

func (r *K8sRuntime) applyPerInstanceService(ctx context.Context, workload *StatefulWorkload, instanceIndex int) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: stateful workload 为空", resourceKindService)
	}

	desired, err := BuildPerInstanceService(workload, r.client.K8sConfig, instanceIndex)
	if err != nil {
		return fmt.Errorf("构建 instance %s %s-%d 失败: %w", resourceKindService, workload.ServiceName, instanceIndex, err)
	}

	return applyTypedService(ctx, desired.Name,
		r.client.TypedClient.CoreV1().Services(desired.Namespace), desired)
}

func (r *K8sRuntime) applyPerInstanceHTTPRoute(ctx context.Context, workload *HTTPRouteWorkload, instanceIndex int) error {
	if workload == nil {
		return fmt.Errorf("failed to build %s <nil>: instance http route workload 为空", resourceKindHTTPRoute)
	}

	desired, err := BuildPerInstanceHTTPRoute(workload, r.client.K8sConfig, instanceIndex)
	if err != nil {
		return fmt.Errorf("构建 instance %s %s-%d 失败: %w", resourceKindHTTPRoute, workload.ServiceName, instanceIndex, err)
	}

	client := r.client.DynamicClient.Resource(httpRouteGVR()).Namespace(desired.GetNamespace())
	current, err := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := client.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("创建 instance %s %s/%s 失败: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
			}
			return nil
		}

		return fmt.Errorf("获取 instance %s %s/%s 失败: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
	}

	desired.SetResourceVersion(current.GetResourceVersion())
	if _, err := client.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 instance %s %s/%s 失败: %w", resourceKindHTTPRoute, desired.GetNamespace(), desired.GetName(), err)
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

func (r *K8sRuntime) pruneHTTPRoutes(ctx context.Context, namespace string, matchLabels labels.Set, expected map[string]struct{}) error {
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
		if _, ok := expected[route.GetName()]; ok {
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

func (r *K8sRuntime) pruneServices(ctx context.Context, namespace string, matchLabels labels.Set, expected map[string]struct{}) error {
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
		if _, ok := expected[service.Name]; ok {
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

func (r *K8sRuntime) deleteStatefulSets(ctx context.Context, namespace string, matchLabels labels.Set) error {
	statefulSets, err := r.client.TypedClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("列出 %s %s 失败: %w", resourceKindStatefulSet, namespace, err)
	}

	for _, sts := range statefulSets.Items {
		if !hasAllLabels(sts.Labels, matchLabels) {
			continue
		}
		if err := r.client.TypedClient.AppsV1().StatefulSets(namespace).Delete(ctx, sts.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindStatefulSet, namespace, sts.Name, err)
		}
	}

	return nil
}

func (r *K8sRuntime) pruneDeployments(ctx context.Context, namespace string, matchLabels labels.Set, expected map[string]struct{}) error {
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
		if _, ok := expected[deployment.Name]; ok {
			continue
		}
		if err := r.client.TypedClient.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindDeployment, namespace, deployment.Name, err)
		}
	}

	return nil
}

func (r *K8sRuntime) pruneStatefulSets(ctx context.Context, namespace string, matchLabels labels.Set, expected map[string]struct{}) error {
	statefulSets, err := r.client.TypedClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: buildLabelSelector(matchLabels)})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("列出 %s %s 失败: %w", resourceKindStatefulSet, namespace, err)
	}

	for _, sts := range statefulSets.Items {
		if !hasAllLabels(sts.Labels, matchLabels) {
			continue
		}
		if _, ok := expected[sts.Name]; ok {
			continue
		}
		if err := r.client.TypedClient.AppsV1().StatefulSets(namespace).Delete(ctx, sts.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除 %s %s/%s 失败: %w", resourceKindStatefulSet, namespace, sts.Name, err)
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

func (r *K8sRuntime) pruneSecrets(ctx context.Context, namespace string, matchLabels labels.Set, expected map[string]struct{}) error {
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
		if _, ok := expected[secret.Name]; ok {
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

func applyStatefulSetResource(
	ctx context.Context,
	kind string,
	name string,
	client v1.StatefulSetInterface,
	desired *appsv1.StatefulSet,
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
