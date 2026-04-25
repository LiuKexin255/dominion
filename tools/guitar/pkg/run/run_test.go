package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	guitarconfig "dominion/tools/guitar/pkg/config"
)

type commandCall struct {
	name string
	args []string
}

func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		config      func(t *testing.T, root string) *guitarconfig.Config
		run         func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error
		assertError func(t *testing.T, err error)
		assertCalls func(t *testing.T, calls []commandCall)
		assertLog   func(t *testing.T, output string, calls []commandCall)
	}{
		{
			name: "deploy failure",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "//case:a"))
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					if name == deployBinary && len(args) >= 2 && args[0] == deployApplyCommand {
						return context.DeadlineExceeded
					}
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "deploy apply") {
					t.Fatalf("Run() error = %v, want deploy apply error", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 2 {
					t.Fatalf("calls = %d, want 2", len(calls))
				}
				assertCommand(t, calls[0], deployBinary, deployApplyCommand)
				runID := assertDeployApplyWithRun(t, calls[0])
				assertCleanupEnv(t, calls[1], "game."+runID)
			},
		},
		{
			name: "test failure with cleanup",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "//case:a"))
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					if name == bazelBinary {
						return context.Canceled
					}
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "bazel test failed") {
					t.Fatalf("Run() error = %v, want bazel test error", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 3 {
					t.Fatalf("calls = %d, want 3", len(calls))
				}
				assertCommand(t, calls[0], deployBinary, deployApplyCommand)
				assertCommand(t, calls[1], bazelBinary, bazelTestCommand)
				runID := assertDeployApplyWithRun(t, calls[0])
				assertTestEnv(t, calls[1], "game."+runID)
				assertCleanupEnv(t, calls[2], "game."+runID)
			},
		},
		{
			name: "cleanup failure",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "//case:a"))
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					if name == deployBinary && len(args) >= 2 && args[0] == deployDeleteCommand {
						return context.DeadlineExceeded
					}
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err != nil {
					t.Fatalf("Run() error = %v, want nil", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 3 {
					t.Fatalf("calls = %d, want 3", len(calls))
				}
				runID := assertDeployApplyWithRun(t, calls[0])
				assertCleanupEnv(t, calls[2], "game."+runID)
			},
		},
		{
			name: "fail fast",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(
					t,
					newSuite(t, root, "suite-a", "//case:a"),
					newSuite(t, root, "suite-b", "//case:b"),
				)
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				var bazelCalls int
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					if name == bazelBinary {
						bazelCalls++
						if bazelCalls == 1 {
							return context.Canceled
						}
					}
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "suite-a") {
					t.Fatalf("Run() error = %v, want first suite error", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 3 {
					t.Fatalf("calls = %d, want 3", len(calls))
				}
				for _, call := range calls {
					if slices.Contains(call.args, "//case:b") {
						t.Fatalf("calls contain second suite execution: %+v", calls)
					}
				}
			},
		},
		{
			name: "validation failure",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return &guitarconfig.Config{}
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "validation failed") {
					t.Fatalf("Run() error = %v, want validation error", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 0 {
					t.Fatalf("calls = %d, want 0", len(calls))
				}
			},
		},
		{
			name: "success",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "//case:a"))
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err != nil {
					t.Fatalf("Run() error = %v, want nil", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 3 {
					t.Fatalf("calls = %d, want 3", len(calls))
				}
				assertCommand(t, calls[0], deployBinary, deployApplyCommand)
				assertCommand(t, calls[1], bazelBinary, bazelTestCommand)
				runID := assertDeployApplyWithRun(t, calls[0])
				fullEnvName := "game." + runID
				assertCleanupEnv(t, calls[2], fullEnvName)
				if !slices.Contains(calls[1].args, bazelLargeTestConfig) {
					t.Fatalf("bazel args = %v, want %q", calls[1].args, bazelLargeTestConfig)
				}
				assertTestEnv(t, calls[1], fullEnvName)
				if !slices.Contains(calls[1].args, "//case:a") {
					t.Fatalf("bazel args = %v, want test target", calls[1].args)
				}
			},
			assertLog: func(t *testing.T, output string, calls []commandCall) {
				runID := deployRunID(t, calls[0])
				for _, want := range []string{"suite suite-a:", "run=" + runID, "env=game." + runID, "deploy="} {
					if !strings.Contains(output, want) {
						t.Fatalf("log output = %q, want %q", output, want)
					}
				}
			},
		},
		{
			name: "run id failure",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "//case:a"))
			},
			run: func(t *testing.T, calls *[]commandCall) func(context.Context, string, ...string) error {
				return func(_ context.Context, name string, args ...string) error {
					*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
					return nil
				}
			},
			assertError: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "generate run id") {
					t.Fatalf("Run() error = %v, want run id error", err)
				}
			},
			assertCalls: func(t *testing.T, calls []commandCall) {
				if len(calls) != 0 {
					t.Fatalf("calls = %d, want 0", len(calls))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newBazelWorkspace(t)
			cfg := tt.config(t, root)

			var calls []commandCall
			originalRunCommand := runCommand
			runCommand = tt.run(t, &calls)
			var logOutput bytes.Buffer
			originalStdout := stdout
			stdout = &logOutput
			if tt.name == "run id failure" {
				originalGenerateRunID := generateRunID
				generateRunID = func() (string, error) {
					return "", context.Canceled
				}
				defer func() {
					generateRunID = originalGenerateRunID
				}()
			}
			defer func() {
				runCommand = originalRunCommand
				stdout = originalStdout
			}()

			err := Run(context.Background(), cfg)

			tt.assertError(t, err)
			tt.assertCalls(t, calls)
			if tt.assertLog != nil {
				tt.assertLog(t, logOutput.String(), calls)
			}
		})
	}
}

func newConfig(t *testing.T, suites ...*guitarconfig.Suite) *guitarconfig.Config {
	t.Helper()

	return &guitarconfig.Config{
		Name:        "testplan",
		Description: "test plan",
		Suites:      suites,
	}
}

func newSuite(t *testing.T, root string, suiteName string, testCase string) *guitarconfig.Suite {
	t.Helper()

	deployPath := filepath.Join(root, suiteName+".yaml")
	raw := strings.Join([]string{
		"name: game.{{run}}",
		"desc: test deploy",
		"type: test",
		"services:",
		"  - infra:",
		"      resource: mongodb",
		"      profile: dev-single",
		"      name: mongo",
		"      app: guitar",
		"      persistence:",
		"        enabled: true",
	}, "\n")
	if err := os.WriteFile(deployPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", deployPath, err)
	}

	return &guitarconfig.Suite{
		Name:   suiteName,
		Deploy: deployPath,
		Cases:  []string{testCase},
	}
}

func newBazelWorkspace(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(MODULE.bazel) failed: %v", err)
	}
	withWorkingDir(t, root)
	return root
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}

func assertCommand(t *testing.T, call commandCall, wantName string, wantArg string) {
	t.Helper()

	if call.name != wantName {
		t.Fatalf("command name = %q, want %q", call.name, wantName)
	}
	if len(call.args) == 0 || call.args[0] != wantArg {
		t.Fatalf("command args = %v, want first arg %q", call.args, wantArg)
	}
}

func assertDeployApplyWithRun(t *testing.T, call commandCall) string {
	t.Helper()

	assertCommand(t, call, deployBinary, deployApplyCommand)
	runID := deployRunID(t, call)
	if !regexp.MustCompile(`^lt[a-z0-9]{6}$`).MatchString(runID) {
		t.Fatalf("deploy args = %v, want run ID format", call.args)
	}
	return runID
}

func deployRunID(t *testing.T, call commandCall) string {
	t.Helper()

	for i := 0; i+1 < len(call.args); i++ {
		if call.args[i] == "--run" {
			return call.args[i+1]
		}
	}
	t.Fatalf("deploy args = %v, want --run flag", call.args)
	return ""
}

func assertCleanupEnv(t *testing.T, call commandCall, wantEnvName string) {
	t.Helper()

	assertCommand(t, call, deployBinary, deployDeleteCommand)
	if !slices.Contains(call.args, wantEnvName) {
		t.Fatalf("cleanup args = %v, want %q", call.args, wantEnvName)
	}
	if slices.Contains(call.args, "test.env") {
		t.Fatalf("cleanup args = %v, must not use suite Env", call.args)
	}
}

func assertTestEnv(t *testing.T, call commandCall, wantEnvName string) {
	t.Helper()

	for _, key := range []string{"TESTTOOL_ENV", "DOMINION_ENVIRONMENT"} {
		flag := "--test_env=" + key + "=" + wantEnvName
		if !slices.Contains(call.args, flag) {
			t.Fatalf("bazel args = %v, want %q", call.args, flag)
		}
	}
}
