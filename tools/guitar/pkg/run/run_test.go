package run

import (
	"context"
	"os"
	"path/filepath"
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
	}{
		{
			name: "deploy failure",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "alpha.dev", "//case:a"))
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
				if len(calls) != 1 {
					t.Fatalf("calls = %d, want 1", len(calls))
				}
				assertCommand(t, calls[0], deployBinary, deployApplyCommand)
			},
		},
		{
			name: "test failure with cleanup",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "alpha.dev", "//case:a"))
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
				assertCommand(t, calls[2], deployBinary, deployDeleteCommand)
			},
		},
		{
			name: "cleanup failure",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(t, newSuite(t, root, "suite-a", "alpha.dev", "//case:a"))
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
				assertCommand(t, calls[2], deployBinary, deployDeleteCommand)
			},
		},
		{
			name: "fail fast",
			config: func(t *testing.T, root string) *guitarconfig.Config {
				return newConfig(
					t,
					newSuite(t, root, "suite-a", "alpha.dev", "//case:a"),
					newSuite(t, root, "suite-b", "beta.dev", "//case:b"),
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
				return newConfig(t, newSuite(t, root, "suite-a", "alpha.dev", "//case:a"))
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
				assertCommand(t, calls[2], deployBinary, deployDeleteCommand)
				if !slices.Contains(calls[1].args, bazelLargeTestConfig) {
					t.Fatalf("bazel args = %v, want %q", calls[1].args, bazelLargeTestConfig)
				}
				if !slices.Contains(calls[1].args, "--test_env=TESTTOOL_ENV=test.env") {
					t.Fatalf("bazel args = %v, want TESTTOOL_ENV flag", calls[1].args)
				}
				if !slices.Contains(calls[1].args, "//case:a") {
					t.Fatalf("bazel args = %v, want test target", calls[1].args)
				}
				if !slices.Contains(calls[2].args, "test.env") {
					t.Fatalf("cleanup args = %v, want env name", calls[2].args)
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
			defer func() {
				runCommand = originalRunCommand
			}()

			err := Run(context.Background(), cfg)

			tt.assertError(t, err)
			tt.assertCalls(t, calls)
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

func newSuite(t *testing.T, root string, suiteName string, deployName string, testCase string) *guitarconfig.Suite {
	t.Helper()

	deployPath := filepath.Join(root, suiteName+".yaml")
	raw := strings.Join([]string{
		"name: " + deployName,
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
		Env:    "test.env",
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
