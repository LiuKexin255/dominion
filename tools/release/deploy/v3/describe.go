package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/projects/infra/deploy"
	"dominion/tools/release/deploy/pkg/workspace"
	"dominion/tools/release/deploy/v2/client"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func describeCommand(ctx context.Context, opts *options) error {
	root := workspace.MustRoot()
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	scope := strings.TrimSpace(opts.scope)
	if scope == "" {
		scope = strings.TrimSpace(cfg.DefaultScope)
	}

	fullEnvName, err := NewFullEnvName(scope, strings.TrimSpace(opts.target))
	if err != nil {
		return err
	}
	scope, envName, err := ParseFullEnvName(fullEnvName)
	if err != nil {
		return err
	}
	resourceName := environmentResourceName(scope, envName)

	environment, err := opts.apiClient.GetEnvironment(ctx, resourceName)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			fmt.Fprintf(stdout, "环境 %s 不存在\n", fullEnvName)
			return err
		}
		return err
	}

	printEnvironmentDetail(fullEnvName, environment)
	return nil
}

// printEnvironmentDetail 按 contracts/deploy-describe.md 输出契约打印环境详情。
func printEnvironmentDetail(fullEnvName string, environment *deploy.Environment) {
	fmt.Fprintf(stdout, "环境 %s\n", fullEnvName)

	state := "未知"
	if environment.Status != nil {
		if s := formatState(environment.Status.State); s != "" {
			state = s
		}
	}
	fmt.Fprintf(stdout, "状态: %s\n", state)

	if environment.Status != nil && environment.Status.Message != "" {
		fmt.Fprintf(stdout, "说明: %s\n", environment.Status.Message)
	}

	artifacts := environment.DesiredState.GetArtifacts()
	infras := environment.DesiredState.GetInfras()
	if len(artifacts) == 0 && len(infras) == 0 {
		fmt.Fprintln(stdout, "服务: （无）")
	} else {
		fmt.Fprintln(stdout, "服务:")
		for _, artifact := range artifacts {
			fmt.Fprintf(stdout, "  - %s (app=%s) [artifact]\n", artifact.GetName(), artifact.GetApp())
		}
		for _, infra := range infras {
			fmt.Fprintf(stdout, "  - %s (app=%s) [infra: %s]\n", infra.GetName(), infra.GetApp(), infra.GetResource())
		}
	}

	fmt.Fprintf(stdout, "最近调和: %s\n", formatDescribeTimestamp(environment.GetStatus().GetLastReconcileTime()))
	fmt.Fprintf(stdout, "最近成功: %s\n", formatDescribeTimestamp(environment.GetStatus().GetLastSuccessTime()))
}

// formatDescribeTimestamp 输出 RFC3339 UTC 时间戳；nil 时输出 "-"。
func formatDescribeTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "-"
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}
