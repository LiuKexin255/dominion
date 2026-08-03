package main

import (
	"context"
	"fmt"
	"strings"
)

func listCommand(ctx context.Context, opts *options) error {
	scope := strings.TrimSpace(opts.scope)
	if scope == "" {
		// AIP-159 通配符：不指定 --scope 时跨 scope 列出所有环境
		// （specs/033-deploy-scope-cleanup/spec.md FR-007）。
		scope = "-"
	}

	environments, err := opts.apiClient.ListEnvironments(ctx, scopeResourceName(scope))
	if err != nil {
		return err
	}

	for _, environment := range environments {
		if environment == nil {
			continue
		}
		// 输出使用响应中的实际完整环境名（FR-008/AIP-159）：
		// 跨 scope 模式下 scope 变量为 "-"，须从 canonical resource name 解析。
		envScope, envName := parseEnvironmentResourceName(environment.Name)
		line := envScope + "." + envName
		if environment.Status != nil {
			if s := formatState(environment.Status.State); s != "" {
				line += "\t" + s
			}
		}
		fmt.Fprintln(stdout, line)
	}

	return nil
}
