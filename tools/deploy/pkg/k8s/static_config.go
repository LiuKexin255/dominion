package k8s

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
)

const staticConfigFileName = "static_config.yaml"

//go:embed static_config.yaml
var staticConfigFS embed.FS

var (
	loadK8sConfigOnce sync.Once
	loadedK8sConfig   *K8sConfig
)

type GatewayConfig struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type K8sConfig struct {
	Namespace string        `yaml:"namespace"`
	ManagedBy string        `yaml:"managed_by"`
	Gateway   GatewayConfig `yaml:"gateway"`
}

func LoadK8sConfig() *K8sConfig {
	loadK8sConfigOnce.Do(func() {
		raw, err := staticConfigFS.ReadFile(staticConfigFileName)
		if err != nil {
			panic(fmt.Errorf("读取静态配置失败: %w", err))
		}

		cfg, parseErr := parseK8sConfig(raw)
		if parseErr != nil {
			panic(parseErr)
		}

		loadedK8sConfig = cfg
	})

	return loadedK8sConfig
}

func parseK8sConfig(raw []byte) (*K8sConfig, error) {
	cfg := new(K8sConfig)
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("解析静态配置失败: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *K8sConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("静态配置为空")
	}

	if strings.TrimSpace(c.Namespace) == "" {
		return fmt.Errorf("静态配置缺少 namespace")
	}
	if strings.TrimSpace(c.ManagedBy) == "" {
		return fmt.Errorf("静态配置缺少 managed_by")
	}
	if strings.TrimSpace(c.Gateway.Name) == "" {
		return fmt.Errorf("静态配置缺少 gateway.name")
	}
	if strings.TrimSpace(c.Gateway.Namespace) == "" {
		return fmt.Errorf("静态配置缺少 gateway.namespace")
	}

	return nil
}
