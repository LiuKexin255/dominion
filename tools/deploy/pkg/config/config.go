package config

// DeployConfig 部署配置
type DeployConfig struct {
	App      string           `yaml:"app"`
	Desc     string           `yaml:"desc"`
	Services []*DeployService `yaml:"services"`
}

type DeployService struct {
	Artifact DeployArtifact `yaml:"artifact"`
	Http     DeployHttp     `yaml:"http"`
}

type DeployArtifact struct {
	Path string `yaml:"path"`
	Name string `yaml:"name"`
}

type DeployHttp struct {
	Hostnames []string           `yaml:"hostnames"`
	Matches   []*DeployHttpMatch `yaml:"matches"`
}

type DeployHttpMatch struct {
	Path *DeployHttpPathMatch `yaml:"path"`
}

type DeployHttpPathMatch struct {
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

// ServiceConfig 服务定义配置
type ServiceConfig struct {
	Name      string            `yaml:"name"`
	App       string            `yaml:"app"`
	Desc      string            `yaml:"desc"`
	Artifacts []*ServiceArtifact `yaml:"artifacts"`
}

type ServiceArtifact struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Target string `yaml:"target"`
}
