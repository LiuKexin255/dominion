package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

type options struct {
	env    string
	deploy string
	del    string
}

type envState struct {
	Name      string `json:"name"`
	DeployApp string `json:"deploy_app,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
}

func parseOptions(args []string) (options, error) {
	var opts options

	fs := flag.NewFlagSet("env_deploy", flag.ContinueOnError)
	fs.StringVar(&opts.env, "env", "", "create or switch environment")
	fs.StringVar(&opts.deploy, "deploy", "", "path to deploy.yaml")
	fs.StringVar(&opts.del, "del", "", "delete environment")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	switch {
	case opts.del != "":
		if opts.env != "" || opts.deploy != "" {
			return options{}, errors.New("--del cannot be combined with --env or --deploy")
		}
	case opts.deploy != "":
		if opts.env == "" {
			return options{}, errors.New("--deploy requires --env")
		}
	case opts.env == "":
		return options{}, errors.New("must provide --env, --deploy with --env, or --del")
	}

	return opts, nil
}

func loadDeployAndServices(deployPath string) (string, deployConfig, []serviceConfig, error) {
	deploySchemaPath, serviceSchemaPath, err := schemaPaths()
	if err != nil {
		return "", deployConfig{}, nil, err
	}
	if !filepath.IsAbs(deployPath) {
		root, err := workspaceRoot()
		if err != nil {
			return "", deployConfig{}, nil, err
		}
		deployPath = filepath.Join(root, deployPath)
	}

	absDeployPath, err := filepath.Abs(deployPath)
	if err != nil {
		return "", deployConfig{}, nil, fmt.Errorf("resolve deploy path: %w", err)
	}

	deployRaw, err := os.ReadFile(absDeployPath)
	if err != nil {
		return "", deployConfig{}, nil, fmt.Errorf("read deploy file: %w", err)
	}
	if err := validateYAML(deployRaw, deploySchemaPath); err != nil {
		return "", deployConfig{}, nil, fmt.Errorf("invalid deploy file %q: %w", absDeployPath, err)
	}

	var deployCfg deployConfig
	if err := yaml.Unmarshal(deployRaw, &deployCfg); err != nil {
		return "", deployConfig{}, nil, fmt.Errorf("decode deploy file: %w", err)
	}

	serviceCfgs := make([]serviceConfig, 0, len(deployCfg.Services))
	baseDir := filepath.Dir(absDeployPath)
	for _, svc := range deployCfg.Services {
		servicePath := svc.Artifact.Path
		if !filepath.IsAbs(servicePath) {
			servicePath = filepath.Join(baseDir, servicePath)
		}
		serviceRaw, err := os.ReadFile(servicePath)
		if err != nil {
			return "", deployConfig{}, nil, fmt.Errorf("read service file %q: %w", servicePath, err)
		}
		if err := validateYAML(serviceRaw, serviceSchemaPath); err != nil {
			return "", deployConfig{}, nil, fmt.Errorf("invalid service file %q: %w", servicePath, err)
		}

		var serviceCfg serviceConfig
		if err := yaml.Unmarshal(serviceRaw, &serviceCfg); err != nil {
			return "", deployConfig{}, nil, fmt.Errorf("decode service file %q: %w", servicePath, err)
		}
		serviceCfgs = append(serviceCfgs, serviceCfg)
	}

	return absDeployPath, deployCfg, serviceCfgs, nil
}

func serviceNames(cfgs []serviceConfig) []string {
	names := make([]string, 0, len(cfgs))
	for _, cfg := range cfgs {
		names = append(names, cfg.Name)
	}
	return names
}

func schemaPaths() (string, string, error) {
	root, err := workspaceRoot()
	if err != nil {
		return "", "", err
	}

	deploySchema := filepath.Join(root, "tools", "deploy", "deploy.schema.json")
	serviceSchema := filepath.Join(root, "tools", "deploy", "service.schema.json")
	if exists(deploySchema) && exists(serviceSchema) {
		return deploySchema, serviceSchema, nil
	}

	return "", "", errors.New("cannot locate deploy schema files under tools/deploy")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func validateYAML(yamlRaw []byte, schemaPath string) error {
	schemaRaw, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read schema %q: %w", schemaPath, err)
	}

	jsonDoc, err := yamlToJSON(yamlRaw)
	if err != nil {
		return fmt.Errorf("convert YAML to JSON document: %w", err)
	}
	jsonSchema, err := yamlToJSON(schemaRaw)
	if err != nil {
		return fmt.Errorf("convert YAML to JSON schema: %w", err)
	}

	result, err := gojsonschema.Validate(
		gojsonschema.NewBytesLoader(jsonSchema),
		gojsonschema.NewBytesLoader(jsonDoc),
	)
	if err != nil {
		return fmt.Errorf("run schema validation: %w", err)
	}
	if result.Valid() {
		return nil
	}

	errs := make([]string, 0, len(result.Errors()))
	for _, issue := range result.Errors() {
		errs = append(errs, fmt.Sprintf("%s: %s", issue.Field(), issue.Description()))
	}
	return errors.New(strings.Join(errs, "; "))
}

func yamlToJSON(raw []byte) ([]byte, error) {
	var data any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return json.Marshal(data)
}

func envFilePath(env string) (string, error) {
	if strings.TrimSpace(env) == "" {
		return "", errors.New("env is empty")
	}

	root, err := workspaceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".env", env+".env.json"), nil
}

func workspaceRoot() (string, error) {
	if root := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); root != "" {
		if exists(root) {
			return root, nil
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		if exists(filepath.Join(wd, "go.mod")) {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}

	return "", errors.New("cannot locate workspace root")
}

func upsertEnvCache(env string) (bool, envState, error) {
	path, err := envFilePath(env)
	if err != nil {
		return false, envState{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, envState{}, fmt.Errorf("create env cache directory: %w", err)
	}

	state := envState{Name: env, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	created := true
	if raw, err := os.ReadFile(path); err == nil {
		created = false
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &state); err != nil {
				return false, envState{}, fmt.Errorf("decode env cache %q: %w", path, err)
			}
			state.Name = env
			state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, envState{}, fmt.Errorf("read env cache %q: %w", path, err)
	}

	if err := writeJSON(path, state); err != nil {
		return false, envState{}, err
	}

	return created, state, nil
}

func writeEnvState(env string, state envState) error {
	path, err := envFilePath(env)
	if err != nil {
		return err
	}
	return writeJSON(path, state)
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	raw = append(raw, '\n')

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

func deleteEnvCache(env string) (bool, error) {
	path, err := envFilePath(env)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("delete env cache %q: %w", path, err)
	}
	return true, nil
}
