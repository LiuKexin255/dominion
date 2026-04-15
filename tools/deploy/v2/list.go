package main

import (
	"context"
	"fmt"
	"strings"

	"dominion/tools/deploy/pkg/workspace"
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

	environments, err := opts.apiClient.ListEnvironments(context.Background(), scopeResourceName(scope))
	if err != nil {
		return err
	}

	for _, environment := range environments {
		if environment == nil {
			continue
		}
		_, envName := parseEnvironmentResourceName(environment.Name)
		line := scope + "." + envName
		if environment.Status != nil {
			if s := formatState(environment.Status.State); s != "" {
				line += "\t" + s
			}
		}
		fmt.Fprintln(stdout, line)
	}

	return nil
}
