package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/env"
	"dominion/tools/deploy/pkg/workspace"
)

const (
	deploySchemaRelPath  = "tools/deploy/deploy.schema.json"
	serviceSchemaRelPath = "tools/deploy/service.schema.json"
)

type options struct {
	env       string
	app       string
	deploy    string
	delete string
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

	env.LazyInit()
	if err := initSchemaValidaters(); err != nil {
		return err
	}

	resolvedAppName, err := resolveAppName(opts.app)
	if err != nil {
		return err
	}

	switch {
	case opts.delete != "":
		return deleteEnvironment(opts.delete, resolvedAppName)
	case opts.deploy != "":
		return deployAndActivate(opts.env, resolvedAppName, opts.deploy)
	default:
		return switchEnvironment(opts.env, resolvedAppName)
	}
}

func parseOptions(args []string) (options, error) {
	var opts options

	fs := flag.NewFlagSet("env_deploy", flag.ContinueOnError)
	fs.StringVar(&opts.env, "env", "", "create or switch environment")
	fs.StringVar(&opts.app, "app", "", "application name")
	fs.StringVar(&opts.deploy, "deploy", "", "path to deploy.yaml")
	fs.StringVar(&opts.delete, "del", "", "delete environment")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	switch {
	case opts.delete != "":
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

func initSchemaValidaters() error {
	deploySchemaAbsPath := filepath.Join(workspace.MustRoot(), deploySchemaRelPath)
	serviceSchemaAbsPath := filepath.Join(workspace.MustRoot(), serviceSchemaRelPath)

	deployValidater, err := config.NewYAMLValidater(deploySchemaAbsPath)
	if err != nil {
		return fmt.Errorf("加载 deploy schema 失败: %w", err)
	}
	serviceValidater, err := config.NewYAMLValidater(serviceSchemaAbsPath)
	if err != nil {
		return fmt.Errorf("加载 service schema 失败: %w", err)
	}

	config.RegisterDeployValidater(deployValidater)
	config.RegisterServiceValidater(serviceValidater)
	return nil
}

func resolvePath(inputPath string) (string, error) {
	if filepath.IsAbs(inputPath) {
		return inputPath, nil
	}
	root, err := workspace.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, inputPath), nil
}

func parseDeployConfig(deployPath string) (*config.DeployConfig, error) {
	absDeployPath, err := resolvePath(deployPath)
	if err != nil {
		return nil, fmt.Errorf("解析 deploy 文件路径失败: %w", err)
	}

	deployConfig, err := config.ParseDeployConfig(absDeployPath)
	if err != nil {
		return nil, fmt.Errorf("解析部署配置失败: %w", err)
	}

	return deployConfig, nil
}

func resolveAppName(appName string) (string, error) {
	if strings.TrimSpace(appName) != "" {
		return appName, nil
	}

	active, err := env.Current()
	if err != nil {
		return "", errors.New("未指定 --app，且当前没有激活环境")
	}
	return active.App, nil
}

func deployAndActivate(envName string, appName string, deployPath string) error {
	deployConfig, err := parseDeployConfig(deployPath)
	if err != nil {
		return err
	}

	deployEnv, err := env.Get(envName, appName)
	if err != nil {
		if !errors.Is(err, env.ErrNotFound) {
			return err
		}

		deployEnv, err = env.New(envName, appName)
		if err != nil {
			return fmt.Errorf("创建环境失败: %w", err)
		}
	}

	if err := deployEnv.Update(deployConfig); err != nil {
		return fmt.Errorf("更新环境失败: %w", err)
	}

	if err := deployEnv.Active(); err != nil {
		return fmt.Errorf("激活环境失败: %w", err)
	}

	fmt.Printf("环境 %s/%s 已激活\n", envName, appName)
	return nil
}

func switchEnvironment(envName string, appName string) error {
	deployEnv, err := env.Get(envName, appName)
	if err != nil {
		if !errors.Is(err, env.ErrNotFound) {
			return err
		}

		deployEnv, err = env.New(envName, appName)
		if err != nil {
			return fmt.Errorf("创建环境失败: %w", err)
		}
	}

	if err := deployEnv.Active(); err != nil {
		return err
	}

	fmt.Printf("已切换到环境 %s/%s\n", envName, appName)
	return nil
}

func deleteEnvironment(envName string, appName string) error {
	deployEnv, err := env.Get(envName, appName)
	if err != nil {
		if errors.Is(err, env.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := deployEnv.Delete(); err != nil {
		return err
	}

	fmt.Printf("环境 %s/%s 已删除\n", envName, appName)
	return nil
}
