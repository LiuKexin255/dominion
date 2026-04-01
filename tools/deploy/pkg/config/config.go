package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"dominion/tools/deploy/pkg/workspace"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	// deployValidator 部署配置校验器
	deployValidator *YAMLValidator
	// serviceValidator 服务配置校验器
	serviceValidator *YAMLValidator
	// ErrNotFound 未找到
	ErrNotFound = errors.New("未找到")
)

// RegisterDeployValidator 注册部署配置校验器
func RegisterDeployValidator(Validator *YAMLValidator) {
	deployValidator = Validator
}

// RegisterServiceValidator 注册服务配置校验器
func RegisterServiceValidator(Validator *YAMLValidator) {
	serviceValidator = Validator
}

type HTTPPathMatchType string

const (
	HTTPPathMatchTypePrefix = "PathPrefix"
)

type ServiceArtifactType string

const (
	ServiceArtifactTypeDeployment = "deployment"
)

// DeployConfig 部署配置
type DeployConfig struct {
	Template string           `yaml:"template"`
	App      string           `yaml:"app"`
	Desc     string           `yaml:"desc"`
	Services []*DeployService `yaml:"services"`

	// URI 资源标识符，如果读取时为空，读取时写入
	URI string `yaml:"uri,omitempty"`
}

type DeployService struct {
	Artifact DeployArtifact `yaml:"artifact"`
	HTTP     DeployHTTP     `yaml:"http,omitempty"`
}

type DeployArtifact struct {
	Path string `yaml:"path"`
	Name string `yaml:"name"`
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
	if err := deployValidator.Vaild(deployRaw); err != nil {
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
		svc.Artifact.Path = normalizeArtifactPath(svc.Artifact.Path, configURI)
	}

	return c, nil
}

// ParseServiceConfig 解析服务配置
func ParseServiceConfig(filePath string) (*ServiceConfig, error) {
	serviceRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if err := serviceValidator.Vaild(serviceRaw); err != nil {
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
		normalized, err := normalizeArtifactTarget(artifact.Target, configURI)
		if err != nil {
			return nil, fmt.Errorf("标准化产物 target 失败: %w", err)
		}
		artifact.Target = normalized
	}

	return c, nil
}

// YAMLValidator yaml 格式校验器 Vaildater
type YAMLValidator struct {
	schema *jsonschema.Schema
}

// Vaild 返回 error 如果 raw 存在格式问题
func (v *YAMLValidator) Vaild(raw []byte) error {
	jsonRaw, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return err
	}

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonRaw))
	if err != nil {
		return err
	}

	return v.schema.Validate(inst)
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
	if !strings.HasPrefix(target, ":") {
		return "", fmt.Errorf("非法 target 格式: %s", target)
	}
	// configURI 形如 "//a/b/service.yaml"，取目录得 "//a/b"
	dir := uriDir(configURI)
	return dir + target, nil
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

// NewYAMLValidator 创建 YAML 校验器
func NewYAMLValidator(path string) (*YAMLValidator, error) {
	schemaRaw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	schemaJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaRaw))
	if err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("/schema.yaml", schemaJSON); err != nil {
		return nil, err
	}

	schema, err := compiler.Compile("/schema.yaml")
	if err != nil {
		return nil, err
	}

	return &YAMLValidator{
		schema: schema,
	}, nil
}
