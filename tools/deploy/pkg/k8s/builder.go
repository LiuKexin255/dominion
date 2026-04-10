// Package k8s provides helpers for building Kubernetes deployment objects.
package k8s

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	// dominionAppLabelKey 标识 Dominion 应用名称标签键。
	dominionAppLabelKey = "dominion.io/app"
	// dominionEnvironmentLabelKey 标识 Dominion 环境名称标签键。
	dominionEnvironmentLabelKey = "dominion.io/environment"
	// reservedEnvNameDominionApp 为 Dominion app 注入环境变量名。
	reservedEnvNameDominionApp = "DOMINION_APP"
	// reservedEnvNameDominionEnvironment 为 Dominion 环境注入环境变量名。
	reservedEnvNameDominionEnvironment = "DOMINION_ENVIRONMENT"
	// reservedEnvNamePodNamespace 为 Pod 命名空间注入环境变量名。
	reservedEnvNamePodNamespace = "POD_NAMESPACE"
	// tlsVolumeName 为 TLS projected volume 固定名称。
	tlsVolumeName = "tls"
	// tlsMountPath 为 TLS 文件固定挂载目录。
	tlsMountPath = "/etc/tls"
	// tlsCAFileName 为容器内固定的 CA 文件名。
	tlsCAFileName = "ca.crt"
	// tlsCertFileName 为容器内固定的证书文件名。
	tlsCertFileName = "tls.crt"
	// tlsKeyFileName 为容器内固定的私钥文件名。
	tlsKeyFileName = "tls.key"
	// envTLSCertFile 为 TLS 证书文件环境变量名。
	envTLSCertFile = "TLS_CERT_FILE"
	// envTLSKeyFile 为 TLS 私钥文件环境变量名。
	envTLSKeyFile = "TLS_KEY_FILE"
	// envTLSCAFile 为 TLS CA 文件环境变量名。
	envTLSCAFile = "TLS_CA_FILE"
	// envTLSDomain 为 TLS 服务名环境变量名。
	envTLSDomain = "TLS_SERVER_NAME"

	// httpRouteKind 是 Gateway API HTTPRoute 资源类型。
	httpRouteKind = "HTTPRoute"
)

// BuildDeployment 将 deployment workload 构造成可直接下发的 Deployment 对象。
func BuildDeployment(workload *DeploymentWorkload) (*appsv1.Deployment, error) {
	k8sConfig := LoadK8sConfig()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
	)
	ports, err := buildContainerPorts(workload.Ports)
	if err != nil {
		return nil, fmt.Errorf("构建 deployment ports 失败: %w", err)
	}

	replicas := workload.Replicas
	containerEnv := []corev1.EnvVar{
		{Name: reservedEnvNameDominionApp, Value: workload.DominionApp},
		{Name: reservedEnvNameDominionEnvironment, Value: workload.EnvironmentName},
		{Name: reservedEnvNamePodNamespace, Value: k8sConfig.Namespace},
	}
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	if workload.TLSEnabled {
		volumes = []corev1.Volume{{
			Name: tlsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: k8sConfig.TLS.Secret}}},
						{ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: k8sConfig.TLS.CAConfigMap.Name},
							Items: []corev1.KeyToPath{{
								Key:  k8sConfig.TLS.CAConfigMap.Key,
								Path: tlsCAFileName,
							}},
						}},
					},
				},
			},
		}}
		volumeMounts = []corev1.VolumeMount{{
			Name:      tlsVolumeName,
			MountPath: tlsMountPath,
			ReadOnly:  true,
		}}
		containerEnv = append(containerEnv,
			corev1.EnvVar{Name: envTLSCertFile, Value: filepath.Join(tlsMountPath, tlsCertFileName)},
			corev1.EnvVar{Name: envTLSKeyFile, Value: filepath.Join(tlsMountPath, tlsKeyFileName)},
			corev1.EnvVar{Name: envTLSCAFile, Value: filepath.Join(tlsMountPath, tlsCAFileName)},
			corev1.EnvVar{Name: envTLSDomain, Value: k8sConfig.TLS.Domain},
		)
	}

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
					Volumes: volumes,
					Containers: []corev1.Container{{
						Name:         workload.WorkloadName(),
						Image:        workload.Image,
						Ports:        ports,
						VolumeMounts: volumeMounts,
						Env:          containerEnv,
					}},
				},
			},
		},
	}, nil
}

// BuildService 将 deployment workload 构造成可直接下发的 Service 对象。
func BuildService(workload *DeploymentWorkload) (*corev1.Service, error) {
	k8sConfig := LoadK8sConfig()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
		withManagedBy(k8sConfig.ManagedBy),
	)
	selectorLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
	)
	ports, err := buildServicePorts(workload.Ports)
	if err != nil {
		return nil, fmt.Errorf("构建 service ports 失败: %w", err)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload.ServiceResourceName(),
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
func BuildHTTPRoute(workload *HTTPRouteWorkload) (*unstructured.Unstructured, error) {
	k8sConfig := LoadK8sConfig()
	objectLabels := buildLabels(
		withApp(workload.App),
		withService(workload.ServiceName),
		withDominionApp(workload.DominionApp),
		withDominionEnvironment(workload.EnvironmentName),
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
	app                 string
	service             string
	dominionApp         string
	dominionEnvironment string
	managedBy           string
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

func withDominionApp(name string) labelOption {
	return func(set *labelSet) {
		set.dominionApp = strings.TrimSpace(name)
	}
}

func withDominionEnvironment(name string) labelOption {
	return func(set *labelSet) {
		set.dominionEnvironment = strings.TrimSpace(name)
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
	if set.dominionApp != "" {
		result[dominionAppLabelKey] = set.dominionApp
	}
	if set.dominionEnvironment != "" {
		result[dominionEnvironmentLabelKey] = set.dominionEnvironment
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
