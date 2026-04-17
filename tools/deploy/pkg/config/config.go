package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"dominion/tools/deploy/pkg/schema"
	"dominion/tools/deploy/pkg/workspace"

	"github.com/goccy/go-yaml"
)

var (
	// ErrNotFound 未找到
	ErrNotFound = errors.New("未找到")
)

type EnvironmentType string
type HTTPPathMatchType string
type ServiceArtifactType string

const (
	HTTPPathMatchTypePrefix       = "PathPrefix"
	ServiceArtifactTypeDeployment = "deployment"

	EnvironmentTypeProd        = "prod"
	EnvironmentTypeDev         = "dev"
	EnvironmentTypeTest        = "test"
	EnvironmentTypeUnspecified = "unspecified"
)

// Validate 校验服务产物类型是否合法。
func (t ServiceArtifactType) Validate() error {
	switch t {
	case ServiceArtifactTypeDeployment:
		return nil
	default:
		return fmt.Errorf("不支持的产物类型 %s", t)
	}
}

// DeployConfig 部署配置
type DeployConfig struct {
	Name     string           `yaml:"name"`
	Desc     string           `yaml:"desc"`
	Type     EnvironmentType  `yaml:"type"`
	Services []*DeployService `yaml:"services"`

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
	Enabled bool `yaml:"enabled"`
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

	// URI 资源标识符，如果读取时为空，读取时写入
	URI string `yaml:"uri,omitempty"`
}

type ServiceArtifact struct {
	Name   string                 `yaml:"name"`
	Type   ServiceArtifactType    `yaml:"type"`
	Target string                 `yaml:"target"`
	TLS    bool                   `yaml:"tls,omitempty"`
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
	}

	return c, nil
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

	configURI, err := workspace.ToURI(filePath)
	if err != nil {
		return nil, fmt.Errorf("解析服务配置 URI 失败: %w", err)
	}
	if c.URI == "" {
		c.URI = configURI
	}

	for _, artifact := range c.Artifacts {
		if err := artifact.Type.Validate(); err != nil {
			return nil, err
		}
		normalized, err := normalizeArtifactTarget(artifact.Target, configURI)
		if err != nil {
			return nil, fmt.Errorf("标准化产物 target 失败: %w", err)
		}
		artifact.Target = normalized
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
