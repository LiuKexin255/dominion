package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

// WailsConfig defines the wails.json fields inspected by this helper.
type WailsConfig struct {
	Name                 string `json:"name"`
	OutputFilename       string `json:"outputfilename"`
	FrontendInstall      string `json:"frontend:install"`
	FrontendBuild        string `json:"frontend:build"`
	FrontendDevWatcher   string `json:"frontend:dev:watcher"`
	FrontendDevServerURL string `json:"frontend:dev:serverUrl"`
	Author               struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
	Hooks map[string]interface{} `json:"hooks,omitempty"`
}

func main() {
	if err := inspect(); err != nil {
		fmt.Fprintf(os.Stderr, "inspect_wails_config: %v\n", err)
		os.Exit(1)
	}
}

func inspect() error {
	wailsJSONPath := flag.String("wails_json", "", "Path to wails.json")
	outPath := flag.String("out", "", "Output marker path")
	flag.Parse()

	if *wailsJSONPath == "" || *outPath == "" {
		return fmt.Errorf("usage: inspect_wails_config -wails_json <wails.json> -out <marker>")
	}

	if err := validateWailsConfigFile(*wailsJSONPath); err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, nil, 0644); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}
	return nil
}

func validateWailsConfigFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read wails.json: %w", err)
	}

	config := new(WailsConfig)
	if err := json.Unmarshal(data, config); err != nil {
		return fmt.Errorf("parse wails.json: %w", err)
	}
	return validateWailsConfig(config)
}

func validateWailsConfig(config *WailsConfig) error {
	if config.FrontendInstall != "" {
		return fmt.Errorf("frontend:install must be empty (found: %q)", config.FrontendInstall)
	}
	if config.FrontendBuild != "" {
		return fmt.Errorf("frontend:build must be empty (found: %q)", config.FrontendBuild)
	}
	if len(config.Hooks) > 0 {
		return fmt.Errorf("hooks must be empty or absent")
	}
	return nil
}
