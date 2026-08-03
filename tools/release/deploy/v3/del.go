package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dominion/tools/release/deploy/v2/client"
)

const deletePollInterval = 100 * time.Millisecond

func delCommand(ctx context.Context, opts *options) error {
	return deleteCommand(ctx, opts)
}

func deleteCommand(ctx context.Context, opts *options) error {
	fullEnvName := strings.TrimSpace(opts.target)
	// ParseFullEnvName 内部已调用 ValidateFullEnvName 校验完整格式（短名报错
	// errInvalidFullEnvName），无需重复校验，与 applyCommand 保持一致
	// （specs/033-deploy-scope-cleanup/spec.md FR-010）。
	scope, envName, err := ParseFullEnvName(fullEnvName)
	if err != nil {
		return err
	}
	resourceName := environmentResourceName(scope, envName)

	if err := opts.apiClient.DeleteEnvironment(ctx, resourceName); err != nil {
		return err
	}

	if err := client.PollUntilDeleted(ctx, opts.apiClient, resourceName, deletePollInterval, opts.timeout); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "环境 %s 已删除\n", fullEnvName)
	return nil
}
