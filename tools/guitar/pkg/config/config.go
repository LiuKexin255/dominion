// Package config provides parsing for guitar testplan YAML files.
package config

import (
	"fmt"
	"os"

	"dominion/tools/deploy/pkg/workspace"

	"github.com/goccy/go-yaml"
)

// Config is the top-level structure of a testplan YAML file.
type Config struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Suites      []*Suite `yaml:"suites"`
}

// Suite is a test suite configuration.
type Suite struct {
	Name     string               `yaml:"name"`
	Env      string               `yaml:"env"`
	Deploy   string               `yaml:"deploy"`
	Endpoint map[string]Endpoints `yaml:"endpoint"` // protocol -> Endpoints
	Cases    []string             `yaml:"cases"`
}

// Endpoints maps endpoint names to URLs for a single protocol.
type Endpoints map[string]string

// Parse reads a testplan YAML file and deserializes it into a Config.
// filePath can be a plain path or a // prefix workspace-relative path.
func Parse(filePath string) (*Config, error) {
	resolved := workspace.ResolvePath(filePath)
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", filePath, err)
	}

	cfg := new(Config)
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", filePath, err)
	}

	return cfg, nil
}
