package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	guitarconfig "dominion/tools/test/guitar/pkg/config"
	"dominion/tools/test/guitar/pkg/run"
	"dominion/tools/test/guitar/pkg/validate"
	"github.com/spf13/pflag"
)

const (
	commandValidate = "validate"
	commandRun      = "run"

	flagTimeout = "timeout"
	flagSuite   = "suite"

	defaultTimeout = 10 * time.Minute
)

type options struct {
	command   string
	target    string
	timeout   time.Duration
	verbose   bool
	suiteName string
}

type commandExecFunc func(opts *options) error

var commandExecTable = map[string]commandExecFunc{
	commandValidate: validateCommand,
	commandRun:      runCommand,
}

var stdout io.Writer = os.Stdout

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string) error {
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

	return exec(opts)
}

func parseOptions(args []string) (*options, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("must provide command: %s or %s", commandValidate, commandRun)
	}

	command := args[0]
	if _, ok := commandExecTable[command]; !ok {
		return nil, fmt.Errorf("unknown command: %s", command)
	}

	fs := pflag.NewFlagSet(command, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var timeout time.Duration
	var verbose bool
	fs.DurationVar(&timeout, flagTimeout, defaultTimeout, "overall execution timeout")
	fs.BoolVarP(&verbose, "verbose", "v", false, "show hidden information such as trace ID")

	var suiteName string
	if command == commandRun {
		fs.StringVar(&suiteName, flagSuite, "", "run only the suite with this exact name")
	}

	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}

	positionArgs := fs.Args()
	if len(positionArgs) != 1 {
		return nil, fmt.Errorf("%s requires exactly one positional arg <plan.yaml>", command)
	}

	opts := &options{
		command: command,
		target:  strings.TrimSpace(positionArgs[0]),
		timeout: timeout,
		verbose: verbose,
	}

	if opts.target == "" {
		return nil, fmt.Errorf("target must not be empty")
	}

	if command == commandRun {
		opts.suiteName = strings.TrimSpace(suiteName)
	}

	return opts, nil
}

func validateCommand(opts *options) error {
	cfg, err := guitarconfig.Parse(opts.target)
	if err != nil {
		return err
	}

	if err := validate.Validate(cfg); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Validation passed for %q\n", cfg.Name)
	return nil
}

func runCommand(opts *options) error {
	cfg, err := guitarconfig.Parse(opts.target)
	if err != nil {
		return err
	}

	ctx := tracecontext.Ensure(context.Background())
	if opts.verbose {
		fmt.Fprintf(stdout, "trace ID: %s\n", tracecontext.ID(ctx))
	}
	return run.Run(ctx, cfg, run.WithTimeout(opts.timeout), run.WithSuite(opts.suiteName))
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
		"Usage: guitar <command> [flags] <plan.yaml>",
		"",
		"Commands:",
		"  validate [--timeout=10m] <plan.yaml>",
		"  run [--timeout=10m] [--suite <name>] <plan.yaml>",
		"",
		"Flags:",
		"  --suite <name>   run only the suite with this exact name",
		"  -v, --verbose    show hidden information such as trace ID",
	}, "\n") + "\n"
}
