package k8s

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
)

// staticConfigFileName 是内置静态配置文件名。
const staticConfigFileName = "static_config.yaml"

// staticConfigFS 保存内置静态配置文件。
//
//go:embed static_config.yaml
var staticConfigFS embed.FS

var (
	// loadK8sConfigOnce 确保静态配置只加载一次。
	loadK8sConfigOnce sync.Once
	// loadedK8sConfig 缓存已加载的静态配置。
	loadedK8sConfig *K8sConfig
)

// GatewayConfig 定义网关资源的命名信息。
type GatewayConfig struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

// K8sConfig 定义部署流程使用的静态 Kubernetes 配置。
type K8sConfig struct {
	Namespace string        `yaml:"namespace"`
	ManagedBy string        `yaml:"managed_by"`
	Gateway   GatewayConfig `yaml:"gateway"`
}

// LoadK8sConfig 加载并缓存静态 Kubernetes 配置。
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

// Validate 校验静态 Kubernetes 配置字段。
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
