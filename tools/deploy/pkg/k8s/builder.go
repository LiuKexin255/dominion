// Package k8s provides helpers for building Kubernetes deployment objects.
package k8s

import (
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// managedByLabelKey 标识由部署工具托管的资源标签键。
	managedByLabelKey = "app.kubernetes.io/managed-by"
	// appLabelKey 标识应用名称标签键。
	appLabelKey = "app.kubernetes.io/name"
	// serviceLabelKey 标识服务名称标签键。
	serviceLabelKey = "app.kubernetes.io/component"
	// environmentLabelKey 标识环境名称标签键。
	environmentLabelKey = "app.kubernetes.io/instance"

	// httpRouteKind 是 Gateway API HTTPRoute 资源类型。
	httpRouteKind = "HTTPRoute"
)

// BuildDeployment 将 deployment workload 构造成可直接下发的 Deployment 对象。
func BuildDeployment(workload *DeploymentWorkload, k8sConfig *K8sConfig) (*appsv1.Deployment, error) {
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withEnvironment(workload.EnvironmentName),
	)
	ports, err := buildContainerPorts(workload.Ports)
	if err != nil {
		return nil, fmt.Errorf("构建 deployment ports 失败: %w", err)
	}

	replicas := workload.Replicas

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.WorkloadName(),
			Namespace: k8sConfig.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string(selectorLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string(objectLabels),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  workload.WorkloadName(),
						Image: workload.Image,
						Ports: ports,
					}},
				},
			},
		},
	}, nil
}

// BuildService 将 service workload 构造成可直接下发的 Service 对象。
func BuildService(workload *ServiceWorkload, k8sConfig *K8sConfig) (*corev1.Service, error) {
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withEnvironment(workload.EnvironmentName),
	)
	ports, err := buildServicePorts(workload.Ports)
	if err != nil {
		return nil, fmt.Errorf("构建 service ports 失败: %w", err)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.ResourceName(),
			Namespace: k8sConfig.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string(selectorLabels),
			Ports:    ports,
		},
	}, nil
}

// BuildHTTPRoute 将 HTTPRoute workload 构造成可直接下发的动态对象。
func BuildHTTPRoute(workload *HTTPRouteWorkload, k8sConfig *K8sConfig) (*unstructured.Unstructured, error) {
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	var hostnames []gatewayv1.Hostname
	for _, hostname := range workload.Hostnames {
		hostnames = append(hostnames, gatewayv1.Hostname(hostname))
	}

	gatewayNamespace := gatewayv1.Namespace(workload.GatewayNamespace)
	var rules []gatewayv1.HTTPRouteRule
	for _, match := range workload.Matches {
		pathType := gatewayv1.PathMatchType(match.Type)
		pathValue := match.Value
		backendName := gatewayv1.ObjectName(workload.BackendService)
		backendPort := gatewayv1.PortNumber(match.BackendPort)

		rules = append(rules, gatewayv1.HTTPRouteRule{
			Matches: []gatewayv1.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathType,
					Value: &pathValue,
				},
			}},
			BackendRefs: []gatewayv1.HTTPBackendRef{{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: backendName,
						Port: &backendPort,
					},
				},
			}},
		})
	}

	typedRoute := &gatewayv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayv1.GroupVersion.String(),
			Kind:       httpRouteKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.ResourceName(),
			Namespace: k8sConfig.Namespace,
			Labels:    map[string]string(objectLabels),
		},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: hostnames,
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name:      gatewayv1.ObjectName(workload.GatewayName),
					Namespace: &gatewayNamespace,
				}},
			},
			Rules: rules,
		},
	}

	rawBytes, err := json.Marshal(typedRoute)
	if err != nil {
		return nil, fmt.Errorf("序列化 http route 失败: %w", err)
	}

	rawMap := make(map[string]any)
	if err := json.Unmarshal(rawBytes, &rawMap); err != nil {
		return nil, fmt.Errorf("反序列化 http route 失败: %w", err)
	}

	route := &unstructured.Unstructured{Object: rawMap}

	return route, nil
}

// labelOption 定义标签构建选项函数类型。
type labelOption func(*labelSet)

// labelSet 聚合标签构建过程中的中间状态。
type labelSet struct {
	app         string
	service     string
	environment string
	managedBy   string
}

func withApp(name string) labelOption {
	return func(set *labelSet) {
		set.app = strings.TrimSpace(name)
	}
}

func withService(name string) labelOption {
	return func(set *labelSet) {
		set.service = strings.TrimSpace(name)
	}
}

func withEnvironment(name string) labelOption {
	return func(set *labelSet) {
		set.environment = strings.TrimSpace(name)
	}
}

func withManagedBy(name string) labelOption {
	return func(set *labelSet) {
		set.managedBy = strings.TrimSpace(name)
	}
}

func buildLabels(options ...labelOption) labels.Set {
	set := &labelSet{}
	for _, option := range options {
		if option != nil {
			option(set)
		}
	}

	result := labels.Set{}
	if set.app != "" {
		result[appLabelKey] = set.app
	}
	if set.service != "" {
		result[serviceLabelKey] = set.service
	}
	if set.environment != "" {
		result[environmentLabelKey] = set.environment
	}
	if set.managedBy != "" {
		result[managedByLabelKey] = set.managedBy
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func buildContainerPorts(ports []*DeploymentPort) ([]corev1.ContainerPort, error) {
	if len(ports) == 0 {
		return nil, nil
	}

	containerPorts := make([]corev1.ContainerPort, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			return nil, fmt.Errorf("端口为空")
		}

		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          port.Name,
			ContainerPort: int32(port.Port),
		})
	}

	return containerPorts, nil
}

func buildServicePorts(ports []*DeploymentPort) ([]corev1.ServicePort, error) {
	if len(ports) == 0 {
		return nil, nil
	}

	servicePorts := make([]corev1.ServicePort, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			return nil, fmt.Errorf("端口为空")
		}

		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       port.Name,
			Port:       int32(port.Port),
			TargetPort: intstr.FromString(port.Name),
		})
	}

	return servicePorts, nil
}

func toAnySlice(values []string) []any {
	if len(values) == 0 {
		return nil
	}

	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}

	return items
}
