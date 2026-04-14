package k8s

import (
	"fmt"

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
	envName := env.Name().String()
	objects := new(DeployObjects)

	// 构建服务名到 DeploymentWorkload 和 ServiceSpec 的映射，供 HTTPRoute 解析后端使用。
	deploymentMap := make(map[string]*DeploymentWorkload)
	serviceSpecMap := make(map[string]*domain.ServiceSpec)

	for _, svc := range desiredState.Services {
		workload := &DeploymentWorkload{
			ServiceName:     svc.Name,
			EnvironmentName: envName,
			App:             svc.App,
			Image:           svc.Image,
			Replicas:        svc.Replicas,
			TLSEnabled:      svc.TLSEnabled,
			Ports:           convertPorts(svc.Ports),
		}
		objects.Deployments = append(objects.Deployments, workload)
		deploymentMap[svc.Name] = workload
		serviceSpecMap[svc.Name] = svc
	}

	for _, infra := range desiredState.Infras {
		switch infra.Resource {
		case infraResourceMongoDB:
			workload := &MongoDBWorkload{
				ServiceName:     infra.Name,
				EnvironmentName: envName,
				App:             infra.App,
				ProfileName:     infra.Profile,
				Persistence:     PersistenceConfig{Enabled: infra.PersistenceEnabled},
			}
			objects.MongoDBWorkloads = append(objects.MongoDBWorkloads, workload)
		default:
			return nil, fmt.Errorf("不支持的 infra resource 类型: %s", infra.Resource)
		}
	}

	for _, route := range desiredState.HTTPRoutes {
		if len(route.Rules) == 0 {
			continue
		}

		primaryBackend := route.Rules[0].Backend
		primaryDeployment, ok := deploymentMap[primaryBackend]
		if !ok {
			return nil, fmt.Errorf("HTTPRoute 后端服务 %q 未找到对应的 Deployment", primaryBackend)
		}
		primarySpec := serviceSpecMap[primaryBackend]

		matches := make([]*HTTPRoutePathMatch, 0, len(route.Rules))
		for _, rule := range route.Rules {
			dep, ok := deploymentMap[rule.Backend]
			if !ok {
				return nil, fmt.Errorf("HTTPRoute 后端服务 %q 未找到对应的 Deployment", rule.Backend)
			}
			spec := serviceSpecMap[rule.Backend]

			var backendPort int
			if len(spec.Ports) > 0 {
				backendPort = int(spec.Ports[0].Port)
			}

			matches = append(matches, &HTTPRoutePathMatch{
				Type:        convertPathType(rule.Path.Type),
				Value:       rule.Path.Value,
				BackendName: dep.ServiceResourceName(),
				BackendPort: backendPort,
			})
		}

		routeWorkload := &HTTPRouteWorkload{
			ServiceName:      primaryBackend,
			EnvironmentName:  envName,
			App:              primarySpec.App,
			Hostnames:        route.Hostnames,
			Matches:          matches,
			BackendService:   primaryDeployment.ServiceResourceName(),
			GatewayName:      cfg.Gateway.Name,
			GatewayNamespace: cfg.Gateway.Namespace,
		}
		objects.HTTPRoutes = append(objects.HTTPRoutes, routeWorkload)
	}

	return objects, nil
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
