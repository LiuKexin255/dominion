package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

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
	Path DeployHTTPPathMatch `yaml:"path"`
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
func ParseDeployConfig(path string) (*DeployConfig, error) {
	deployRaw, err := os.ReadFile(path)
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

	return c, nil
}

// ParseServiceConfig 解析服务配置
func ParseServiceConfig(path string) (*ServiceConfig, error) {
	serviceRaw, err := os.ReadFile(path)
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
