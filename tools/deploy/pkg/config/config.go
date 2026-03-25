package config

import (
	"bytes"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	// deployValidater 部署配置校验器
	deployValidater *YAMLValidater
	// serviceValidater 服务配置校验器
	serviceValidater *YAMLValidater
)

// RegisterDeployValidater 注册部署配置校验器
func RegisterDeployValidater(validater *YAMLValidater) {
	deployValidater = validater
}

// RegisterServiceValidater 注册服务配置校验器
func RegisterServiceValidater(validater *YAMLValidater) {
	serviceValidater = validater
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

// ParseDeployConfig 解析部署配置
func ParseDeployConfig(path string) (*DeployConfig, error) {
	deployRaw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := deployValidater.Vaild(deployRaw); err != nil {
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

	if err := serviceValidater.Vaild(serviceRaw); err != nil {
		return nil, err
	}

	c := new(ServiceConfig)
	if err := yaml.Unmarshal(serviceRaw, c); err != nil {
		return nil, err
	}

	return c, nil
}

// YAMLValidater yaml 格式校验器 Vaildater
type YAMLValidater struct {
	schema *jsonschema.Schema
}

// Vaild 返回 error 如果 raw 存在格式问题
func (v *YAMLValidater) Vaild(raw []byte) error {
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

// NewYAMLValidater 创建 YAML 校验器
func NewYAMLValidater(path string) (*YAMLValidater, error) {
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

	return &YAMLValidater{
		schema: schema,
	}, nil
}
