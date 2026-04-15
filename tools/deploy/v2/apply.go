package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/projects/infra/deploy"
	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/imagepush"
	"dominion/tools/deploy/pkg/workspace"
	"dominion/tools/deploy/v2/client"
	"dominion/tools/deploy/v2/compiler"
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

	fullEnvName, err := NewFullEnvName(opts.scope, strings.TrimSpace(deployConfig.Name))
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
