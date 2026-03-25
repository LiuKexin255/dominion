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
	workspacePathPrefix  = "//"

	commandUse    = "use"
	commandDeploy = "deploy"
	commandDel    = "del"

	flagApp = "app"
)

type options struct {
	command string
	target  string
	app     string
}

func (o *options) Default() error {
	// app 为空时使用当前激活的环境 app
	if o.app == "" {
		switch o.command {
		case commandUse, commandDel:
			active, err := env.Current()
			if err != nil {
				return fmt.Errorf("未指定 --%s，且当前没有激活环境", flagApp)
			}
			o.app = active.App
		}
	}

	o.app = strings.TrimSpace(o.app)
	o.target = strings.TrimSpace(o.target)

	return nil
}

type flagSpec struct {
	name         string
	defaultValue string
	usage        string
	bind         func(fs *flag.FlagSet, opts *options, spec flagSpec)
}

var flagSpecs = map[string]flagSpec{
	flagApp: {
		name:         flagApp,
		defaultValue: "",
		usage:        "application name",
		bind: func(fs *flag.FlagSet, opts *options, spec flagSpec) {
			fs.StringVar(&opts.app, spec.name, spec.defaultValue, spec.usage)
		},
	},
}

var commandFlagTable = map[string][]string{
	commandUse:    {flagApp},
	commandDeploy: {},
	commandDel:    {flagApp},
}

// commandExecFunc 命令执行方法
type commandExecFunc = func(opts *options) error

// commandValidaterFunc 命令校验方法
type commandValidaterFunc = func(opts *options) error

var commandExecTable = map[string]commandExecFunc{
	commandUse:    switchEnvironment,
	commandDeploy: deployAndActivate,
	commandDel:    deleteEnvironment,
}

var commandValidaterTable = map[string]commandValidaterFunc{
	commandUse:    validateUseOptions,
	commandDeploy: validateDeployOptions,
	commandDel:    validateDelOptions,
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

	if err := opts.Default(); err != nil {
		return err
	}

	exec, ok := commandExecTable[opts.command]
	if !ok {
		return fmt.Errorf("未知命令: %s", opts.command)
	}

	return exec(opts)
}

func parseOptions(args []string) (*options, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("必须提供命令：%s、%s 或 %s", commandUse, commandDeploy, commandDel)
	}

	fs, opts, err := newCommandFlagSet(args[0])
	if err != nil {
		return nil, err
	}

	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}

	positionArgs := fs.Args()
	if len(positionArgs) > 0 {
		opts.target = positionArgs[0]
	}

	if err := validateOptions(opts); err != nil {
		return nil, err
	}

	return opts, nil
}

func newCommandFlagSet(command string) (*flag.FlagSet, *options, error) {
	flagNames, ok := commandFlagTable[command]
	if !ok {
		return nil, nil, fmt.Errorf("unknown command: %s", command)
	}

	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	opts := &options{command: command}

	for _, flagName := range flagNames {
		spec, ok := flagSpecs[flagName]
		if !ok {
			return nil, nil, fmt.Errorf("unknown flag spec for command %s: %s", command, flagName)
		}
		spec.bind(fs, opts, spec)
	}

	return fs, opts, nil
}

func validateOptions(opts *options) error {
	validater, ok := commandValidaterTable[opts.command]
	if !ok {
		return fmt.Errorf("unknown command: %s", opts.command)
	}

	return validater(opts)
}

func validateUseOptions(opts *options) error {
	if opts.target == "" {
		return fmt.Errorf("%s requires env name", commandUse)
	}
	return nil
}

func validateDeployOptions(opts *options) error {
	if opts.target == "" {
		return fmt.Errorf("%s requires deploy.yaml path", commandDeploy)
	}
	if opts.app != "" {
		return fmt.Errorf("%s does not support --%s", commandDeploy, flagApp)
	}
	return nil
}

func validateDelOptions(opts *options) error {
	if opts.target == "" {
		return fmt.Errorf("%s requires env name", commandDel)
	}
	return nil
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

func resolvePath(inputPath string) string {
	if strings.HasPrefix(inputPath, workspacePathPrefix) {
		return filepath.Join(workspace.MustRoot(), strings.TrimPrefix(inputPath, workspacePathPrefix))
	}

	if filepath.IsAbs(inputPath) {
		return inputPath
	}

	return filepath.Join(workspace.MustWorking(), inputPath)
}

func parseDeployConfig(deployPath string) (*config.DeployConfig, error) {
	deployConfig, err := config.ParseDeployConfig(resolvePath(deployPath))
	if err != nil {
		return nil, fmt.Errorf("解析部署配置失败: %w", err)
	}

	return deployConfig, nil
}

func deployAndActivate(opts *options) error {
	active, err := env.Current()
	if err != nil {
		return fmt.Errorf("%s 需要当前已激活环境", commandDeploy)
	}

	deployConfig, err := parseDeployConfig(opts.target)
	if err != nil {
		return err
	}

	if err := active.Update(deployConfig); err != nil {
		return fmt.Errorf("更新环境失败: %w", err)
	}

	if err := active.Active(); err != nil {
		return fmt.Errorf("激活环境失败: %w", err)
	}

	fmt.Printf("环境 %s/%s 已激活\n", active.Name, active.App)
	return nil
}

func switchEnvironment(opts *options) error {
	deployEnv, err := env.Get(opts.target, opts.app)
	if err != nil {
		if !errors.Is(err, env.ErrNotFound) {
			return err
		}

		deployEnv, err = env.New(opts.target, opts.app)
		if err != nil {
			return fmt.Errorf("创建环境失败: %w", err)
		}
	}

	if err := deployEnv.Active(); err != nil {
		return err
	}

	fmt.Printf("已切换到环境 %s/%s\n", opts.target, opts.app)
	return nil
}

func deleteEnvironment(opts *options) error {
	deployEnv, err := env.Get(opts.target, opts.app)
	if err != nil {
		if errors.Is(err, env.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := deployEnv.Delete(); err != nil {
		return err
	}

	fmt.Printf("环境 %s/%s 已删除\n", opts.target, opts.app)
	return nil
}
