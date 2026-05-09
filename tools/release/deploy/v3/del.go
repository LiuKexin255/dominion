package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dominion/tools/release/deploy/pkg/workspace"
	"dominion/tools/release/deploy/v2/client"
)

const deletePollInterval = 100 * time.Millisecond

func delCommand(ctx context.Context, opts *options) error {
	return deleteCommand(ctx, opts)
}

func deleteCommand(ctx context.Context, opts *options) error {
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

	if err := opts.apiClient.DeleteEnvironment(ctx, resourceName); err != nil {
		return err
	}

	if err := client.PollUntilDeleted(ctx, opts.apiClient, resourceName, deletePollInterval, opts.timeout); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "环境 %s 已删除\n", fullEnvName)
	return nil
}
