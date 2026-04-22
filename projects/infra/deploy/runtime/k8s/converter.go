package k8s

import (
	"fmt"
	"strings"

	"dominion/projects/infra/deploy/domain"
)

const infraResourceMongoDB = "mongodb"

// DeployObjects 表示一次部署所需的 Kubernetes 工作负载对象集合。
type DeployObjects struct {
	Deployments       []*DeploymentWorkload
	HTTPRoutes        []*HTTPRouteWorkload
	MongoDBWorkloads  []*MongoDBWorkload
	StatefulWorkloads []*StatefulWorkload
	InstanceRoutes    []*HTTPRouteWorkload
}

// ConvertToWorkloads 将领域模型 Environment 转换为 Kubernetes 工作负载对象。
//
// 该函数只做数据映射，不执行任何 K8s 集群操作。env 提供 DesiredState 中的
// Artifacts 和 Infras 分别映射为 DeploymentWorkload（含可选 HTTPRouteWorkload）
// 和 MongoDBWorkload。cfg 提供网关等静态配置。
func ConvertToWorkloads(env *domain.Environment, cfg *K8sConfig) (*DeployObjects, error) {
	desiredState := env.DesiredState()
	envName := env.Name().Label()
	envType := env.Type()
	objects := &DeployObjects{}

	for _, artifact := range desiredState.Artifacts {
		switch artifact.WorkloadKind {
		case domain.WorkloadKindStateful:
			statefulWorkload := convertArtifactToStatefulWorkload(artifact, envName)
			objects.StatefulWorkloads = append(objects.StatefulWorkloads, statefulWorkload)

			instanceRoutes, err := convertArtifactHTTPToInstanceRoutes(artifact, envName, cfg, envType)
			if err != nil {
				return nil, err
			}
			objects.InstanceRoutes = append(objects.InstanceRoutes, instanceRoutes...)
		default:
			deployment := convertArtifactToDeployment(artifact, envName)
			objects.Deployments = append(objects.Deployments, deployment)

			if artifact.HTTP != nil && len(artifact.HTTP.Matches) > 0 {
				route, err := convertArtifactHTTPToRoute(artifact, envName, cfg, deployment, envType)
				if err != nil {
					return nil, err
				}
				objects.HTTPRoutes = append(objects.HTTPRoutes, route)
			}
		}
	}

	for _, infra := range desiredState.Infras {
		workload, err := convertInfraToMongoWorkload(infra, envName)
		if err != nil {
			return nil, err
		}
		objects.MongoDBWorkloads = append(objects.MongoDBWorkloads, workload)
	}

	return objects, nil
}

func convertArtifactToStatefulWorkload(artifact *domain.ArtifactSpec, envName string) *StatefulWorkload {
	var hostnames []string
	if artifact.HTTP != nil {
		hostnames = artifact.HTTP.Hostnames
	}

	return &StatefulWorkload{
		ServiceName:     artifact.Name,
		EnvironmentName: envName,
		App:             artifact.App,
		Image:           artifact.Image,
		Replicas:        artifact.Replicas,
		TLSEnabled:      artifact.TLSEnabled,
		OSSEnabled:      artifact.OSSEnabled,
		Ports:           convertPorts(artifact.Ports),
		Hostnames:       hostnames,
		Env:             artifact.Env,
	}
}

func convertArtifactToDeployment(artifact *domain.ArtifactSpec, envName string) *DeploymentWorkload {
	return &DeploymentWorkload{
		ServiceName:     artifact.Name,
		EnvironmentName: envName,
		App:             artifact.App,
		Image:           artifact.Image,
		Replicas:        artifact.Replicas,
		TLSEnabled:      artifact.TLSEnabled,
		OSSEnabled:      artifact.OSSEnabled,
		Ports:           convertPorts(artifact.Ports),
		Env:             artifact.Env,
	}
}

func convertInfraToMongoWorkload(infra *domain.InfraSpec, envName string) (*MongoDBWorkload, error) {
	switch infra.Resource {
	case infraResourceMongoDB:
		return &MongoDBWorkload{
			ServiceName:     infra.Name,
			EnvironmentName: envName,
			App:             infra.App,
			ProfileName:     infra.Profile,
			Persistence:     PersistenceConfig{Enabled: infra.Persistence.Enabled},
		}, nil
	default:
		return nil, fmt.Errorf("不支持的 infra resource 类型: %s", infra.Resource)
	}
}

func convertArtifactHTTPToRoute(
	artifact *domain.ArtifactSpec,
	envName string,
	cfg *K8sConfig,
	deployment *DeploymentWorkload,
	envType domain.EnvironmentType,
) (*HTTPRouteWorkload, error) {
	matches, err := convertHTTPRouteMatches(artifact.Ports, artifact.HTTP.Matches)
	if err != nil {
		return nil, err
	}

	return &HTTPRouteWorkload{
		ServiceName:      artifact.Name,
		EnvironmentName:  envName,
		App:              artifact.App,
		Hostnames:        artifact.HTTP.Hostnames,
		Matches:          matches,
		BackendService:   deployment.ServiceResourceName(),
		GatewayName:      cfg.Gateway.Name,
		GatewayNamespace: cfg.Gateway.Namespace,
		EnvType:          envType,
	}, nil
}

func convertArtifactHTTPToInstanceRoutes(
	artifact *domain.ArtifactSpec,
	envName string,
	cfg *K8sConfig,
	envType domain.EnvironmentType,
) ([]*HTTPRouteWorkload, error) {
	if artifact.HTTP == nil || len(artifact.HTTP.Hostnames) == 0 || artifact.Replicas == 0 {
		return nil, nil
	}
	matches, err := convertHTTPRouteMatches(artifact.Ports, artifact.HTTP.Matches)
	if err != nil {
		return nil, err
	}

	instanceRoutes := make([]*HTTPRouteWorkload, 0, artifact.Replicas)
	for i := 0; i < int(artifact.Replicas); i++ {
		instanceRoutes = append(instanceRoutes, &HTTPRouteWorkload{
			ServiceName:      artifact.Name,
			EnvironmentName:  envName,
			App:              artifact.App,
			Hostnames:        expandStatefulHostnames(artifact.Name, i, artifact.HTTP.Hostnames),
			Matches:          matches,
			BackendService:   newInstanceObjectName(WorkloadKindInstanceService, envName, artifact.Name, i),
			GatewayName:      cfg.Gateway.Name,
			GatewayNamespace: cfg.Gateway.Namespace,
			EnvType:          envType,
		})
	}

	return instanceRoutes, nil
}
func expandStatefulHostnames(serviceName string, instanceIndex int, hostnames []string) []string {
	if len(hostnames) == 0 {
		return nil
	}

	expandedHostnames := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		expandedHostnames = append(expandedHostnames, fmt.Sprintf("%s-%d.%s", serviceName, instanceIndex, hostname))
	}

	return expandedHostnames
}
func convertHTTPRouteMatches(ports []domain.ArtifactPortSpec, rules []domain.HTTPRouteRule) ([]*HTTPRoutePathMatch, error) {
	backendPortMap := make(map[string]int, len(ports))
	for _, port := range ports {
		key := strings.ToLower(strings.TrimSpace(port.Name))
		if key == "" {
			continue
		}
		backendPortMap[key] = int(port.Port)
	}

	matches := make([]*HTTPRoutePathMatch, 0, len(rules))
	for _, rule := range rules {
		backend := strings.TrimSpace(rule.Backend)
		backendPort, ok := backendPortMap[strings.ToLower(backend)]
		if !ok {
			return nil, fmt.Errorf("HTTPRoute backend %q 未找到对应端口", backend)
		}

		matches = append(matches, &HTTPRoutePathMatch{
			Type:        convertPathType(rule.Path.Type),
			Value:       rule.Path.Value,
			BackendName: backend,
			BackendPort: backendPort,
		})
	}

	return matches, nil
}

// convertPorts 将领域模型端口列表转换为部署端口列表。
func convertPorts(ports []domain.ArtifactPortSpec) []*DeploymentPort {
	if len(ports) == 0 {
		return nil
	}

	result := make([]*DeploymentPort, len(ports))
	for i, p := range ports {
		result[i] = &DeploymentPort{
			Name: p.Name,
			Port: int(p.Port),
		}
	}
	return result
}

// convertPathType 将领域模型路径匹配类型转换为 K8s 路径匹配类型。
func convertPathType(t domain.HTTPPathRuleType) HTTPPathMatchType {
	switch t {
	case domain.HTTPPathRuleTypePathPrefix:
		return HTTPPathMatchTypePathPrefix
	default:
		return HTTPPathMatchTypeUnspecified
	}
}
