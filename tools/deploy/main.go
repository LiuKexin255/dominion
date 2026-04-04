package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/env"
	"dominion/tools/deploy/pkg/k8s"
	"dominion/tools/deploy/pkg/workspace"

	"github.com/spf13/pflag"
)

const (
	commandUse    = "use"
	commandDeploy = "deploy"
	commandDel    = "del"
	commandList   = "list"
	commandCur    = "cur"

	flagApp        = "app"
	flagKubeconfig = "kubeconfig"
)

type options struct {
	command        string
	target         string
	app            string
	kubeconfigPath string
}

type executor interface {
	Apply(context.Context, *k8s.DeployObjects) error
	Delete(context.Context, string, string) error
}

func (o *options) Default() error {
	// app 为空时使用当前激活的环境 app
	if o.app == "" {
		switch o.command {
		case commandUse, commandDel:
			app, err := env.DefaultApp()
			if err != nil {
				return err
			}
			o.app = app
		}
	}

	o.app = strings.TrimSpace(o.app)
	o.target = strings.TrimSpace(o.target)
	o.kubeconfigPath = strings.TrimSpace(o.kubeconfigPath)

	return nil
}

type flagSpec struct {
	name         string
	defaultValue string
	usage        string
	bind         func(fs *pflag.FlagSet, opts *options, spec flagSpec)
}

var flagSpecs = map[string]flagSpec{
	flagApp: {
		name:         flagApp,
		defaultValue: "",
		usage:        "application name",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.StringVar(&opts.app, spec.name, spec.defaultValue, spec.usage)
		},
	},
	flagKubeconfig: {
		name:         flagKubeconfig,
		defaultValue: "/var/snap/microk8s/current/credentials/client.config",
		usage:        "path to kubeconfig; empty uses client-go default loading rules",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.StringVar(&opts.kubeconfigPath, spec.name, spec.defaultValue, spec.usage)
		},
	},
}

var commandFlagTable = map[string][]string{
	commandUse:    {flagApp},
	commandDeploy: {flagKubeconfig},
	commandDel:    {flagApp, flagKubeconfig},
	commandList:   {},
	commandCur:    {},
}

var runtimeClientFactory = k8s.NewRuntimeClient

// commandExecFunc 命令执行方法
type commandExecFunc = func(opts *options) error

// commandValidatorFunc 命令校验方法
type commandValidatorFunc = func(opts *options) error

var commandExecTable = map[string]commandExecFunc{
	commandUse:    switchEnvironment,
	commandDeploy: deployAndActivate,
	commandDel:    deleteEnvironment,
	commandList:   listEnvironments,
	commandCur:    showCurrentEnvironment,
}

var commandValidatorTable = map[string]commandValidatorFunc{
	commandUse:    validateUseOptions,
	commandDeploy: validateDeployOptions,
	commandDel:    validateDelOptions,
	commandList:   validateListOptions,
	commandCur:    validateCurOptions,
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if isHelpArgs(args) {
		fmt.Fprint(os.Stdout, usageText())
		return nil
	}

	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	env.LazyInit()

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
		return nil, fmt.Errorf("必须提供命令：%s、%s、%s、%s 或 %s", commandUse, commandDeploy, commandDel, commandList, commandCur)
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

func newCommandFlagSet(command string) (*pflag.FlagSet, *options, error) {
	flagNames, ok := commandFlagTable[command]
	if !ok {
		return nil, nil, fmt.Errorf("unknown command: %s", command)
	}

	fs := pflag.NewFlagSet(command, pflag.ContinueOnError)
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
	Validator, ok := commandValidatorTable[opts.command]
	if !ok {
		return fmt.Errorf("unknown command: %s", opts.command)
	}

	return Validator(opts)
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

func validateListOptions(opts *options) error {
	if opts.target != "" {
		return fmt.Errorf("%s does not accept positional args", commandList)
	}
	return nil
}

func validateCurOptions(opts *options) error {
	if opts.target != "" {
		return fmt.Errorf("%s does not accept positional args", commandCur)
	}
	return nil
}

func parseDeployConfig(deployPath string) (*config.DeployConfig, error) {
	deployConfig, err := config.ParseDeployConfig(workspace.ResolvePath(deployPath))
	if err != nil {
		return nil, fmt.Errorf("解析部署配置失败: %w", err)
	}

	return deployConfig, nil
}

func newExecutor(opts *options) (executor, error) {
	runtimeClient, err := runtimeClientFactory(opts.kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("创建 runtime client 失败: %w", err)
	}
	return k8s.NewExecutor(runtimeClient), nil
}

func deployAndActivate(opts *options) error {
	active, err := env.Current()
	if err != nil {
		return fmt.Errorf("%s 需要当前已激活环境，请先执行 `%s <env>`", commandDeploy, commandUse)
	}

	deployConfig, err := parseDeployConfig(opts.target)
	if err != nil {
		return err
	}

	if err := active.Update(deployConfig); err != nil {
		return fmt.Errorf("更新环境配置失败: %w", err)
	}

	exec, err := newExecutor(opts)
	if err != nil {
		return err
	}

	if err := active.Deploy(context.Background(), exec); err != nil {
		return fmt.Errorf("部署环境失败: %w", err)
	}

	if err := active.Active(); err != nil {
		return fmt.Errorf("激活环境失败: %w", err)
	}

	fmt.Printf("环境 %s/%s 已部署\n", active.Name, active.App)
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
		return err
	}

	exec, err := newExecutor(opts)
	if err != nil {
		return err
	}

	if err := deployEnv.Delete(context.Background(), exec); err != nil {
		return err
	}

	fmt.Printf("环境 %s/%s 已删除\n", opts.target, opts.app)
	return nil
}

func listEnvironments(_ *options) error {
	envs, err := env.List()
	if err != nil {
		return err
	}

	if len(envs) == 0 {
		fmt.Println("暂无环境")
		return nil
	}

	for _, item := range envs {
		fmt.Printf("%s/%s\n", item.App, item.Name)
	}

	return nil
}

func showCurrentEnvironment(_ *options) error {
	active, err := env.Current()
	if err != nil {
		return err
	}

	fmt.Println(active)
	return nil
}

func isHelpArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch args[0] {
	case "--help", "-h", "help":
		return true
	default:
		return false
	}
}

func usageText() string {
	return strings.Join([]string{
		"Usage: deploy <command> [args]",
		"",
		"Commands:",
		"  use <env> [--app=app]   创建或切换环境",
		"  deploy [--kubeconfig=path] <deploy.yaml>  读取部署配置并执行部署",
		"  del [--app=app] [--kubeconfig=path] <env> 删除环境",
		"  list                    列出环境",
		"  cur                     查看当前激活环境",
	}, "\n") + "\n"
}
