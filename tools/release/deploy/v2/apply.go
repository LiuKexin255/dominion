package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dominion/projects/infra/deploy"
	"dominion/tools/release/deploy/pkg/config"
	"dominion/tools/release/deploy/pkg/imagepush"
	"dominion/tools/release/deploy/pkg/workspace"
	"dominion/tools/release/deploy/v2/client"
	"dominion/tools/release/deploy/v2/compiler"
)

const applyPollInterval = 100 * time.Millisecond

var (
	newImageRunner = imagepush.NewRunner
	pollUntilReady = client.PollUntilReady
)

func applyCommand(opts *options) error {
	deployConfig, err := config.ParseDeployConfig(workspace.ResolvePath(opts.target))
	if err != nil {
		return err
	}

	envName, err := resolvePlaceholders(deployConfig.Name, deployConfig.Type, opts.run)
	if err != nil {
		return err
	}

	serviceConfigs, err := compiler.ReadServiceConfigs(deployConfig)
	if err != nil {
		return err
	}
	artifactTargets, err := compiler.ResolveArtifactTargets(deployConfig, serviceConfigs)
	if err != nil {
		return err
	}

	imageResults, err := resolveImages(context.Background(), artifactTargets)
	if err != nil {
		return err
	}

	desiredState, err := compiler.Compile(deployConfig, serviceConfigs, imageResults)
	if err != nil {
		return err
	}

	fullEnvName, err := NewFullEnvName(opts.scope, strings.TrimSpace(envName))
	if err != nil {
		return err
	}
	scope, envName, err := ParseFullEnvName(fullEnvName)
	if err != nil {
		return err
	}

	parentName := scopeResourceName(scope)
	environmentName := environmentResourceName(scope, envName)

	if _, err := opts.apiClient.GetEnvironment(context.Background(), environmentName); err != nil {
		if !errors.Is(err, client.ErrNotFound) {
			return err
		}
		createRequest := &deploy.Environment{
			Description:  deployConfig.Desc,
			DesiredState: desiredState,
			Type:         environmentTypeFromEnum(deployConfig.Type),
		}
		if _, err := opts.apiClient.CreateEnvironment(context.Background(), parentName, envName, createRequest); err != nil {
			return err
		}
	} else {
		updateRequest := &deploy.Environment{
			Name:         environmentName,
			DesiredState: desiredState,
		}
		if _, err := opts.apiClient.UpdateEnvironment(context.Background(), updateRequest); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "环境 %s 已提交，等待启动...\n", fullEnvName)

	readyEnv, err := pollUntilReady(context.Background(), opts.apiClient, environmentName, applyPollInterval, opts.timeout)
	if err != nil {
		return err
	}

	state := ""
	if readyEnv != nil && readyEnv.Status != nil {
		state = formatState(readyEnv.Status.State)
	}
	if state == "" {
		fmt.Fprintf(stdout, "环境 %s 已应用\n", fullEnvName)
		return nil
	}

	fmt.Fprintf(stdout, "环境 %s 已应用，状态: %s\n", fullEnvName, state)
	return nil
}

const (
	placeholderRun     = "{{run}}"
	placeholderPattern = `\{\{[^}]+\}\}`
)

var placeholderRegexp = regexp.MustCompile(placeholderPattern)

// resolvePlaceholders 处理 deploy config name 中的占位符。
// 只有 type=test 允许使用 {{run}} 占位符，且必须通过 --run 传入替换值。
func resolvePlaceholders(name string, envType config.EnvironmentType, run string) (string, error) {
	hasRun := strings.Contains(name, placeholderRun)

	if !hasRun && run == "" {
		return name, nil
	}

	if !hasRun && run != "" {
		return "", fmt.Errorf("--run 仅用于含 %s 的 deploy 配置", placeholderRun)
	}

	// name contains {{run}} from here
	if envType != config.EnvironmentTypeTest {
		return "", fmt.Errorf("只有 test 类型允许使用 %s 占位符", placeholderRun)
	}

	if run == "" {
		return "", fmt.Errorf("deploy 配置含 %s 但未传 --run 参数", placeholderRun)
	}

	if strings.Count(name, placeholderRun) > 1 {
		return "", fmt.Errorf("deploy 配置中 %s 占位符出现多次", placeholderRun)
	}

	// 检测非 {{run}} 的未知占位符
	withoutRun := strings.ReplaceAll(name, placeholderRun, "")
	if matches := placeholderRegexp.FindString(withoutRun); matches != "" {
		return "", fmt.Errorf("deploy 配置含未知占位符 %q，仅支持 %s", matches, placeholderRun)
	}

	if !envPartRegexp.MatchString(run) {
		return "", fmt.Errorf("--run 值 %q 不合法，须匹配 %s", run, envPartRegexp.String())
	}

	return strings.ReplaceAll(name, placeholderRun, run), nil
}

func resolveImages(ctx context.Context, artifactTargets []string) (map[string]*imagepush.Result, error) {
	if len(artifactTargets) == 0 {
		return nil, nil
	}

	runner, err := newImageRunner()
	if err != nil {
		return nil, err
	}
	resolver := imagepush.NewResolver(runner)
	results := make(map[string]*imagepush.Result)
	for _, artifactTarget := range artifactTargets {
		result, err := resolver.Resolve(ctx, artifactTarget)
		if err != nil {
			return nil, fmt.Errorf("resolve image for %s failed: %w", artifactTarget, err)
		}
		results[artifactTarget] = result
	}

	return results, nil
}

func scopeResourceName(scope string) string {
	return "deploy/scopes/" + scope
}

func environmentResourceName(scope, envName string) string {
	return scopeResourceName(scope) + "/environments/" + envName
}

func parseEnvironmentResourceName(name string) (scope, envName string) {
	const prefix = "deploy/scopes/"
	const infix = "/environments/"
	rest := strings.TrimPrefix(name, prefix)
	scope, envName, _ = strings.Cut(rest, infix)
	return scope, envName
}

func formatState(s deploy.EnvironmentState) string {
	switch s {
	case deploy.EnvironmentState_ENVIRONMENT_STATE_PENDING:
		return "等待中"
	case deploy.EnvironmentState_ENVIRONMENT_STATE_RECONCILING:
		return "部署中"
	case deploy.EnvironmentState_ENVIRONMENT_STATE_READY:
		return "就绪"
	case deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED:
		return "失败"
	case deploy.EnvironmentState_ENVIRONMENT_STATE_DELETING:
		return "删除中"
	default:
		return ""
	}
}

// environmentTypeFromEnum 将配置文件中的环境类型转换为 proto 枚举值。
func environmentTypeFromEnum(s config.EnvironmentType) deploy.EnvironmentType {
	switch s {
	case config.EnvironmentTypeProd:
		return deploy.EnvironmentType_ENVIRONMENT_TYPE_PROD
	case config.EnvironmentTypeTest:
		return deploy.EnvironmentType_ENVIRONMENT_TYPE_TEST
	case config.EnvironmentTypeDev:
		return deploy.EnvironmentType_ENVIRONMENT_TYPE_DEV
	default:
		return deploy.EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED
	}
}
