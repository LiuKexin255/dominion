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
	"dominion/tools/deploy/pkg/imagepush"
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
	// cacheDir 环境信息缓存路径
	cacheDir = ".env"

	// deployConfigDir 环境配置缓存路径
	deployConfigDir = path.Join(cacheDir, "deploy")

	// serviceConfigDir 环境服务配置缓存路径
	serviceConfigDir = path.Join(cacheDir, "service")

	profileDir     = path.Join(cacheDir, "profile")
	currentEnvPath = path.Join(cacheDir, "current.json")

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
	LazyInit = lazyInit

	initOnce sync.Once
	initErr  error
)

// executor 定义环境层依赖的远端执行接口。
type executor interface {
	Apply(ctx context.Context, objects *k8s.DeployObjects) error
	Delete(ctx context.Context, environment string) error
}

// imageResolver 定义 env 层镜像解析依赖，负责把部署引用转换为最终镜像引用。
type imageResolver interface {
	Resolve(ctx context.Context, deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig) (map[string]string, error)
}

// newImageResolver 返回默认镜像解析器，测试可覆盖该工厂注入桩实现。
var newImageResolver = func() imageResolver {
	return &defaultImageResolver{
		newRunner: imagepush.NewRunner,
	}
}

// defaultImageResolver 使用 imagepush.Runner 解析部署所需的镜像引用。
type defaultImageResolver struct {
	newRunner func() (imagepush.Runner, error)
}

func (r *defaultImageResolver) Resolve(ctx context.Context, deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig) (map[string]string, error) {
	if r == nil || r.newRunner == nil {
		return nil, fmt.Errorf("image resolver runner factory is nil")
	}

	runner, err := r.newRunner()
	if err != nil {
		return nil, err
	}

	return resolveImages(ctx, imagepush.NewResolver(runner), deployConfig, serviceConfigs)
}

func lazyInit() error {
	initOnce.Do(func() {
		initErr = internalInit()
	})
	return initErr
}

func internalInit() error {
	// 创建保存文件所需目录
	for _, dir := range []string{
		deployConfigDir,
		serviceConfigDir,
	} {
		if err := os.MkdirAll(path.Join(workspace.MustRoot(), dir), os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

// Profile 环境基本信息
type Profile struct {
	Name         FullEnvName  `json:"env_name"`
	UpdatedAt    time.Time    `json:"updated_at"`
	RemoteStatus RemoteStatus `json:"remote_status,omitempty"`
}

func profileFileName(fullEnvName FullEnvName) string {
	return fullEnvName.SafeFileName() + ".json"
}

// DeployEnv 部署环境
type DeployEnv struct {
	Profile

	deployConfig   *config.DeployConfig
	serviceConfigs []*config.ServiceConfig
}

func (e *DeployEnv) deployConfigFileName() string {
	return e.Name.SafeFileName() + ".yaml"
}

func (e *DeployEnv) serviceConfigFileName() string {
	return e.Name.SafeFileName() + ".yaml"
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
	if e.deployConfig == nil {
		return fmt.Errorf("no cached deploy config, call Update first")
	}

	var resolvedImages map[string]string
	for _, deployService := range e.deployConfig.Services {
		if deployService == nil {
			return fmt.Errorf("deploy service 为空")
		}
		if isInfraService(deployService) {
			continue
		}

		resolver := newImageResolver()
		if resolver == nil {
			return fmt.Errorf("image resolver is nil")
		}

		var err error
		resolvedImages, err = resolver.Resolve(ctx, e.deployConfig, e.serviceConfigs)
		if err != nil {
			return err
		}
		break
	}

	objects, err := e.BuildDeployObjects(resolvedImages)
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

	e.deployConfig = deployConfig
	e.serviceConfigs = serviceConfigs
	return nil
}

// Active 设置该环境为当前环境
func (e *DeployEnv) Active() error {
	scope, _ := e.Name.Split()
	return (&DeployContext{
		ActiveEnv:    e.Name,
		DefaultScope: scope,
	}).Save()
}

// Delete 删除环境
func (e *DeployEnv) Delete(ctx context.Context, exec executor) error {
	if exec == nil {
		return fmt.Errorf("executor is nil")
	}

	// 先执行远程删除（remote-first）
	if err := exec.Delete(ctx, string(e.Name)); err != nil {
		return err
	}

	ctxInfo, err := LoadDeployContext()
	if err != nil {
		return err
	}
	if ctxInfo != nil && ctxInfo.ActiveEnv != EmpytEnvName && ctxInfo.ActiveEnv == e.Name {
		ctxInfo.ActiveEnv = EmpytEnvName
		if err := ctxInfo.Save(); err != nil {
			return err
		}
	}

	if err := e.deleteDeployConfig(); err != nil {
		return err
	}

	if err := e.deleteServiceConfigs(); err != nil {
		return err
	}

	return os.RemoveAll(path.Join(workspace.MustRoot(), profileDir, profileFileName(e.Name)))
}

// save 保存环境信息
func (e *DeployEnv) save() error {
	if err := e.saveServiceConfigs(); err != nil {
		return err
	}

	if err := e.saveDeployConfig(); err != nil {
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
	filePath := path.Join(workspace.MustRoot(), profileDir, profileFileName(e.Name))

	return os.WriteFile(filePath, profileRaw, os.ModePerm)
}

func (e *DeployEnv) saveDeployConfig() error {
	// 序列化配置文件
	// 单独设置主配置文件名
	if e.deployConfig == nil {
		return nil
	}

	configRaw, err := yaml.Marshal(e.deployConfig)
	if err != nil {
		return err
	}

	filePath := path.Join(workspace.MustRoot(), deployConfigDir, e.deployConfigFileName())
	return os.WriteFile(filePath, configRaw, os.ModePerm)
}

func (e *DeployEnv) loadDeployConfig() error {
	deployConfig, err := config.ParseDeployConfig(path.Join(workspace.MustRoot(), deployConfigDir, e.deployConfigFileName()))
	if err != nil {
		// 忽略不存在错误
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	e.deployConfig = deployConfig
	return nil
}

func (e *DeployEnv) deleteDeployConfig() error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), deployConfigDir, e.deployConfigFileName()))
}

func (e *DeployEnv) saveServiceConfigs() error {
	raw, err := yaml.Marshal(e.serviceConfigs)
	if err != nil {
		return err
	}

	filePath := path.Join(workspace.MustRoot(), serviceConfigDir, e.serviceConfigFileName())
	return os.WriteFile(filePath, raw, os.ModePerm)
}

func (e *DeployEnv) loadServiceConfigs() error {
	raw, err := os.ReadFile(path.Join(workspace.MustRoot(), serviceConfigDir, e.serviceConfigFileName()))
	if err != nil {
		// 忽略不存在错误
		if os.IsNotExist(err) {
			return nil
		}
	}

	var serviceConfigs []*config.ServiceConfig
	if err := yaml.Unmarshal(raw, &serviceConfigs); err != nil {
		return err
	}

	e.serviceConfigs = serviceConfigs
	return nil
}

func (e *DeployEnv) deleteServiceConfigs() error {
	return os.RemoveAll(path.Join(workspace.MustRoot(), serviceConfigDir, e.serviceConfigFileName()))
}

// Equal 判断两个 DeployEnv 是否指向同一个环境。
func (e *DeployEnv) Equal(other *DeployEnv) bool {
	if other == nil {
		return false
	}
	return e.Name == other.Name
}

// BuildDeployObjects 根据当前环境缓存配置构建部署对象。
func (e *DeployEnv) BuildDeployObjects(resolvedImages map[string]string) (*k8s.DeployObjects, error) {
	return k8s.NewDeployObjects(e.deployConfig, e.serviceConfigs, string(e.Name), resolvedImages)
}

func resolveImages(ctx context.Context, resolver *imagepush.Resolver, deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig) (map[string]string, error) {
	if resolver == nil {
		return nil, fmt.Errorf("imagepush resolver is nil")
	}

	artifactTargets, err := collectReferencedArtifactTargets(deployConfig, serviceConfigs)
	if err != nil {
		return nil, err
	}
	if len(artifactTargets) == 0 {
		return nil, nil
	}

	resolvedImages := make(map[string]string)
	for _, artifactTarget := range artifactTargets {
		result, err := resolver.Resolve(ctx, artifactTarget)
		if err != nil {
			return nil, err
		}

		imageRef, err := result.ImageRef()
		if err != nil {
			return nil, err
		}
		resolvedImages[artifactTarget] = imageRef
	}

	return resolvedImages, nil
}

func collectReferencedArtifactTargets(deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig) ([]string, error) {
	if deployConfig == nil {
		return nil, fmt.Errorf("deploy config is nil")
	}

	serviceConfigMap := make(map[string]*config.ServiceConfig)
	for _, serviceConfig := range serviceConfigs {
		if serviceConfig == nil {
			return nil, fmt.Errorf("service config 为空")
		}
		if strings.TrimSpace(serviceConfig.URI) == "" {
			return nil, fmt.Errorf("service config %s 的 URI 为空", serviceConfig.Name)
		}
		if _, exists := serviceConfigMap[serviceConfig.URI]; exists {
			return nil, fmt.Errorf("service config URI 重复: %s", serviceConfig.URI)
		}
		serviceConfigMap[serviceConfig.URI] = serviceConfig
	}

	artifactTargetSet := make(map[string]struct{})
	for _, deployService := range deployConfig.Services {
		if deployService == nil {
			return nil, fmt.Errorf("deploy service 为空")
		}
		if isInfraService(deployService) {
			continue
		}

		serviceConfig, ok := serviceConfigMap[deployService.Artifact.Path]
		if !ok {
			return nil, fmt.Errorf("deploy service 引用的 path %s 未找到对应的 service config", deployService.Artifact.Path)
		}

		artifact, err := serviceConfig.GetArtifact(deployService.Artifact.Name)
		if err != nil {
			return nil, fmt.Errorf("service config %s 中未找到 artifact %s", serviceConfig.URI, deployService.Artifact.Name)
		}

		artifactTargetSet[artifact.Target] = struct{}{}
	}

	artifactTargets := make([]string, 0, len(artifactTargetSet))
	for artifactTarget := range artifactTargetSet {
		artifactTargets = append(artifactTargets, artifactTarget)
	}
	sort.Strings(artifactTargets)
	return artifactTargets, nil
}

func readServiceConfigs(deployConfig *config.DeployConfig) ([]*config.ServiceConfig, error) {
	var serviceConfigs []*config.ServiceConfig
	for _, service := range deployConfig.Services {
		if service == nil {
			return nil, fmt.Errorf("deploy service 为空")
		}
		if isInfraService(service) {
			continue
		}

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

func isInfraService(deployService *config.DeployService) bool {
	if deployService == nil {
		return false
	}

	return strings.TrimSpace(deployService.Infra.Resource) != ""
}

// New 创建新的环境
func New(fullEnvName FullEnvName) (*DeployEnv, error) {
	// 检查是否有同名服务
	env, err := Get(fullEnvName)
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
			Name:      fullEnvName,
			UpdatedAt: now(),
		},
	}
	if err := env.save(); err != nil {
		return nil, err
	}

	return env, nil
}

// Get 获取指定环境
func Get(fullEnvName FullEnvName) (*DeployEnv, error) {
	filePath := path.Join(workspace.MustRoot(), profileDir, profileFileName(fullEnvName))
	_, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	return loadDeployEnv(filePath)
}

// Current 返回当前激活中的环境
func Current() (*DeployEnv, error) {
	ctx, err := LoadDeployContext()
	if err != nil {
		return nil, err
	}
	if ctx == nil || ctx.ActiveEnv == EmpytEnvName {
		return nil, ErrNotActive
	}

	return Get(ctx.ActiveEnv)
}

// List 返回当前所有环境
func List() ([]*DeployEnv, error) {
	root := workspace.MustRoot()
	entries, err := os.ReadDir(path.Join(root, profileDir))
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
		base := strings.TrimSuffix(name, ".json")
		if !strings.HasSuffix(name, ".json") || strings.Contains(base, ".") {
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
		env, err := loadDeployEnv(path.Join(root, profileDir, name))
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

	if err := env.loadDeployConfig(); err != nil {
		return nil, err
	}

	if env.deployConfig != nil {
		if err := env.loadServiceConfigs(); err != nil {
			return nil, err
		}
	}

	return env, nil
}

// EnvRef 表示一个可激活的环境引用。
type EnvRef struct {
	Name string `json:"name"`
	App  string `json:"app"`
}

// DeployContext 表示 current.json 中保存的部署上下文。
type DeployContext struct {
	ActiveEnv    FullEnvName `json:"active_env,omitempty"`
	DefaultScope string      `json:"default_scope,omitempty"`
}

// GetDefaultScope 返回默认 scope。
func (ctx *DeployContext) GetDefaultScope() string {
	if ctx == nil {
		return ""
	}

	return ctx.DefaultScope
}

// SetDefaultScope 设置默认 scope。
func (ctx *DeployContext) SetDefaultScope(scope string) error {
	if err := ValidateScope(scope); err != nil {
		return err
	}

	ctx.DefaultScope = scope
	return nil
}

func (ctx *DeployContext) Save() error {
	if ctx == nil {
		ctx = &DeployContext{}
	}

	raw, err := json.Marshal(ctx)
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(workspace.MustRoot(), currentEnvPath), raw, os.ModePerm)
}

func LoadDeployContext() (*DeployContext, error) {
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
