package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dominion/tools/deploy/v2/client"
	"github.com/spf13/pflag"
)

const (
	commandApply = "apply"
	commandDel   = "del"
	commandList  = "list"
	commandScope = "scope"

	flagEndpoint = "endpoint"
	flagTimeout  = "timeout"
	flagScope    = "scope"

	defaultEndpoint = "http://infra.liukexin.com"
	defaultTimeout  = 5 * time.Minute
)

type options struct {
	command   string
	target    string
	endpoint  string
	timeout   time.Duration
	scope     string
	apiClient *client.Client
}

type commandExecFunc func(opts *options) error
type commandValidatorFunc func(opts *options) error

type flagSpec struct {
	name         string
	defaultValue any
	usage        string
	bind         func(fs *pflag.FlagSet, opts *options, spec flagSpec)
}

var commandExecTable = map[string]commandExecFunc{
	commandApply: applyCommand,
	commandDel:   delCommand,
	commandList:  listCommand,
	commandScope: scopeCommand,
}

var commandValidatorTable = map[string]commandValidatorFunc{
	commandApply: validateApplyOptions,
	commandDel:   validateDelOptions,
	commandList:  validateListOptions,
	commandScope: validateScopeOptions,
}

var flagSpecs = map[string]flagSpec{
	flagEndpoint: {
		name:         flagEndpoint,
		defaultValue: defaultEndpoint,
		usage:        "deploy service endpoint",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.StringVar(&opts.endpoint, spec.name, spec.defaultValue.(string), spec.usage)
		},
	},
	flagTimeout: {
		name:         flagTimeout,
		defaultValue: defaultTimeout,
		usage:        "request timeout",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.DurationVar(&opts.timeout, spec.name, spec.defaultValue.(time.Duration), spec.usage)
		},
	},
	flagScope: {
		name:         flagScope,
		defaultValue: "",
		usage:        "environment scope",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.StringVar(&opts.scope, spec.name, spec.defaultValue.(string), spec.usage)
		},
	},
}

var commandFlagTable = map[string][]string{
	commandApply: {flagEndpoint, flagTimeout, flagScope},
	commandDel:   {flagEndpoint, flagTimeout, flagScope},
	commandList:  {flagEndpoint, flagTimeout, flagScope},
	commandScope: {flagEndpoint, flagTimeout, flagScope},
}

var stdout io.Writer = os.Stdout

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if isHelpArgs(args) {
		fmt.Fprint(stdout, usageText())
		return nil
	}

	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	exec, ok := commandExecTable[opts.command]
	if !ok {
		return fmt.Errorf("unknown command: %s", opts.command)
	}

	opts.apiClient = client.NewClient(opts.endpoint)
	return exec(opts)
}

func parseOptions(args []string) (*options, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("must provide command: %s, %s, %s or %s", commandApply, commandDel, commandList, commandScope)
	}

	fs, opts, err := newCommandFlagSet(args[0])
	if err != nil {
		return nil, err
	}
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}

	positionArgs := fs.Args()
	if len(positionArgs) > 1 {
		return nil, fmt.Errorf("%s accepts at most one positional arg", opts.command)
	}
	if len(positionArgs) == 1 {
		opts.target = strings.TrimSpace(positionArgs[0])
	}

	opts.endpoint = strings.TrimSpace(opts.endpoint)
	opts.scope = strings.TrimSpace(opts.scope)

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
			return nil, nil, fmt.Errorf("unknown flag spec: %s", flagName)
		}
		spec.bind(fs, opts, spec)
	}

	return fs, opts, nil
}

func validateOptions(opts *options) error {
	if opts.endpoint == "" {
		return fmt.Errorf("%s must not be empty", flagEndpoint)
	}
	if opts.timeout <= 0 {
		return fmt.Errorf("%s must be positive", flagTimeout)
	}
	if opts.scope != "" {
		if err := ValidateScope(opts.scope); err != nil {
			return err
		}
	}

	validator, ok := commandValidatorTable[opts.command]
	if !ok {
		return fmt.Errorf("unknown command: %s", opts.command)
	}
	return validator(opts)
}

func validateApplyOptions(opts *options) error {
	if opts.target == "" {
		return fmt.Errorf("%s requires deploy.yaml path", commandApply)
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

func validateScopeOptions(opts *options) error {
	if opts.target == "" {
		return nil
	}
	return ValidateScope(opts.target)
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
		"  apply [--endpoint=url] [--timeout=5m] [--scope=name] <deploy.yaml>",
		"  del [--endpoint=url] [--timeout=5m] [--scope=name] <env>",
		"  list [--endpoint=url] [--timeout=5m] [--scope=name]",
		"  scope [scope-name]",
	}, "\n") + "\n"
}
