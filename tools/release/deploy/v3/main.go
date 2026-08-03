package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/tools/release/deploy/v2/client"

	"github.com/spf13/pflag"
)

const (
	commandApply    = "apply"
	commandDel      = "del"
	commandDescribe = "describe"
	commandList     = "list"
	commandScope    = "scope"

	flagEndpoint = "endpoint"
	flagTimeout  = "timeout"
	flagScope    = "scope"
	flagRun      = "run"
	flagVerbose  = "verbose"

	defaultEndpoint = "http://infra.liukexin.com"
	defaultTimeout  = 5 * time.Minute
)

type options struct {
	command   string
	target    string
	endpoint  string
	timeout   time.Duration
	scope     string
	run       string
	verbose   bool
	apiClient *client.Client
}

type commandExecFunc func(ctx context.Context, opts *options) error
type commandValidatorFunc func(opts *options) error

type flagSpec struct {
	name         string
	defaultValue any
	usage        string
	bind         func(fs *pflag.FlagSet, opts *options, spec flagSpec)
}

var commandExecTable = map[string]commandExecFunc{
	commandApply:    applyCommand,
	commandDel:      delCommand,
	commandDescribe: describeCommand,
	commandList:     listCommand,
	commandScope:    scopeCommand,
}

var commandValidatorTable = map[string]commandValidatorFunc{
	commandApply:    validateApplyOptions,
	commandDel:      validateDelOptions,
	commandDescribe: validateDescribeOptions,
	commandList:     validateListOptions,
	commandScope:    validateScopeOptions,
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
	flagRun: {
		name:         flagRun,
		defaultValue: "",
		usage:        "run identifier for {{run}} placeholder in deploy name (apply only)",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.StringVar(&opts.run, spec.name, spec.defaultValue.(string), spec.usage)
		},
	},
	flagVerbose: {
		name:         flagVerbose,
		defaultValue: false,
		usage:        "show hidden information such as trace ID",
		bind: func(fs *pflag.FlagSet, opts *options, spec flagSpec) {
			fs.BoolVarP(&opts.verbose, spec.name, "v", spec.defaultValue.(bool), spec.usage)
		},
	},
}

var commandFlagTable = map[string][]string{
	commandApply:    {flagEndpoint, flagTimeout, flagScope, flagRun, flagVerbose},
	commandDel:      {flagEndpoint, flagTimeout, flagScope, flagVerbose},
	commandDescribe: {flagEndpoint, flagTimeout, flagScope, flagVerbose},
	commandList:     {flagEndpoint, flagTimeout, flagScope, flagVerbose},
	commandScope:    {flagEndpoint, flagTimeout, flagScope, flagVerbose},
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

	ctx := tracecontext.FromEnv(context.Background())
	if opts.verbose {
		fmt.Fprintf(stdout, "trace ID: %s\n", tracecontext.ID(ctx))
	}

	opts.apiClient = client.NewClient(opts.endpoint, client.WithHTTPTransport(tracecontext.NewHTTPTransport(nil)))
	return exec(ctx, opts)
}

func parseOptions(args []string) (*options, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("must provide command: %s, %s, %s, %s or %s", commandApply, commandDel, commandDescribe, commandList, commandScope)
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
	opts.run = strings.TrimSpace(opts.run)

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

func validateDescribeOptions(opts *options) error {
	if opts.target == "" {
		return fmt.Errorf("%s requires env name", commandDescribe)
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
		"Usage: deploy_v3 <command> [args]",
		"",
		"Commands:",
		"  apply [-v] [--endpoint=url] [--timeout=5m] [--scope=name] [--run=id] <deploy.yaml>",
		"  del [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>",
		"  describe [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>",
		"  list [-v] [--endpoint=url] [--timeout=5m] [--scope=name]",
		"  scope [-v] [--scope=name] [scope-name]",
		"",
		"Flags:",
		"  -v, --verbose   show hidden information such as trace ID",
	}, "\n") + "\n"
}
