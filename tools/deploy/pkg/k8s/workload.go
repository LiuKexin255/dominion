package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"dominion/tools/deploy/pkg/config"
)

var (
	// nonDNSLabel 匹配名称中不符合 DNS label 规范的字符。
	nonDNSLabel = regexp.MustCompile(`[^a-z0-9-]+`)
)

// WorkloadKind 表示不同 Kubernetes workload 对象的类型前缀。
type WorkloadKind string

const (
	// WorkloadEmpty 类型为空
	WorkloadEmpty = ""
	// WorkloadUnknown 表示未知类型前缀。
	WorkloadUnknown WorkloadKind = "unknown"
	// WorkloadKindDeployment 表示 Deployment 类型前缀。
	WorkloadKindDeployment WorkloadKind = "dp"
	// WorkloadKindService 表示 Service 类型前缀。
	WorkloadKindService WorkloadKind = "svc"
	// WorkloadKindHTTPRoute 表示 HTTPRoute 类型前缀。
	WorkloadKindHTTPRoute WorkloadKind = "route"
	// WorkloadKindMongoDB 表示 MongoDB 类型前缀。
	WorkloadKindMongoDB WorkloadKind = "mongo"
	// WorkloadKindPVC 表示 PVC 类型前缀。
	WorkloadKindPVC WorkloadKind = "pvc"
	// WorkloadKindSecret 表示 Secret 类型前缀。
	WorkloadKindSecret WorkloadKind = "secret"

	maxK8sResourceNameSize = 63
)

// DeploymentPort 定义 deployment 暴露端口。
type DeploymentPort struct {
	Name string
	Port int
}

// DeploymentWorkload 描述 deployment 生成所需字段。
type DeploymentWorkload struct {
	TLSEnabled      bool
	ServiceName     string
	EnvironmentName string
	App             string
	DominionApp     string
	Desc            string
	Image           string
	Replicas        int32
	Ports           []*DeploymentPort
}

// WorkloadName 返回 deployment 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *DeploymentWorkload) WorkloadName() string {
	if w == nil {
		return ""
	}

	return newWorkloadName(w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// Validate 校验 deployment workload 字段是否合法。
// 当必填字段缺失、名称超长或端口非法时返回错误。
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
	if strings.TrimSpace(w.Desc) == "" {
		return fmt.Errorf("deployment workload 缺少 desc")
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
		if strings.TrimSpace(port.Name) == "" {
			return fmt.Errorf("deployment workload 存在空端口名")
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("deployment workload 端口 %d 非法", port.Port)
		}
	}

	return nil
}

// NewServiceWorkload 基于 deployment workload 生成 service workload。
// 返回的新对象会复用当前对象中的 service 标识和端口信息，并在返回前完成校验。
func (w *DeploymentWorkload) NewServiceWorkload() (*ServiceWorkload, error) {
	if w == nil {
		return nil, fmt.Errorf("deployment workload 为空")
	}

	svc := &ServiceWorkload{
		ServiceName:     w.ServiceName,
		EnvironmentName: w.EnvironmentName,
		App:             w.App,
		DominionApp:     w.DominionApp,
		Desc:            w.Desc,
		Ports:           w.Ports,
	}

	if err := svc.Validate(); err != nil {
		return nil, err
	}

	return svc, nil
}

func newWorkloadName(app string, dominionApp string, serviceName string, environmentName string) string {
	return newObjectName(WorkloadKindDeployment, app, dominionApp, serviceName, environmentName)
}

// ServiceWorkload 描述 service 生成所需字段。
type ServiceWorkload struct {
	ServiceName     string
	EnvironmentName string
	App             string
	DominionApp     string
	Desc            string
	Ports           []*DeploymentPort
}

// ResourceName 返回 service 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *ServiceWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newServiceName(w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

func newServiceName(app string, dominionApp string, serviceName string, environmentName string) string {
	return newObjectName(WorkloadKindService, app, dominionApp, serviceName, environmentName)
}

// Validate 校验 service workload 字段是否合法。
// 当必填字段缺失、名称超长或端口非法时返回错误。
func (w *ServiceWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("service workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("service workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("service workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("service workload 缺少 app")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("service workload name 超过 63 字符")
	}
	for _, port := range w.Ports {
		if port == nil {
			return fmt.Errorf("service workload 存在空端口")
		}
		if strings.TrimSpace(port.Name) == "" {
			return fmt.Errorf("service workload 存在空端口名")
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("service workload 端口 %d 非法", port.Port)
		}
	}

	return nil
}

// NewHTTPRouteWorkload 基于 service workload 生成 HTTPRoute workload。
// deployService 提供路由匹配配置；网关配置通过静态配置加载。
func (w *ServiceWorkload) NewHTTPRouteWorkload(deployService *config.DeployService) (*HTTPRouteWorkload, error) {
	k8sConfig := LoadK8sConfig()
	matches, err := buildHTTPRoutePathMatches(w.Ports, deployService.HTTP.Matches)
	if err != nil {
		return nil, err
	}

	route := &HTTPRouteWorkload{
		ServiceName:      w.ServiceName,
		EnvironmentName:  w.EnvironmentName,
		App:              w.App,
		DominionApp:      w.DominionApp,
		Hostnames:        deployService.HTTP.Hostnames,
		Matches:          matches,
		BackendService:   w.ResourceName(),
		GatewayName:      k8sConfig.Gateway.Name,
		GatewayNamespace: k8sConfig.Gateway.Namespace,
	}

	if err := route.Validate(); err != nil {
		return nil, err
	}

	return route, nil
}

func buildHTTPRoutePathMatches(ports []*DeploymentPort, deployHTTPMatches []*config.DeployHTTPMatch) ([]*HTTPRoutePathMatch, error) {
	backendPortMap := make(map[string]int)
	for _, port := range ports {
		if port == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(port.Name))
		backendPortMap[key] = port.Port
	}

	matches := make([]*HTTPRoutePathMatch, 0, len(deployHTTPMatches))
	for _, match := range deployHTTPMatches {
		if match == nil {
			continue
		}

		backend := strings.TrimSpace(match.Backend)
		if backend == "" {
			return nil, fmt.Errorf("service workload 缺少 backend，无法生成路由")
		}

		backendPort, exists := backendPortMap[strings.ToLower(backend)]
		if !exists {
			return nil, fmt.Errorf("service workload backend %s 不存在", backend)
		}

		matches = append(matches, &HTTPRoutePathMatch{
			Type:        match.Path.Type,
			Value:       strings.TrimSpace(match.Path.Value),
			BackendPort: backendPort,
			BackendName: backend,
		})
	}
	if len(matches) == 0 {
		return nil, nil
	}

	return matches, nil
}

// HTTPRoutePathMatch 描述 HTTPRoute 的单条 path 匹配规则。
type HTTPRoutePathMatch struct {
	Type        config.HTTPPathMatchType
	Value       string
	BackendName string
	BackendPort int
}

// HTTPRouteWorkload 描述 HTTPRoute 生成所需字段。
type HTTPRouteWorkload struct {
	ServiceName      string
	EnvironmentName  string
	App              string
	DominionApp      string
	Hostnames        []string
	Matches          []*HTTPRoutePathMatch
	BackendService   string
	GatewayName      string
	GatewayNamespace string
}

// ResourceName 返回 HTTPRoute 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *HTTPRouteWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newHTTPRouteName(w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

func newHTTPRouteName(app string, dominionApp string, serviceName string, environmentName string) string {
	return newObjectName(WorkloadKindHTTPRoute, app, dominionApp, serviceName, environmentName)
}

// Validate 校验 HTTPRoute workload 字段是否合法。
// 当必填字段缺失、名称超长、匹配规则缺失或端口非法时返回错误。
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
		if strings.TrimSpace(string(match.Type)) == "" {
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

func newDeploymentWorkload(serviceCfg *config.ServiceConfig, artifact *config.ServiceArtifact, envName string, dominionApp string, imageRef string) (*DeploymentWorkload, error) {
	if strings.TrimSpace(imageRef) == "" {
		return nil, fmt.Errorf("deployment workload image 为空")
	}
	if artifact == nil {
		return nil, fmt.Errorf("service artifact 为空")
	}

	w := &DeploymentWorkload{
		TLSEnabled:      artifact.TLS,
		ServiceName:     serviceCfg.Name,
		EnvironmentName: envName,
		App:             serviceCfg.App,
		DominionApp:     dominionApp,
		Desc:            serviceCfg.Desc,
		Image:           strings.TrimSpace(imageRef),
		Replicas:        1,
		Ports:           toDeploymentPorts(artifact.Ports),
	}

	if err := w.Validate(); err != nil {
		return nil, err
	}

	return w, nil
}
func resolveArtifactByName(serviceCfg *config.ServiceConfig, artifactName string) (*config.ServiceArtifact, error) {
	if serviceCfg == nil {
		return nil, fmt.Errorf("service config 为空")
	}
	if strings.TrimSpace(artifactName) == "" {
		return nil, fmt.Errorf("service artifact name 为空")
	}

	artifact, err := serviceCfg.GetArtifact(strings.TrimSpace(artifactName))
	if err != nil {
		return nil, fmt.Errorf("service config 不存在产物 %s: %w", artifactName, err)
	}

	return artifact, nil
}
func toDeploymentPorts(ports []*config.ServiceArtifactPort) []*DeploymentPort {
	mapped := make([]*DeploymentPort, 0, len(ports))
	for _, p := range ports {
		if p == nil {
			continue
		}
		mapped = append(mapped, &DeploymentPort{
			Name: p.Name,
			Port: p.Port,
		})
	}
	if len(mapped) == 0 {
		return nil
	}

	return mapped
}

func newObjectName(kind WorkloadKind, app string, dominionApp string, serviceName string, environmentName string) string {
	if kind == WorkloadEmpty {
		kind = WorkloadUnknown
	}
	parts := []string{string(kind), environmentName, serviceName, shortNameHash(app, dominionApp)}
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeNamePart(part)
		if part != "" {
			normalized = append(normalized, part)
		}
	}

	return strings.Join(normalized, "-")
}

func sanitizeNamePart(part string) string {
	part = strings.TrimSpace(strings.ToLower(part))
	part = nonDNSLabel.ReplaceAllString(part, "-")
	part = strings.Trim(part, "-")
	return part
}

func shortNameHash(app string, dominionApp string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(app) + "\x00" + strings.TrimSpace(dominionApp)))
	return hex.EncodeToString(sum[:4])
}
