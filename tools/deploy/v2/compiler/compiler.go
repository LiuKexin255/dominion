package compiler

import (
	"fmt"
	"sort"
	"strings"

	"dominion/projects/infra/deploy"
	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/imagepush"
	"dominion/tools/deploy/pkg/workspace"
)

const defaultServiceReplicas = int32(1)

// Compile compiles deploy config, service configs, and image results into desired state.
func Compile(deployConfig *config.DeployConfig, serviceConfigs map[string]*config.ServiceConfig, imageResults map[string]*imagepush.Result) (*deploy.EnvironmentDesiredState, error) {
	if deployConfig == nil {
		return nil, fmt.Errorf("deploy config is nil")
	}

	desiredState := new(deploy.EnvironmentDesiredState)
	for _, deployService := range deployConfig.Services {
		if deployService == nil {
			return nil, fmt.Errorf("deploy service is nil")
		}

		if isInfraService(deployService) {
			infraSpec := &deploy.InfraSpec{
				Resource: deployService.Infra.Resource,
				Profile:  deployService.Infra.Profile,
				Name:     deployService.Infra.Name,
				App:      deployService.Infra.App,
			}
			if deployService.Infra.Persistence.Enabled {
				infraSpec.Persistence = &deploy.InfraPersistenceSpec{
					Enabled: true,
				}
			}
			desiredState.Infras = append(desiredState.Infras, infraSpec)
			continue
		}

		serviceConfig, ok := serviceConfigs[deployService.Artifact.Path]
		if !ok {
			return nil, fmt.Errorf("service config %s not found", deployService.Artifact.Path)
		}

		artifact, err := serviceConfig.GetArtifact(deployService.Artifact.Name)
		if err != nil {
			return nil, fmt.Errorf("service config %s artifact %s not found: %w", deployService.Artifact.Path, deployService.Artifact.Name, err)
		}

		imageResult, ok := imageResults[artifact.Target]
		if !ok {
			return nil, fmt.Errorf("image result %s not found", artifact.Target)
		}

		imageRef, err := imageResult.ImageRef()
		if err != nil {
			return nil, fmt.Errorf("build image ref for %s failed: %w", artifact.Target, err)
		}

		replicas := defaultServiceReplicas
		if deployService.Artifact.Replicas != 0 {
			replicas = int32(deployService.Artifact.Replicas)
		}

		compiledArtifact := &deploy.ArtifactSpec{
			Name:       serviceConfig.Name,
			App:        serviceConfig.App,
			Image:      imageRef,
			Replicas:   replicas,
			TlsEnabled: artifact.TLS,
			OssEnabled: artifact.OSS,
			Env:        deployService.Artifact.Env,
		}
		mergedPorts, err := mergePorts(serviceConfig.Ports, artifact.Ports, artifact.Name)
		if err != nil {
			return nil, err
		}

		for _, port := range mergedPorts {
			if port == nil {
				return nil, fmt.Errorf("service artifact %s has nil port", artifact.Name)
			}
			compiledArtifact.Ports = append(compiledArtifact.Ports, &deploy.ArtifactPortSpec{
				Name: port.Name,
				Port: int32(port.Port),
			})
		}

		compiledArtifactHTTP, err := compileArtifactHTTP(deployService, mergedPorts, artifact.Name)
		if err != nil {
			return nil, err
		}
		compiledArtifact.Http = compiledArtifactHTTP
		compiledArtifact.WorkloadKind = mapWorkloadKind(serviceConfig.Kind)

		desiredState.Artifacts = append(desiredState.Artifacts, compiledArtifact)
	}

	return desiredState, nil
}

// ResolveArtifactTargets collects all artifact targets referenced by deploy config.
func ResolveArtifactTargets(deployConfig *config.DeployConfig, serviceConfigs map[string]*config.ServiceConfig) ([]string, error) {
	if deployConfig == nil {
		return nil, fmt.Errorf("deploy config is nil")
	}

	artifactTargetSet := make(map[string]struct{})
	for _, deployService := range deployConfig.Services {
		if deployService == nil {
			return nil, fmt.Errorf("deploy service is nil")
		}
		if isInfraService(deployService) {
			continue
		}

		serviceConfig, ok := serviceConfigs[deployService.Artifact.Path]
		if !ok {
			return nil, fmt.Errorf("service config %s not found", deployService.Artifact.Path)
		}

		artifact, err := serviceConfig.GetArtifact(deployService.Artifact.Name)
		if err != nil {
			return nil, fmt.Errorf("service config %s artifact %s not found: %w", deployService.Artifact.Path, deployService.Artifact.Name, err)
		}
		artifactTargetSet[artifact.Target] = struct{}{}
	}

	if len(artifactTargetSet) == 0 {
		return nil, nil
	}

	artifactTargets := make([]string, 0, len(artifactTargetSet))
	for artifactTarget := range artifactTargetSet {
		artifactTargets = append(artifactTargets, artifactTarget)
	}
	sort.Strings(artifactTargets)

	return artifactTargets, nil
}

// ReadServiceConfigs reads all service configs referenced by deploy config.
func ReadServiceConfigs(deployConfig *config.DeployConfig) (map[string]*config.ServiceConfig, error) {
	if deployConfig == nil {
		return nil, fmt.Errorf("deploy config is nil")
	}

	serviceConfigs := make(map[string]*config.ServiceConfig)
	for _, deployService := range deployConfig.Services {
		if deployService == nil {
			return nil, fmt.Errorf("deploy service is nil")
		}
		if isInfraService(deployService) {
			continue
		}
		if _, ok := serviceConfigs[deployService.Artifact.Path]; ok {
			continue
		}

		serviceConfig, err := config.ParseServiceConfig(workspace.ResolveRootPath(deployService.Artifact.Path))
		if err != nil {
			return nil, fmt.Errorf("read service config %s failed: %w", deployService.Artifact.Path, err)
		}
		if _, err := serviceConfig.GetArtifact(deployService.Artifact.Name); err != nil {
			return nil, fmt.Errorf("service config %s missing artifact %s: %w", deployService.Artifact.Path, deployService.Artifact.Name, err)
		}

		serviceConfigs[deployService.Artifact.Path] = serviceConfig
	}

	if len(serviceConfigs) == 0 {
		return nil, nil
	}

	return serviceConfigs, nil
}

func mapWorkloadKind(kind config.WorkloadKind) deploy.WorkloadKind {
	switch kind {
	case config.WorkloadKindStateful:
		return deploy.WorkloadKind_WORKLOAD_KIND_STATEFUL
	default:
		return deploy.WorkloadKind_WORKLOAD_KIND_STATELESS
	}
}

func compileArtifactHTTP(deployService *config.DeployService, mergedPorts []*config.ServiceArtifactPort, artifactName string) (*deploy.ArtifactHTTPSpec, error) {
	if deployService == nil {
		return nil, nil
	}
	if len(deployService.HTTP.Hostnames) == 0 && len(deployService.HTTP.Matches) == 0 {
		return nil, nil
	}

	route := &deploy.ArtifactHTTPSpec{
		Hostnames: append([]string(nil), deployService.HTTP.Hostnames...),
	}

	for _, match := range deployService.HTTP.Matches {
		if match == nil {
			return nil, fmt.Errorf("http match is nil for service %s", artifactName)
		}
		if !artifactHasPort(mergedPorts, match.Backend) {
			return nil, fmt.Errorf("http backend %s not found in service %s", match.Backend, artifactName)
		}
		if err := validateHTTPPathType(match.Path.Type); err != nil {
			return nil, err
		}

		route.Matches = append(route.Matches, &deploy.HTTPRouteRule{
			Backend: match.Backend,
			Path: &deploy.HTTPPathRule{
				Type:  deploy.HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX,
				Value: match.Path.Value,
			},
		})
	}

	return route, nil
}

func artifactHasPort(ports []*config.ServiceArtifactPort, portName string) bool {
	for _, port := range ports {
		if port == nil {
			continue
		}
		if port.Name == portName {
			return true
		}
	}
	return false
}

func validateHTTPPathType(pathType config.HTTPPathMatchType) error {
	switch pathType {
	case config.HTTPPathMatchTypePrefix:
		return nil
	default:
		return fmt.Errorf("unsupported http path type %s", pathType)
	}
}

// mergePorts 合并 service 级 ports 和 artifact 级 ports，返回合并后的列表。
// 如果发现同名 port，返回错误。合并顺序：service 级在前，artifact 级在后。
func mergePorts(servicePorts []*config.ServiceArtifactPort, artifactPorts []*config.ServiceArtifactPort, artifactName string) ([]*config.ServiceArtifactPort, error) {
	nameSet := make(map[string]struct{})
	merged := make([]*config.ServiceArtifactPort, 0, len(servicePorts)+len(artifactPorts))

	for _, port := range servicePorts {
		if port == nil {
			return nil, fmt.Errorf("service port is nil for service artifact %q", artifactName)
		}
		nameSet[port.Name] = struct{}{}
		merged = append(merged, port)
	}

	for _, port := range artifactPorts {
		if port == nil {
			return nil, fmt.Errorf("service artifact %q has nil port", artifactName)
		}
		if _, exists := nameSet[port.Name]; exists {
			return nil, fmt.Errorf("duplicate port name %q between service ports and service artifact %q", port.Name, artifactName)
		}
		merged = append(merged, port)
	}

	return merged, nil
}

func isInfraService(deployService *config.DeployService) bool {
	if deployService == nil {
		return false
	}

	return strings.TrimSpace(deployService.Infra.Resource) != ""
}
