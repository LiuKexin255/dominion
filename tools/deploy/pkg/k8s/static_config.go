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
	// readStaticConfigFile 读取内置静态配置文件，允许测试替换。
	readStaticConfigFile = func() ([]byte, error) {
		return staticConfigFS.ReadFile(staticConfigFileName)
	}
	// loadK8sConfigFunc 执行静态配置加载，允许测试替换。
	loadK8sConfigFunc = loadStaticK8sConfig
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

// ConfigMapConfig 定义 ConfigMap 及其键名引用。
type ConfigMapConfig struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

// TLSConfig 定义静态 TLS 配置。
type TLSConfig struct {
	Secret      string          `yaml:"secret"`
	Domain      string          `yaml:"domain"`
	CAConfigMap ConfigMapConfig `yaml:"ca_config_map"`
}

// MongoStorageConfig 定义 Mongo 默认持久化配置。
type MongoStorageConfig struct {
	StorageClassName string   `yaml:"storage_class_name"`
	Capacity         string   `yaml:"capacity"`
	AccessModes      []string `yaml:"access_modes"`
	VolumeMode       string   `yaml:"volume_mode"`
}

// MongoSecurityConfig 定义 Mongo workload 的安全上下文配置。
type MongoSecurityConfig struct {
	RunAsUser  int64 `yaml:"run_as_user"`
	RunAsGroup int64 `yaml:"run_as_group"`
}

// MongoProfileConfig 定义 Mongo 内置 profile 的默认参数。
type MongoProfileConfig struct {
	Image         string              `yaml:"image"`
	Version       string              `yaml:"version"`
	Port          int                 `yaml:"port"`
	AdminUsername string              `yaml:"admin_username"`
	Security      MongoSecurityConfig `yaml:"security"`
	Storage       MongoStorageConfig  `yaml:"storage"`
}

// K8sConfig 定义部署流程使用的静态 Kubernetes 配置。
type K8sConfig struct {
	Namespace string                         `yaml:"namespace"`
	ManagedBy string                         `yaml:"managed_by"`
	Gateway   GatewayConfig                  `yaml:"gateway"`
	TLS       TLSConfig                      `yaml:"tls,omitempty"`
	MongoDB   map[string]*MongoProfileConfig `yaml:"mongodb,omitempty"`
}

// LoadK8sConfig 加载并缓存静态 Kubernetes 配置。
func LoadK8sConfig() *K8sConfig {
	loadK8sConfigOnce.Do(func() {
		loadedK8sConfig = loadK8sConfigFunc()
	})

	return loadedK8sConfig
}

func loadStaticK8sConfig() *K8sConfig {
	raw, err := readStaticConfigFile()
	if err != nil {
		panic(fmt.Errorf("读取静态配置失败: %w", err))
	}

	cfg, parseErr := parseK8sConfig(raw)
	if parseErr != nil {
		panic(parseErr)
	}

	return cfg
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

// MongoProfile 加载静态配置中的 Mongo profile。
// 当 profile 不存在时返回 nil。
func (c *K8sConfig) MongoProfile(profile string) *MongoProfileConfig {
	if c == nil {
		return nil
	}

	profile = strings.TrimSpace(profile)
	if profile == "" || c.MongoDB == nil {
		return nil
	}

	return c.MongoDB[profile]
}

// Validate 校验 Mongo profile 字段是否合法。
func (c *MongoProfileConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("mongo profile 为空")
	}

	if strings.TrimSpace(c.Image) == "" {
		return fmt.Errorf("mongo profile 缺少 image")
	}
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("mongo profile 缺少 version")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("mongo profile port 非法")
	}
	if strings.TrimSpace(c.AdminUsername) == "" {
		return fmt.Errorf("mongo profile 缺少 admin_username")
	}
	if c.Security.RunAsUser <= 0 {
		return fmt.Errorf("mongo profile security.run_as_user 非法")
	}
	if c.Security.RunAsGroup <= 0 {
		return fmt.Errorf("mongo profile security.run_as_group 非法")
	}
	if strings.TrimSpace(c.Storage.StorageClassName) == "" {
		return fmt.Errorf("mongo profile 缺少 storage.storage_class_name")
	}
	if strings.TrimSpace(c.Storage.Capacity) == "" {
		return fmt.Errorf("mongo profile 缺少 storage.capacity")
	}
	if len(c.Storage.AccessModes) == 0 {
		return fmt.Errorf("mongo profile 缺少 storage.access_modes")
	}
	if strings.TrimSpace(c.Storage.VolumeMode) == "" {
		return fmt.Errorf("mongo profile 缺少 storage.volume_mode")
	}

	return nil
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
	for profileName, profileCfg := range c.MongoDB {
		if profileCfg == nil {
			return fmt.Errorf("静态配置缺少 mongodb.%s", profileName)
		}
		if err := profileCfg.Validate(); err != nil {
			return fmt.Errorf("静态配置 mongodb.%s 无效: %w", profileName, err)
		}
	}

	secret := strings.TrimSpace(c.TLS.Secret)
	domain := strings.TrimSpace(c.TLS.Domain)
	caConfigMapName := strings.TrimSpace(c.TLS.CAConfigMap.Name)
	caConfigMapKey := strings.TrimSpace(c.TLS.CAConfigMap.Key)
	if secret == "" && domain == "" && caConfigMapName == "" && caConfigMapKey == "" {
		return nil
	}
	if secret == "" {
		return fmt.Errorf("静态配置缺少 tls.secret")
	}
	if domain == "" {
		return fmt.Errorf("静态配置缺少 tls.domain")
	}
	if caConfigMapName == "" {
		return fmt.Errorf("静态配置缺少 tls.ca_config_map.name")
	}
	if caConfigMapKey == "" {
		return fmt.Errorf("静态配置缺少 tls.ca_config_map.key")
	}

	return nil
}
