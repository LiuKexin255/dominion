package k8s

import (
	"fmt"
	"strings"

	"dominion/projects/infra/deploy/domain"
)

const infraResourceMongoDB = "mongodb"

// DeployObjects 表示一次部署所需的 Kubernetes 工作负载对象集合。
type DeployObjects struct {
	Deployments      []*DeploymentWorkload
	HTTPRoutes       []*HTTPRouteWorkload
	MongoDBWorkloads []*MongoDBWorkload
}

// ConvertToWorkloads 将领域模型 Environment 转换为 Kubernetes 工作负载对象。
//
// 该函数只做数据映射，不执行任何 K8s 集群操作。env 提供 DesiredState 中的
// Services、Infras、HTTPRoutes 分别映射为 DeploymentWorkload、MongoDBWorkload
// 和 HTTPRouteWorkload。cfg 提供网关等静态配置。
func ConvertToWorkloads(env *domain.Environment, cfg *K8sConfig) (*DeployObjects, error) {
	desiredState := env.DesiredState()
	envName := env.Name().Label()
	objects := &DeployObjects{}
	deploymentMap := make(map[string]*DeploymentWorkload, len(desiredState.Services))
	serviceSpecMap := make(map[string]*domain.ServiceSpec, len(desiredState.Services))

	for _, svc := range desiredState.Services {
		workload := convertServiceToWorkload(svc, envName)
		objects.Deployments = append(objects.Deployments, workload)
		deploymentMap[svc.Name] = workload
		serviceSpecMap[svc.Name] = svc
	}

	for _, infra := range desiredState.Infras {
		workload, err := convertInfraToMongoWorkload(infra, envName)
		if err != nil {
			return nil, err
		}
		objects.MongoDBWorkloads = append(objects.MongoDBWorkloads, workload)
	}

	for _, route := range desiredState.HTTPRoutes {
		if len(route.Rules) == 0 {
			continue
		}
		deployment, ok := deploymentMap[route.ServiceName]
		if !ok {
			return nil, fmt.Errorf("HTTPRoute service %q 未找到对应的 Deployment", route.ServiceName)
		}
		spec := serviceSpecMap[route.ServiceName]

		workload, err := convertHTTPRouteToWorkload(route, envName, cfg, deployment, spec)
		if err != nil {
			return nil, err
		}
		objects.HTTPRoutes = append(objects.HTTPRoutes, workload)
	}

	return objects, nil
}

func convertServiceToWorkload(svc *domain.ServiceSpec, envName string) *DeploymentWorkload {
	return &DeploymentWorkload{
		ServiceName:     svc.Name,
		EnvironmentName: envName,
		App:             svc.App,
		Image:           svc.Image,
		Replicas:        svc.Replicas,
		TLSEnabled:      svc.TLSEnabled,
		Ports:           convertPorts(svc.Ports),
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
			Persistence:     PersistenceConfig{Enabled: infra.PersistenceEnabled},
		}, nil
	default:
		return nil, fmt.Errorf("不支持的 infra resource 类型: %s", infra.Resource)
	}
}

func convertHTTPRouteToWorkload(route *domain.HTTPRouteSpec, envName string, cfg *K8sConfig, deployment *DeploymentWorkload, spec *domain.ServiceSpec) (*HTTPRouteWorkload, error) {
	matches, err := convertHTTPRouteMatches(spec.Ports, route.Rules)
	if err != nil {
		return nil, err
	}

	return &HTTPRouteWorkload{
		ServiceName:      route.ServiceName,
		EnvironmentName:  envName,
		App:              spec.App,
		Hostnames:        route.Hostnames,
		Matches:          matches,
		BackendService:   deployment.ServiceResourceName(),
		GatewayName:      cfg.Gateway.Name,
		GatewayNamespace: cfg.Gateway.Namespace,
	}, nil
}

func convertHTTPRouteMatches(ports []domain.ServicePortSpec, rules []domain.HTTPRouteRule) ([]*HTTPRoutePathMatch, error) {
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
func convertPorts(ports []domain.ServicePortSpec) []*DeploymentPort {
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
