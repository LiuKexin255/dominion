package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dominion/tools/release/deploy/pkg/workspace"
)

const (
	cliConfigDir  = ".env"
	cliConfigFile = "cli.json"
)

// cliConfig 保存 CLI 本地配置。
type cliConfig struct {
	DefaultScope string `json:"default_scope,omitempty"`
}

func loadConfig(workspaceRoot string) (*cliConfig, error) {
	path := filepath.Join(workspaceRoot, cliConfigDir, cliConfigFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &cliConfig{}, nil
		}
		return nil, err
	}

	cfg := new(cliConfig)
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func saveConfig(workspaceRoot string, cfg *cliConfig) error {
	if cfg == nil {
		cfg = &cliConfig{}
	}

	dir := filepath.Join(workspaceRoot, cliConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, cliConfigFile), raw, 0o644)
}

func scopeCommand(opts *options) error {
	root := workspace.MustRoot()
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	if opts == nil || strings.TrimSpace(opts.target) == "" {
		scope := strings.TrimSpace(cfg.DefaultScope)
		if scope == "" {
			fmt.Fprintln(stdout, "not set")
			return nil
		}

		fmt.Fprintln(stdout, scope)
		return nil
	}

	target := strings.TrimSpace(opts.target)
	if err := ValidateScope(target); err != nil {
		return err
	}

	cfg.DefaultScope = target
	if err := saveConfig(root, cfg); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "默认 scope 已设置为 %s\n", target)
	return nil
}
