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

func delCommand(opts *options) error {
	return deleteCommand(opts)
}

func deleteCommand(opts *options) error {
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

	if err := opts.apiClient.DeleteEnvironment(context.Background(), resourceName); err != nil {
		return err
	}

	if err := client.PollUntilDeleted(context.Background(), opts.apiClient, resourceName, deletePollInterval, opts.timeout); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "环境 %s 已删除\n", fullEnvName)
	return nil
}
