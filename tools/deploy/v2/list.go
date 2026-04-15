package main

import (
	"context"
	"fmt"
	"strings"

	"dominion/tools/deploy/pkg/workspace"
	"dominion/tools/deploy/v2/client"
)

func listCommand(opts *options) error {
	root := workspace.MustRoot()
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	scope := strings.TrimSpace(opts.scope)
	if scope == "" {
		scope = strings.TrimSpace(cfg.DefaultScope)
	}
	if scope == "" {
		return errNoDefaultScope
	}
	if err := ValidateScope(scope); err != nil {
		return err
	}

	apiClient := client.NewClient(opts.endpoint)
	environments, err := apiClient.ListEnvironments(context.Background(), scopeResourceName(scope))
	if err != nil {
		return err
	}

	for _, environment := range environments {
		if environment == nil {
			continue
		}
		fmt.Fprintln(stdout, environment.Name)
	}

	return nil
}
