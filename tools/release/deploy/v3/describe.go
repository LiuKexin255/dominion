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

// printEnvironmentDetail 按 specs/032-guitar-deploy-failure-state/contracts/deploy-describe.md
// 输出契约打印环境详情：服务列表以 per-service rollout 状态为主线（决策 R5，
// specs/032-guitar-deploy-failure-state/research.md），环境级 message 降级为次要。
func printEnvironmentDetail(fullEnvName string, environment *deploy.Environment) {
	fmt.Fprintf(stdout, "环境 %s\n", fullEnvName)

	state := "未知"
	if environment.Status != nil {
		if s := formatState(environment.Status.State); s != "" {
			state = s
		}
	}
	fmt.Fprintf(stdout, "状态: %s\n", state)

	// 有 per-service 数据时不输出 说明:，避免与 per-service message 重复；仅当
	// services 为空（旧版服务端无 per-service 数据，或非 rollout 原因如 apply
	// 失败/retry-exhausted）且 message 非空时输出。
	services := environment.GetStatus().GetServices()
	if environment.Status != nil && len(services) == 0 && environment.Status.Message != "" {
		fmt.Fprintf(stdout, "说明: %s\n", environment.Status.Message)
	}

	artifacts := environment.DesiredState.GetArtifacts()
	infras := environment.DesiredState.GetInfras()
	if len(artifacts) == 0 && len(infras) == 0 {
		fmt.Fprintln(stdout, "服务: （无）")
	} else {
		// 预建 name+app+kind 三元组 → ServiceStatus 的查找表。用三元组而非
		// name+app 是因为 domain 校验仅保证 artifact name 唯一、infra name
		// 唯一，不保证跨类唯一，见 specs/032-guitar-deploy-failure-state/data-model.md。
		servicesByKey := make(map[serviceIdentity]*deploy.ServiceStatus, len(services))
		for _, service := range services {
			servicesByKey[serviceIdentity{Name: service.GetName(), App: service.GetApp(), Kind: service.GetKind()}] = service
		}

		fmt.Fprintln(stdout, "服务:")
		for _, artifact := range artifacts {
			service := servicesByKey[serviceIdentity{Name: artifact.GetName(), App: artifact.GetApp(), Kind: deploy.ServiceKind_SERVICE_KIND_ARTIFACT}]
			fmt.Fprintf(stdout, "  - %s (app=%s) [artifact]%s\n", artifact.GetName(), artifact.GetApp(), serviceStateText(service))
		}
		for _, infra := range infras {
			service := servicesByKey[serviceIdentity{Name: infra.GetName(), App: infra.GetApp(), Kind: deploy.ServiceKind_SERVICE_KIND_INFRA}]
			fmt.Fprintf(stdout, "  - %s (app=%s) [infra: %s]%s\n", infra.GetName(), infra.GetApp(), infra.GetResource(), serviceStateText(service))
		}
	}

	fmt.Fprintf(stdout, "最近调和: %s\n", formatDescribeTimestamp(environment.GetStatus().GetLastReconcileTime()))
	fmt.Fprintf(stdout, "最近成功: %s\n", formatDescribeTimestamp(environment.GetStatus().GetLastSuccessTime()))
}

// serviceIdentity 标识 status.services 中的单个服务（name+app+kind 三元组），
// 作为 describe 归并 per-service 状态到 desired_state 服务列表的查找 key。
type serviceIdentity struct {
	Name string
	App  string
	Kind deploy.ServiceKind
}

// serviceStateText 返回服务列表项尾追加的 per-service rollout 状态文本；service
// 为 nil（status.services 无对应三元组，兼容旧版服务端无 per-service 数据）或状态
// 为 UNSPECIFIED 时不追加。
func serviceStateText(service *deploy.ServiceStatus) string {
	if service == nil {
		return ""
	}
	switch service.GetState() {
	case deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_READY:
		return " 就绪"
	case deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_WAITING:
		return " 等待发布: " + service.GetMessage()
	case deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_FAILED:
		return " 失败: " + service.GetMessage()
	case deploy.ServiceRolloutState_SERVICE_ROLLOUT_STATE_PENDING:
		return " 已提交，等待观测"
	default:
		return ""
	}
}

// formatDescribeTimestamp 输出 RFC3339 UTC 时间戳；nil 时输出 "-"。
func formatDescribeTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "-"
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}
