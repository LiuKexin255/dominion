package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"dominion/tools/release/deploy/pkg/schema"
	"dominion/tools/release/deploy/pkg/workspace"

	"github.com/goccy/go-yaml"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// ErrNotFound 未找到
	ErrNotFound = errors.New("未找到")
)

const maxInfraPersistenceCapacity = "1Ti"

type EnvironmentType string
type HTTPPathMatchType string
type WorkloadKind string

const (
	HTTPPathMatchTypePrefix       = "PathPrefix"
	ServiceArtifactTypeDeployment = "deployment"

	EnvironmentTypeProd        = "prod"
	EnvironmentTypeDev         = "dev"
	EnvironmentTypeTest        = "test"
	EnvironmentTypeUnspecified = "unspecified"

	WorkloadKindStateless WorkloadKind = "stateless"
	WorkloadKindStateful  WorkloadKind = "stateful"
)

// DeployConfig 部署配置
type DeployConfig struct {
	Name     string           `yaml:"name"`
	Desc     string           `yaml:"desc"`
	Type     EnvironmentType  `yaml:"type"`
	Services []*DeployService `yaml:"services"`

	// Version 配置版本，如果读取时为空，默认为 "2.0"
	Version string `yaml:"version,omitempty"`

	// URI 资源标识符，如果读取时为空，读取时写入
	URI string `yaml:"uri,omitempty"`
}

type DeployService struct {
	Artifact DeployArtifact `yaml:"artifact,omitempty"`
	Infra    DeployInfra    `yaml:"infra,omitempty"`
	HTTP     DeployHTTP     `yaml:"http,omitempty"`
}

type DeployArtifact struct {
	Path string `yaml:"path"`
	Name string `yaml:"name"`
	// Replicas 指定该产物的部署副本数，未设置时由编译器使用默认值。
	Replicas int `yaml:"replicas,omitempty"`
	// Env 指定该产物的环境变量。
	Env map[string]string `yaml:"env,omitempty"`
}

// DeployInfra 表示基于基础设施的部署定义。
type DeployInfra struct {
	Resource    string                 `yaml:"resource"`
	Profile     string                 `yaml:"profile"`
	Name        string                 `yaml:"name"`
	App         string                 `yaml:"app"`
	Persistence DeployInfraPersistence `yaml:"persistence"`
}

// DeployInfraPersistence 表示基础设施部署的持久化配置。
type DeployInfraPersistence struct {
	Enabled  bool   `yaml:"enabled"`
	Capacity string `yaml:"capacity,omitempty"`
}

type DeployHTTP struct {
	Hostnames []string           `yaml:"hostnames"`
	Matches   []*DeployHTTPMatch `yaml:"matches"`
}

type DeployHTTPMatch struct {
	Backend string              `yaml:"backend"`
	Path    DeployHTTPPathMatch `yaml:"path"`
}

type DeployHTTPPathMatch struct {
	Type  HTTPPathMatchType `yaml:"type"`
	Value string            `yaml:"value"`
}

// ServiceConfig 服务定义配置
type ServiceConfig struct {
	Name      string             `yaml:"name"`
	App       string             `yaml:"app"`
	Desc      string             `yaml:"desc"`
	Artifacts []*ServiceArtifact `yaml:"artifacts"`

	// Ports 所有 artifact 共有的默认端口。
	Ports []*ServiceArtifactPort `yaml:"ports,omitempty"`

	// Version 配置版本，如果读取时为空，默认为 "2.0"
	Version string `yaml:"version,omitempty"`

	// URI 资源标识符，如果读取时为空，读取时写入
	URI string `yaml:"uri,omitempty"`

	Kind WorkloadKind `yaml:"kind,omitempty"`
}

type ServiceArtifact struct {
	Name   string                 `yaml:"name"`
	Target string                 `yaml:"target"`
	TLS    bool                   `yaml:"tls,omitempty"`
	OSS    bool                   `yaml:"oss,omitempty"`
	Ports  []*ServiceArtifactPort `yaml:"ports"`
}

type ServiceArtifactPort struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
}

// GetArtifact 根据产物名称返回产物，如果没有，返回 ErrNotFound
func (c *ServiceConfig) GetArtifact(name string) (*ServiceArtifact, error) {
	for _, artifacts := range c.Artifacts {
		if artifacts.Name == name {
			return artifacts, nil
		}
	}
	return nil, fmt.Errorf("产物 %s %w", name, ErrNotFound)
}

// ParseDeployConfig 解析部署配置
func ParseDeployConfig(filePath string) (*DeployConfig, error) {
	deployRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if err := schema.ValidateDeployYAML(deployRaw); err != nil {
		return nil, err
	}

	c := new(DeployConfig)
	if err := yaml.Unmarshal(deployRaw, c); err != nil {
		return nil, err
	}

	if c.Version == "" {
		c.Version = "2.0"
	}

	configURI, err := workspace.ToURI(filePath)
	if err != nil {
		return nil, fmt.Errorf("解析部署配置 URI 失败: %w", err)
	}
	if c.URI == "" {
		c.URI = configURI
	}

	for _, svc := range c.Services {
		if svc.Artifact.Path != "" || svc.Artifact.Name != "" {
			svc.Artifact.Path = normalizeArtifactPath(svc.Artifact.Path, configURI)
		}
		if svc.Infra.Resource != "" {
			if err := validateDeployInfraPersistence(svc.Infra.Persistence); err != nil {
				return nil, fmt.Errorf("infra %s: %w", svc.Infra.Name, err)
			}
		}
	}

	return c, nil
}

// validateDeployInfraPersistence 校验基础设施部署的持久化容量配置。
// 规则：
//  1. persistence 未启用时不能配置 capacity
//  2. capacity 须为合法的 Kubernetes 资源量
//  3. capacity 不能超过 1Ti
func validateDeployInfraPersistence(p DeployInfraPersistence) error {
	if !p.Enabled && strings.TrimSpace(p.Capacity) != "" {
		return errors.New("persistence 未启用时不能配置 capacity")
	}
	if strings.TrimSpace(p.Capacity) != "" {
		q, err := resource.ParseQuantity(p.Capacity)
		if err != nil {
			return fmt.Errorf("capacity 解析失败: %w", err)
		}
		upperBound := resource.MustParse(maxInfraPersistenceCapacity)
		if q.Cmp(upperBound) > 0 {
			return fmt.Errorf("capacity 超过上限 %s: %s", maxInfraPersistenceCapacity, p.Capacity)
		}
	}
	return nil
}

// ParseServiceConfig 解析服务配置
func ParseServiceConfig(filePath string) (*ServiceConfig, error) {
	serviceRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if err := schema.ValidateServiceYAML(serviceRaw); err != nil {
		return nil, err
	}

	c := new(ServiceConfig)
	if err := yaml.Unmarshal(serviceRaw, c); err != nil {
		return nil, err
	}

	if c.Version == "" {
		c.Version = "2.0"
	}

	configURI, err := workspace.ToURI(filePath)
	if err != nil {
		return nil, fmt.Errorf("解析服务配置 URI 失败: %w", err)
	}
	if c.URI == "" {
		c.URI = configURI
	}

	for _, artifact := range c.Artifacts {
		normalized, err := normalizeArtifactTarget(artifact.Target, configURI)
		if err != nil {
			return nil, fmt.Errorf("标准化产物 target 失败: %w", err)
		}
		artifact.Target = normalized
	}

	if c.Kind == "" {
		c.Kind = WorkloadKindStateless
	}

	return c, nil
}

// uriDir 返回 URI 的目录部分，保留 "//" 前缀。
// 例如 "//a/b/file.yaml" 返回 "//a/b"，"//file.yaml" 返回 "//"。
func uriDir(uri string) string {
	// 去掉 "//" 前缀后取目录，再补回 "//"
	rest := strings.TrimPrefix(uri, workspace.WorkspacePathPrefix)
	dir := path.Dir(rest)
	if dir == "." {
		return workspace.WorkspacePathPrefix
	}
	return workspace.WorkspacePathPrefix + dir
}

// normalizeArtifactTarget 将 artifact target 标准化为 // 开头的完整 URI 格式。
// 短标签（":name"）依据 configURI 的目录部分拼接为完整 URI；
// 已是完整格式（"//pkg:name"）则原样返回。
func normalizeArtifactTarget(target string, configURI string) (string, error) {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "//") {
		return target, nil
	}
	// configURI 形如 "//a/b/service.yaml"，取目录得 "//a/b"
	dir := uriDir(configURI)
	if strings.HasPrefix(target, ":") {
		if len(target) == 1 {
			return "", fmt.Errorf("非法 target 格式: %s", target)
		}
		return dir + target, nil
	}
	pathPart, namePart, ok := strings.Cut(target, ":")
	if !ok || pathPart == "" || namePart == "" || strings.Contains(namePart, ":") {
		return "", fmt.Errorf("非法 target 格式: %s", target)
	}
	baseDir := strings.TrimPrefix(dir, workspace.WorkspacePathPrefix)
	joined := path.Join(baseDir, pathPart)
	if joined == "." {
		joined = ""
	}
	if joined == "" {
		return workspace.WorkspacePathPrefix + ":" + namePart, nil
	}
	return workspace.WorkspacePathPrefix + joined + ":" + namePart, nil
}

// normalizeArtifactPath 将 artifact.path 标准化为 // 开头的 URI 格式。
// 已是 // 前缀则原样返回；相对路径基于 configURI 目录拼接后规范化。
func normalizeArtifactPath(artifactPath string, configURI string) string {
	trimmed := strings.TrimSpace(artifactPath)
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	// configURI 形如 "//a/b/deploy.yaml"，取目录得 "//a/b"
	dir := uriDir(configURI)
	return dir + "/" + trimmed
}
