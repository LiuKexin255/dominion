package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	deployconfig "dominion/tools/release/deploy/pkg/config"
	"dominion/tools/release/deploy/pkg/workspace"
	guitarconfig "dominion/tools/test/guitar/pkg/config"
	"dominion/tools/test/guitar/pkg/env"
	"dominion/tools/test/guitar/pkg/runid"
	"dominion/tools/test/guitar/pkg/validate"
)

const (
	bazelBinary          = "bazel"
	bazelTestCommand     = "test"
	bazelLargeTestConfig = "--config=largetest"
	deployBinary         = "deploy"
	deployApplyCommand   = "apply"
	deployDeleteCommand  = "del"
)

var (
	// stdout is the default writer for command standard output.
	stdout io.Writer = os.Stdout
	// stderr is the default writer for command standard error.
	stderr io.Writer = os.Stderr
	// runCommand executes external commands. Tests replace it with a stub.
	runCommand = defaultRunCommand
	// generateRunID creates the per-suite run identifier. Tests replace it with a stub.
	generateRunID = runid.Generate
)

// options configures Run behavior.
type options struct {
	timeout     time.Duration
	suiteFilter string
}

// Option configures Run behavior.
type Option func(*options)

// WithTimeout sets the overall execution timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

// WithSuite filters execution to the suite with the given exact name.
func WithSuite(name string) Option {
	return func(o *options) {
		o.suiteFilter = name
	}
}

// Run executes the full testplan: validate, deploy, test, then cleanup.
func Run(ctx context.Context, cfg *guitarconfig.Config, opts ...Option) error {
	o := new(options)
	for _, opt := range opts {
		opt(o)
	}

	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	if err := validate.Validate(cfg); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	r := NewReporter(stdout)

	if o.suiteFilter != "" {
		for _, s := range cfg.Suites {
			if s.Name == o.suiteFilter {
				cfg.Suites = []*guitarconfig.Suite{s}
				break
			}
		}
		if len(cfg.Suites) != 1 || cfg.Suites[0].Name != o.suiteFilter {
			names := make([]string, len(cfg.Suites))
			for i, s := range cfg.Suites {
				names[i] = s.Name
			}
			return fmt.Errorf("suite %q not found. Available suites: %s", o.suiteFilter, strings.Join(names, ", "))
		}
	}

	for _, suite := range cfg.Suites {
		err := runSuite(ctx, suite, r)
		if err != nil {
			r.SuiteStatus(suite.Name, statusFailure, err)
			return err
		}
		r.SuiteStatus(suite.Name, statusSuccess, nil)
	}

	return nil
}

func runSuite(ctx context.Context, suite *guitarconfig.Suite, r *Reporter) (err error) {
	deployPath := workspace.ResolvePath(suite.Deploy)
	runID, genErr := generateRunID()
	if genErr != nil {
		return fmt.Errorf("generate run id for suite %q: %w", suite.Name, genErr)
	}

	deployCfg, parseErr := deployconfig.ParseDeployConfig(deployPath)
	if parseErr != nil {
		return fmt.Errorf("parse deploy config %s: %w", suite.Deploy, parseErr)
	}
	scope, _, ok := strings.Cut(deployCfg.Name, ".")
	if !ok {
		return fmt.Errorf("deploy name %q must contain scope", deployCfg.Name)
	}
	fullEnvName := fmt.Sprintf("%s.%s", scope, runID)
	r.SuiteHeader(suite.Name, runID, fullEnvName, suite.Deploy)

	defer func() {
		r.Step("Cleanup")
		cleanupErr := runCommand(context.WithoutCancel(ctx), deployBinary, deployDeleteCommand, fullEnvName)
		if err != nil {
			if cleanupErr != nil {
				err = fmt.Errorf("%w; cleanup failed: deploy del %s: %v", err, fullEnvName, cleanupErr)
			}
			return
		}
		if cleanupErr != nil {
			fmt.Fprintf(stderr, "warning: cleanup failed: deploy del %s: %v\n", fullEnvName, cleanupErr)
		}
	}()

	r.Step("Deploy")
	if applyErr := runCommand(ctx, deployBinary, deployApplyCommand, "--run", runID, deployPath); applyErr != nil {
		return fmt.Errorf("deploy apply %s: %w", suite.Deploy, applyErr)
	}

	r.Step("Test")
	if testErr := runTests(ctx, suite, fullEnvName); testErr != nil {
		return testErr
	}

	return nil
}

func runTests(ctx context.Context, suite *guitarconfig.Suite, envName string) error {
	args := []string{bazelTestCommand, bazelLargeTestConfig}
	args = append(args, env.BuildTestEnvFlags(suite, envName)...)
	args = append(args, suite.Cases...)

	testCtx := ctx
	if suite.Timeout > 0 {
		var cancel context.CancelFunc
		testCtx, cancel = context.WithTimeout(ctx, time.Duration(suite.Timeout)*time.Second)
		defer cancel()
	}

	if err := runCommand(testCtx, bazelBinary, args...); err != nil {
		return fmt.Errorf("bazel test failed for suite %q: %w", suite.Name, err)
	}

	return nil
}

func defaultRunCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), tracecontext.Environ(ctx)...)
	return cmd.Run()
}
