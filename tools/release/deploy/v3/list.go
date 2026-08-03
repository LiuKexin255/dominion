package main

import (
	"context"
	"fmt"
	"strings"
)

func listCommand(ctx context.Context, opts *options) error {
	scope := strings.TrimSpace(opts.scope)
	if scope == "" {
		return fmt.Errorf("%s 需要 --scope 参数", commandList)
	}

	environments, err := opts.apiClient.ListEnvironments(ctx, scopeResourceName(scope))
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
