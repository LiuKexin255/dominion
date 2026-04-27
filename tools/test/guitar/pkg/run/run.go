package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

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

func runSuite(ctx context.Context, suite *guitarconfig.Suite) (err error) {
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
	fmt.Fprintf(stdout, "suite %s: run=%s env=%s deploy=%s\n", suite.Name, runID, fullEnvName, suite.Deploy)

	defer func() {
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

	if applyErr := runCommand(ctx, deployBinary, deployApplyCommand, "--run", runID, deployPath); applyErr != nil {
		return fmt.Errorf("deploy apply %s: %w", suite.Deploy, applyErr)
	}

	if testErr := runTests(ctx, suite, fullEnvName); testErr != nil {
		return testErr
	}

	return nil
}

func runTests(ctx context.Context, suite *guitarconfig.Suite, envName string) error {
	args := []string{bazelTestCommand, bazelLargeTestConfig}
	args = append(args, env.BuildTestEnvFlags(suite, envName)...)
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
