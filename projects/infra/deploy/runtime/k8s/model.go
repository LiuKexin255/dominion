package k8s

import (
	"fmt"
	"strings"

	"dominion/projects/infra/deploy/domain"
)

// DeploymentPort 定义 deployment 暴露端口。
type DeploymentPort struct {
	Name string
	Port int
}

// DeploymentWorkload 描述 deployment 生成所需字段。
type DeploymentWorkload struct {
	TLSEnabled      bool
	OSSEnabled      bool
	ServiceName     string
	EnvironmentName string
	App             string
	Image           string
	Replicas        int32
	Ports           []*DeploymentPort
	Env             map[string]string
}

// WorkloadName 返回 deployment 对应的资源名。
func (w *DeploymentWorkload) WorkloadName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindDeployment, w.App, w.ServiceName)
}

// ServiceResourceName 返回 deployment 对应 service 的资源名。
func (w *DeploymentWorkload) ServiceResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindService, w.App, w.ServiceName)
}

// Validate 校验 deployment workload 字段是否合法。
func (w *DeploymentWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("deployment workload 为空")
	}

	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("deployment workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("deployment workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("deployment workload 缺少 app")
	}
	if strings.TrimSpace(w.Image) == "" {
		return fmt.Errorf("deployment workload 缺少 image")
	}
	if len(w.WorkloadName()) > maxK8sResourceNameSize {
		return fmt.Errorf("deployment workload name 超过 63 字符")
	}
	if w.Replicas < 0 {
		return fmt.Errorf("deployment workload replicas 不能小于 0")
	}

	for _, port := range w.Ports {
		if port == nil {
			return fmt.Errorf("deployment workload 存在空端口")
		}
		if strings.TrimSpace(port.Name) == "" {
			return fmt.Errorf("deployment workload 存在空端口名")
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("deployment workload 端口 %d 非法", port.Port)
		}
	}

	return nil
}

// StatefulWorkload 描述 statefulset 生成所需字段。
type StatefulWorkload struct {
	TLSEnabled      bool
	OSSEnabled      bool
	ServiceName     string
	EnvironmentName string
	App             string
	Image           string
	Replicas        int32
	Ports           []*DeploymentPort
	Hostnames       []string
	Env             map[string]string
}

// WorkloadName 返回 statefulset 对应的资源名。
func (w *StatefulWorkload) WorkloadName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindStatefulSet, w.App, w.ServiceName)
}

// ServiceResourceName 返回 statefulset 对应 governing headless service 的资源名。
func (w *StatefulWorkload) ServiceResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindService, w.App, w.ServiceName)
}

// Validate 校验 stateful workload 字段是否合法。
func (w *StatefulWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("stateful workload 为空")
	}

	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("stateful workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("stateful workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("stateful workload 缺少 app")
	}
	if strings.TrimSpace(w.Image) == "" {
		return fmt.Errorf("stateful workload 缺少 image")
	}
	if len(w.WorkloadName()) > maxK8sResourceNameSize {
		return fmt.Errorf("stateful workload name 超过 63 字符")
	}
	if w.Replicas < 0 {
		return fmt.Errorf("stateful workload replicas 不能小于 0")
	}

	for _, port := range w.Ports {
		if port == nil {
			return fmt.Errorf("stateful workload 存在空端口")
		}
		if strings.TrimSpace(port.Name) == "" {
			return fmt.Errorf("stateful workload 存在空端口名")
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("stateful workload 端口 %d 非法", port.Port)
		}
	}

	return nil
}

// HTTPPathMatchType 表示 HTTP path 匹配类型。
type HTTPPathMatchType string

const (
	HTTPPathMatchTypeUnspecified HTTPPathMatchType = ""
	HTTPPathMatchTypePathPrefix  HTTPPathMatchType = "PathPrefix"
	HTTPPathMatchTypeExact       HTTPPathMatchType = "Exact"
)

// HTTPRoutePathMatch 描述 HTTPRoute 的单条 path 匹配规则。
type HTTPRoutePathMatch struct {
	Type        HTTPPathMatchType
	Value       string
	BackendName string
	BackendPort int
}

// HTTPRouteWorkload 描述 HTTPRoute 生成所需字段。
type HTTPRouteWorkload struct {
	ServiceName      string
	EnvironmentName  string
	App              string
	Hostnames        []string
	Matches          []*HTTPRoutePathMatch
	BackendService   string
	GatewayName      string
	GatewayNamespace string
	EnvType          domain.EnvironmentType
}

// ResourceName 返回 HTTPRoute 对应的资源名。
func (w *HTTPRouteWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindHTTPRoute, w.App, w.ServiceName)
}

// Validate 校验 HTTPRoute workload 字段是否合法。
func (w *HTTPRouteWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("http route workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("http route workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("http route workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("http route workload 缺少 app")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("http route workload name 超过 63 字符")
	}
	if strings.TrimSpace(w.BackendService) == "" {
		return fmt.Errorf("http route workload 缺少 backend service")
	}
	if strings.TrimSpace(w.GatewayName) == "" {
		return fmt.Errorf("http route workload 缺少 gateway name")
	}
	if strings.TrimSpace(w.GatewayNamespace) == "" {
		return fmt.Errorf("http route workload 缺少 gateway namespace")
	}
	if len(w.Matches) == 0 {
		return fmt.Errorf("http route workload 缺少 matches")
	}
	for _, match := range w.Matches {
		if match == nil {
			return fmt.Errorf("http route workload 存在空 match")
		}
		if match.Type == "" {
			return fmt.Errorf("http route workload path type 为空")
		}
		if strings.TrimSpace(match.Value) == "" {
			return fmt.Errorf("http route workload path value 为空")
		}
		if match.BackendPort < 1 || match.BackendPort > 65535 {
			return fmt.Errorf("http route workload backend port 非法")
		}
	}

	return nil
}

// PersistenceConfig 表示基础设施部署的持久化配置。
type PersistenceConfig struct {
	Enabled bool
}

// MongoDBWorkload 描述 MongoDB workload 生成所需字段。
type MongoDBWorkload struct {
	ServiceName     string
	EnvironmentName string
	App             string
	ProfileName     string
	Persistence     PersistenceConfig
}

// ResourceName 返回 MongoDB workload 对应的资源名。
func (w *MongoDBWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindMongoDB, w.App, w.ServiceName)
}

// ServiceResourceName 返回 MongoDB Service 对应的资源名。
func (w *MongoDBWorkload) ServiceResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindService, w.App, w.ServiceName)
}

// SecretResourceName 返回 MongoDB Secret 对应的资源名。
func (w *MongoDBWorkload) SecretResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindSecret, w.App, w.ServiceName)
}

// PVCResourceName 返回 MongoDB PVC 对应的资源名。
func (w *MongoDBWorkload) PVCResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindPVC, w.App, w.ServiceName)
}

// Validate 校验 MongoDB workload 字段是否合法。
func (w *MongoDBWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("mongo workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("mongo workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("mongo workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("mongo workload 缺少 app")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("mongo workload name 超过 63 字符")
	}
	if strings.TrimSpace(w.ProfileName) == "" {
		return fmt.Errorf("mongo workload 缺少 profile name")
	}

	return nil
}
