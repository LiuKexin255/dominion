package env

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/k8s"
	"dominion/tools/deploy/pkg/workspace"

	"github.com/goccy/go-yaml"
)

// RemoteStatus 表示环境的远程部署状态类型。
type RemoteStatus string

const (
	RemoteStatusPending  RemoteStatus = "pending"
	RemoteStatusDeployed RemoteStatus = "deployed"
)

var (

	// envProfileDir 环境信息缓存路径
	envProfileDir = ".env"

	// deployConfigDir 环境配置缓存路径
	deployConfigDir = path.Join(envProfileDir, "deploy")
	// deployConfigFileName 环境配置文件名 {应用名}__{环境名}___{模板应用名}__{模板名}
	deployConfigFileName = "%s__%s__%s__%s.yaml"

	// serviceConfigDir 环境服务配置缓存路径
	serviceConfigDir = path.Join(envProfileDir, "service")
	// serviceConfigFileName 环境配置文件名 {应用名}__{环境名}
	serviceConfigFileName = "%s__%s.yaml"

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

// executor 定义环境层依赖的远端执行接口。
type executor interface {
	Apply(ctx context.Context, objects *k8s.DeployObjects) error
	Delete(ctx context.Context, app, environment string) error
}

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
	Name         string       `json:"name"`
	App          string       `json:"app"`
	UpdatedAt    time.Time    `json:"updated_at"`
	RemoteStatus RemoteStatus `json:"remote_status,omitempty"`

	MainConfig string `json:"main_config,omitempty"`
}

// DeployEnv 部署环境
type DeployEnv struct {
	Profile

	mainDeployConfig *config.DeployConfig
	serviceConfigs   []*config.ServiceConfig
}

// Update 更新环境
func (e *DeployEnv) Update(deployConfig *config.DeployConfig) error {
	return e.persistLocalDesiredState(deployConfig)
}

// Deploy 使用已缓存的期望配置执行远程部署，成功后仅更新 profile 状态为 deployed。
func (e *DeployEnv) Deploy(ctx context.Context, exec executor) error {
	if exec == nil {
		return fmt.Errorf("executor is nil")
	}
	if e.mainDeployConfig == nil {
		return fmt.Errorf("no cached deploy config, call Update first")
	}

	objects, err := e.BuildDeployObjects()
	if err != nil {
		return err
	}

	if err := exec.Apply(ctx, objects); err != nil {
		return err
	}

	e.RemoteStatus = RemoteStatusDeployed
	e.UpdatedAt = now()
	return e.saveProfile()
}

func (e *DeployEnv) persistLocalDesiredState(deployConfig *config.DeployConfig) error {
	if err := e.prepareDesiredState(deployConfig); err != nil {
		return err
	}

	e.RemoteStatus = RemoteStatusPending
	e.UpdatedAt = now()
	return e.save()
}

func (e *DeployEnv) prepareDesiredState(deployConfig *config.DeployConfig) error {
	if deployConfig == nil {
		return fmt.Errorf("部署配置为空")
	}

	serviceConfigs, err := readServiceConfigs(deployConfig)
	if err != nil {
		return err
	}

	e.mainDeployConfig = deployConfig
	e.serviceConfigs = serviceConfigs
	return nil
}

// Active 设置该环境为当前环境
func (e *DeployEnv) Active() error {
	return saveDeployContext(&DeployContext{
		ActiveEnv: &EnvRef{
			Name: e.Name,
			App:  e.App,
		},
		LastApp: e.App,
	})
}

// Delete 删除环境
func (e *DeployEnv) Delete(ctx context.Context, exec executor) error {
	if exec == nil {
		return fmt.Errorf("executor is nil")
	}

	// 先执行远程删除（remote-first）
	if err := exec.Delete(ctx, e.App, e.Name); err != nil {
		return err
	}

	ctxInfo, err := loadDeployContext()
	if err != nil {
		return err
	}
	if ctxInfo != nil && ctxInfo.ActiveEnv != nil && ctxInfo.ActiveEnv.Name == e.Name && ctxInfo.ActiveEnv.App == e.App {
		ctxInfo.ActiveEnv = nil
		if err := saveDeployContext(ctxInfo); err != nil {
			return err
		}
	}

	if err := e.deleteDeployConfigs(); err != nil {
		return err
	}

	if err := e.deleteServiceConfigs(); err != nil {
		return err
	}

	return os.RemoveAll(path.Join(workspace.MustRoot(), envProfileDir, profileName(e.Name, e.App)))
}

// save 保存环境信息
func (e *DeployEnv) save() error {
	if err := e.saveServiceConfigs(); err != nil {
		return err
	}

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

func (e *DeployEnv) saveServiceConfigs() error {
	raw, err := yaml.Marshal(e.serviceConfigs)
	if err != nil {
		return err
	}

	filePath := path.Join(workspace.MustRoot(), serviceConfigDir, fmt.Sprintf(serviceConfigFileName, e.App, e.Name))
	return os.WriteFile(filePath, raw, os.ModePerm)
}

func (e *DeployEnv) loadServiceConfigs() error {
	raw, err := os.ReadFile(path.Join(workspace.MustRoot(), serviceConfigDir, fmt.Sprintf(serviceConfigFileName, e.App, e.Name)))
	if err != nil {
		return err
	}

	var serviceConfigs []*config.ServiceConfig
	if err := yaml.Unmarshal(raw, &serviceConfigs); err != nil {
		return err
	}

	e.serviceConfigs = serviceConfigs
	return nil
}

func (e *DeployEnv) deleteServiceConfigs() error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), serviceConfigDir, fmt.Sprintf(serviceConfigFileName, e.App, e.Name)))
}

func (e *DeployEnv) Equal(other *DeployEnv) bool {
	if other == nil {
		return false
	}
	return e.Name == other.Name && e.App == other.App
}

// BuildDeployObjects 根据当前环境缓存配置构建部署对象。
func (e *DeployEnv) BuildDeployObjects() (*k8s.DeployObjects, error) {
	return k8s.NewDeployObjects(e.mainDeployConfig, e.serviceConfigs, e.Name)
}

// func (e *DeployEnv) String() string {
// 	return fmt.Sprintf("env: %s, app: %s", e.Name, e.App)
// }

func readServiceConfigs(deployConfig *config.DeployConfig) ([]*config.ServiceConfig, error) {
	serviceConfigs := make([]*config.ServiceConfig, 0, len(deployConfig.Services))
	for _, service := range deployConfig.Services {
		serviceConfig, err := config.ParseServiceConfig(workspace.ResolveRootPath(service.Artifact.Path))
		if err != nil {
			return nil, fmt.Errorf("读取服务配置 %s 失败: %w", service.Artifact.Path, err)
		}

		if _, err := serviceConfig.GetArtifact(service.Artifact.Name); err != nil {
			return nil, fmt.Errorf("服务配置 %s 中不存在产物 %s: %w", service.Artifact.Path, service.Artifact.Name, err)
		}

		serviceConfigs = append(serviceConfigs, serviceConfig)
	}

	return serviceConfigs, nil
}

func saveDeployConfig(fileName string, deployConfig *config.DeployConfig) error {
	configRaw, err := yaml.Marshal(deployConfig)
	if err != nil {
		return err
	}

	filePath := path.Join(workspace.MustRoot(), deployConfigDir, fileName)
	return os.WriteFile(filePath, configRaw, os.ModePerm)
}

func loadDeployConfig(fileName string) (*config.DeployConfig, error) {
	deployConfig, err := config.ParseDeployConfig(path.Join(workspace.MustRoot(), deployConfigDir, fileName))
	if err != nil {
		return nil, err
	}

	return deployConfig, nil
}

func deleteDeployConfig(fileName string) error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), deployConfigDir, fileName))
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
	ctx, err := loadDeployContext()
	if err != nil {
		return nil, err
	}
	if ctx == nil || ctx.ActiveEnv == nil || ctx.ActiveEnv.Name == "" || ctx.ActiveEnv.App == "" {
		return nil, ErrNotActive
	}

	return Get(ctx.ActiveEnv.Name, ctx.ActiveEnv.App)
}
  
func DefaultApp() (string, error) {
	ctx, err := loadDeployContext()
	if err != nil {
		return "", err
	}
	if ctx != nil && strings.TrimSpace(ctx.LastApp) != "" {
		return ctx.LastApp, nil
	}

	return "", fmt.Errorf("未指定 --app，且当前没有可用 app，请先执行 `%s <env>`", "use")
}

// List 返回当前所有环境
func List() ([]*DeployEnv, error) {
	entries, err := os.ReadDir(path.Join(workspace.MustRoot(), envProfileDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	profileNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == currentEnvFileName || !strings.HasSuffix(name, ".json") {
			continue
		}
		profileNames = append(profileNames, name)
	}

	if len(profileNames) == 0 {
		return nil, nil
	}

	sort.Strings(profileNames)

	envs := make([]*DeployEnv, 0, len(profileNames))
	for _, name := range profileNames {
		env, err := loadDeployEnv(path.Join(workspace.MustRoot(), envProfileDir, name))
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}

	return envs, nil
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

	if env.mainDeployConfig != nil {
		if err := env.loadServiceConfigs(); err != nil {
			return nil, err
		}
	}

	return env, nil
}

func profileName(name string, app string) string {
	return fmt.Sprintf(profileFormat, app, name)
}

// EnvRef 表示一个可激活的环境引用。
type EnvRef struct {
	Name string `json:"name"`
	App  string `json:"app"`
}

// DeployContext 表示 current.json 中保存的部署上下文。
type DeployContext struct {
	ActiveEnv *EnvRef `json:"active_env,omitempty"`
	LastApp   string  `json:"last_app,omitempty"`
}

func saveDeployContext(ctx *DeployContext) error {
	if ctx == nil {
		ctx = &DeployContext{}
	}

	raw, err := json.Marshal(ctx)
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(workspace.MustRoot(), currentEnvPath), raw, os.ModePerm)
}

func loadDeployContext() (*DeployContext, error) {
	raw, err := os.ReadFile(path.Join(workspace.MustRoot(), currentEnvPath))
	if err != nil {
		if os.IsNotExist(err) {
			return &DeployContext{}, nil
		}
		return nil, err
	}

	ctx := new(DeployContext)
	if err := json.Unmarshal(raw, ctx); err != nil {
		return nil, fmt.Errorf("load deploy context from %s: %w", currentEnvPath, err)
	}

	return ctx, nil
}

func deleteCurrentEnvInfo() error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), currentEnvPath))
}
