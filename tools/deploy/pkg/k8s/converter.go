package k8s

import (
	"fmt"

	"dominion/tools/deploy/pkg/config"
)

// DeployObjects 表示一次部署所需的 Kubernetes 工作负载对象集合。
type DeployObjects struct {
	Deployments []*DeploymentWorkload
	Services    []*ServiceWorkload
	HTTPRoutes  []*HTTPRouteWorkload
}

// NewDeployObjects 根据部署配置和服务配置构建 Kubernetes 部署对象。
func NewDeployObjects(deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig, envName string) (*DeployObjects, error) {
	k8sConfig := LoadK8sConfig()

	// 构建 URI -> ServiceConfig 的 map
	serviceConfigMap := make(map[string]*config.ServiceConfig)
	for _, sc := range serviceConfigs {
		if sc.URI == "" {
			return nil, fmt.Errorf("service config %s 的 URI 为空", sc.Name)
		}
		if _, exists := serviceConfigMap[sc.URI]; exists {
			return nil, fmt.Errorf("service config URI 重复: %s", sc.URI)
		}
		serviceConfigMap[sc.URI] = sc
	}

	objects := new(DeployObjects)

	for _, deployService := range deployConfig.Services {
		if deployService == nil {
			return nil, fmt.Errorf("deploy service 为空")
		}

		// 根据 Artifact.Path (URI) 匹配 service config
		serviceConfig, ok := serviceConfigMap[deployService.Artifact.Path]
		if !ok {
			return nil, fmt.Errorf("deploy service 引用的 path %s 未找到对应的 service config", deployService.Artifact.Path)
		}

		// 验证 artifact name 是否存在
		if _, err := serviceConfig.GetArtifact(deployService.Artifact.Name); err != nil {
			return nil, fmt.Errorf("service config %s 中未找到 artifact %s", serviceConfig.URI, deployService.Artifact.Name)
		}

		deployment, err := NewDeploymentWorkload(
			serviceConfig,
			envName,
			deployService.Artifact.Name,
		)
		if err != nil {
			return nil, fmt.Errorf("创建 deployment workload 失败: %w", err)
		}
		objects.Deployments = append(objects.Deployments, deployment)

		svc, err := deployment.NewServiceWorkload()
		if err != nil {
			return nil, fmt.Errorf("创建 service workload 失败: %w", err)
		}
		objects.Services = append(objects.Services, svc)

		if len(deployService.HTTP.Matches) == 0 {
			continue
		}

		route, err := svc.NewHTTPRouteWorkload(deployService, k8sConfig)
		if err != nil {
			return nil, fmt.Errorf("创建 http route workload 失败: %w", err)
		}
		objects.HTTPRoutes = append(objects.HTTPRoutes, route)
	}

	return objects, nil
}
