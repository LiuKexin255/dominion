package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"dominion/tools/deploy/pkg/workspace"
	guitarconfig "dominion/tools/guitar/pkg/config"
	"dominion/tools/guitar/pkg/env"
	"dominion/tools/guitar/pkg/validate"
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
)

// options configures Run behavior.
type options struct {
	timeout time.Duration
}

// Option configures Run behavior.
type Option func(*options)

// WithTimeout sets the overall execution timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
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

	for _, suite := range cfg.Suites {
		if err := runSuite(ctx, suite); err != nil {
			return err
		}
	}

	return nil
}

func runSuite(ctx context.Context, suite *guitarconfig.Suite) error {
	deployPath := workspace.ResolvePath(suite.Deploy)

	if err := runCommand(ctx, deployBinary, deployApplyCommand, deployPath); err != nil {
		return fmt.Errorf("deploy apply %s: %w", suite.Deploy, err)
	}

	testErr := runTests(ctx, suite)
	cleanupErr := runCommand(ctx, deployBinary, deployDeleteCommand, suite.Env)

	if testErr != nil {
		if cleanupErr != nil {
			return fmt.Errorf("%w; cleanup failed: deploy del %s: %v", testErr, suite.Env, cleanupErr)
		}
		return testErr
	}
	if cleanupErr != nil {
		fmt.Fprintf(stderr, "warning: cleanup failed: deploy del %s: %v\n", suite.Env, cleanupErr)
		return nil
	}

	return nil
}

func runTests(ctx context.Context, suite *guitarconfig.Suite) error {
	args := []string{bazelTestCommand, bazelLargeTestConfig}
	args = append(args, env.BuildTestEnvFlags(suite)...)
	args = append(args, suite.Cases...)

	if err := runCommand(ctx, bazelBinary, args...); err != nil {
		return fmt.Errorf("bazel test failed for suite %q: %w", suite.Name, err)
	}

	return nil
}

func defaultRunCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
