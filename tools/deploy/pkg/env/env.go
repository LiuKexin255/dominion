package env

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/workspace"

	"github.com/goccy/go-yaml"
)

var (
	// envProfileDir 环境信息缓存路径
	envProfileDir = ".env"

	// deployConfigDir 环境配置缓存路径
	deployConfigDir = path.Join(envProfileDir, "deploy")
	// deployConfigFileName 环境配置文件名 {应用名}__{环境名}___{模板应用名}__{模板名}
	deployConfigFileName = "%s__%s__%s__%s.yaml"

	// deployConfigDir 环境配置缓存路径
	serviceConfigDir = path.Join(envProfileDir, "service")

	currentEnvFileName = "current.json"
	currentEnvPath     = path.Join(envProfileDir, currentEnvFileName)

	// profileFormat 环境信息文件格式 {应用名}_{环境名}.json
	profileFormat = "%s__%s.json"

	now = time.Now
)

var (
	// ErrNotFound 环境未找到
	ErrNotFound = errors.New("环境未找到")
	// ErrNotActive 没有激活中的环境
	ErrNotActive = errors.New("没有激活中的环境")
	// ErrIsExist 环境已存在
	ErrIsExist = errors.New("没有激活中的环境")

	// LazyInit 初始化所需操作
	LazyInit = sync.OnceFunc(internalInit)
)

func internalInit() {
	// 创建保存文件所需目录
	for _, dir := range []string{
		deployConfigDir,
		serviceConfigDir,
	} {
		if err := os.MkdirAll(path.Join(workspace.MustRoot(), dir), os.ModePerm); err != nil {
			panic(err)
		}
	}
}

// Profile 环境基本信息
type Profile struct {
	Name      string    `json:"name"`
	App       string    `json:"app"`
	UpdatedAt time.Time `json:"updated_at"`

	MainConfig    string   `json:"main_config,omitempty"`
	DeployConfigs []string `json:"deploy_configs,omitempty"`
}

// DeployEnv 部署环境
type DeployEnv struct {
	Profile

	mainDeployConfig *config.DeployConfig
	deployConfigs    []*config.DeployConfig
}

// Update 更新环境
func (e *DeployEnv) Update(deployConfig *config.DeployConfig) error {
	e.mainDeployConfig = deployConfig
	e.UpdatedAt = now()

	return e.save()
}

// Active 设置该环境为当前环境
func (e *DeployEnv) Active() error {
	return saveCurrentEnvInfo(&currentEnvInfo{
		Name: e.Name,
		App:  e.App,
	})
}

// Delete 删除环境
func (e *DeployEnv) Delete() error {
	// 如果当前激活环境为自身，移除缓存
	cur, err := Current()
	if err != nil {
		if !errors.Is(err, ErrNotActive) {
			return err
		}
	}

	if cur != nil && e.Equal(cur) {
		if err := deleteCurrentEnvInfo(); err != nil {
			return err
		}
	}

	if err := e.deleteDeployConfigs(); err != nil {
		return err
	}

	return os.RemoveAll(path.Join(workspace.MustRoot(), envProfileDir, profileName(e.Name, e.App)))
}

// save 保存环境信息
func (e *DeployEnv) save() error {
	if err := e.saveDeployConfigs(); err != nil {
		return err
	}

	if err := e.saveProfile(); err != nil {
		return err
	}

	return nil

}

func (e *DeployEnv) saveProfile() error {
	// 序列化并保存 profile 文件
	profileRaw, err := json.Marshal(&e.Profile)
	if err != nil {
		return err
	}
	filePath := path.Join(workspace.MustRoot(), envProfileDir, profileName(e.Name, e.App))

	return os.WriteFile(filePath, profileRaw, os.ModePerm)
}

func (e *DeployEnv) saveDeployConfigs() error {
	// 序列化配置文件
	// 单独设置主配置文件名
	if e.mainDeployConfig == nil {
		return nil
	}

	e.MainConfig = fmt.Sprintf(deployConfigFileName, e.App, e.Name, e.mainDeployConfig.App, e.mainDeployConfig.Template)
	return saveDeployConfig(e.MainConfig, e.mainDeployConfig)
}

func (e *DeployEnv) loadDeployConfigs() error {
	if e.MainConfig == "" {
		return nil
	}

	mainDeployConfig, err := loadDeployConfig(e.MainConfig)
	if err != nil {
		return err
	}
	e.mainDeployConfig = mainDeployConfig
	return nil
}

func (e *DeployEnv) deleteDeployConfigs() error {
	if e.MainConfig == "" {
		return nil
	}
	return deleteDeployConfig(e.MainConfig)
}

func (e *DeployEnv) Equal(other *DeployEnv) bool {
	if other == nil {
		return false
	}
	return e.Name == other.Name && e.App == other.App
}

func saveDeployConfig(name string, deployConfig *config.DeployConfig) error {
	configRaw, err := yaml.Marshal(deployConfig)
	if err != nil {
		return err
	}

	filePath := path.Join(workspace.MustRoot(), deployConfigDir, name)
	return os.WriteFile(filePath, configRaw, os.ModePerm)
}

func loadDeployConfig(name string) (*config.DeployConfig, error) {
	deployConfigRaw, err := os.ReadFile(path.Join(workspace.MustRoot(), deployConfigDir, name))
	if err != nil {
		return nil, err
	}

	deployConfig := new(config.DeployConfig)
	if err := yaml.Unmarshal(deployConfigRaw, deployConfig); err != nil {
		return nil, err
	}
	return deployConfig, nil
}

func deleteDeployConfig(name string) error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), deployConfigDir, name))
}

// New 创建新的环境
func New(name string, app string) (*DeployEnv, error) {
	// 检查是否有同名服务
	env, err := Get(name, app)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	if err == nil && env != nil {
		return nil, ErrIsExist
	}

	// 未找到环境
	env = &DeployEnv{
		Profile: Profile{
			Name:      name,
			App:       app,
			UpdatedAt: now(),
		},
	}
	if err := env.save(); err != nil {
		return nil, err
	}

	return env, nil
}

// Get 获取指定环境
func Get(name string, app string) (*DeployEnv, error) {
	filePath := path.Join(workspace.MustRoot(), envProfileDir, profileName(name, app))
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s/%s %w", name, app, ErrNotFound)
		}
		return nil, err
	}

	return loadDeployEnv(filePath)
}

// Current 返回当前激活中的环境
func Current() (*DeployEnv, error) {
	info, err := loadCurrentEnvInfo()
	if err != nil {
		return nil, err
	}

	return Get(info.Name, info.App)
}

// loadDeployEnv 根据 profile 文件载入环境对象
func loadDeployEnv(filePath string) (*DeployEnv, error) {
	profileRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	profile := new(Profile)
	if err := json.Unmarshal(profileRaw, profile); err != nil {
		return nil, err
	}

	env := &DeployEnv{
		Profile: *profile,
	}

	if err := env.loadDeployConfigs(); err != nil {
		return nil, err
	}

	return env, nil
}

func profileName(name string, app string) string {
	return fmt.Sprintf(profileFormat, app, name)
}

type currentEnvInfo struct {
	Name string `json:"name"`
	App  string `json:"app"`
}

func saveCurrentEnvInfo(info *currentEnvInfo) error {
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(workspace.MustRoot(), currentEnvPath), raw, os.ModePerm)
}

func deleteCurrentEnvInfo() error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), currentEnvPath))
}

func loadCurrentEnvInfo() (*currentEnvInfo, error) {
	raw, err := os.ReadFile(path.Join(workspace.MustRoot(), currentEnvPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotActive
		}
		return nil, err
	}

	info := new(currentEnvInfo)
	if err := json.Unmarshal(raw, info); err != nil {
		return nil, err
	}

	return info, nil
}
