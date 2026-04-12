package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dominion/tools/deploy/pkg/env"
	"dominion/tools/deploy/pkg/k8s"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "empty args", args: nil, wantErr: true},
		{name: "use full env name", args: []string{"use", "alice.dev"}},
		{name: "apply only", args: []string{"apply", "deploy.yaml"}},
		{name: "apply with kubeconfig", args: []string{"apply", "--kubeconfig=/tmp/kubeconfig", "deploy.yaml"}},
		{name: "del full env name", args: []string{"del", "alice.dev"}},
		{name: "del with kubeconfig", args: []string{"del", "--kubeconfig=/tmp/kubeconfig", "alice.dev"}},
		{name: "scope view", args: []string{"scope"}},
		{name: "scope only", args: []string{"scope", "team"}},
		{name: "scope invalid", args: []string{"scope", "TEAM"}, wantErr: true},
		{name: "list only", args: []string{"list"}},
		{name: "list with positional arg", args: []string{"list", "dev"}, wantErr: true},
		{name: "cur only", args: []string{"cur"}},
		{name: "cur with positional arg", args: []string{"cur", "dev"}, wantErr: true},
		{name: "unknown command", args: []string{"switch", "dev"}, wantErr: true},
		{name: "use missing env", args: []string{"use"}, wantErr: true},
		{name: "apply missing path", args: []string{"apply"}, wantErr: true},
		{name: "unknown option", args: []string{"use", "--env=dev", "dev"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args)
			if tt.wantErr && err == nil {
				t.Fatalf("parseOptions(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
		})
	}
}

func TestParseOptions_NormalizesShortEnvNameUsingDefaultScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".env"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	withWorkingDir(t, filepath.Join(root, "apps", "svc"))

	if err := (&env.DeployContext{DefaultScope: "team"}).Save(); err != nil {
		t.Fatalf("DeployContext.Save() failed: %v", err)
	}

	opts, err := parseOptions([]string{"use", "dev"})
	if err != nil {
		t.Fatalf("parseOptions() unexpected error: %v", err)
	}
	if got, want := opts.target, "team.dev"; got != want {
		t.Fatalf("parseOptions() target = %q, want %q", got, want)
	}
}

func TestParseOptions_NormalizesDeleteShortEnvNameUsingDefaultScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".env"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	withWorkingDir(t, filepath.Join(root, "apps", "svc"))

	if err := (&env.DeployContext{DefaultScope: "team"}).Save(); err != nil {
		t.Fatalf("DeployContext.Save() failed: %v", err)
	}

	opts, err := parseOptions([]string{"del", "dev"})
	if err != nil {
		t.Fatalf("parseOptions() unexpected error: %v", err)
	}
	if got, want := opts.target, "team.dev"; got != want {
		t.Fatalf("parseOptions() target = %q, want %q", got, want)
	}
}

func TestParseOptions_ShortEnvNameWithoutDefaultScopeFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".env"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	withWorkingDir(t, filepath.Join(root, "apps", "svc"))

	if _, err := parseOptions([]string{"use", "dev"}); err == nil {
		t.Fatal("parseOptions() succeeded unexpectedly")
	} else if !strings.Contains(err.Error(), env.ErrNoDefaultScope.Error()) {
		t.Fatalf("parseOptions() error = %v, want missing default scope", err)
	}
}

func TestOptionsDefault_TrimsWhitespace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".env"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	withWorkingDir(t, filepath.Join(root, "apps", "svc"))

	opts := &options{target: "  dev  ", kubeconfigPath: "  /tmp/kubeconfig  "}
	if err := opts.Default(); err != nil {
		t.Fatalf("Default() unexpected error: %v", err)
	}
	if got, want := opts.target, "dev"; got != want {
		t.Fatalf("Default() target = %q, want %q", got, want)
	}
	if got, want := opts.kubeconfigPath, "/tmp/kubeconfig"; got != want {
		t.Fatalf("Default() kubeconfigPath = %q, want %q", got, want)
	}
}

func TestScopeCommand_SetsDefaultScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".env"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	withWorkingDir(t, filepath.Join(root, "apps", "svc"))

	if err := scopeCommand(&options{target: "team"}); err != nil {
		t.Fatalf("scopeCommand() unexpected error: %v", err)
	}

	ctx, err := env.LoadDeployContext()
	if err != nil {
		t.Fatalf("env.LoadDeployContext() failed: %v", err)
	}
	if got, want := ctx.GetDefaultScope(), "team"; got != want {
		t.Fatalf("GetDefaultScope() = %q, want %q", got, want)
	}
}

func TestScopeCommand_ShowsDefaultScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".env"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	withWorkingDir(t, filepath.Join(root, "apps", "svc"))

	if err := (&env.DeployContext{DefaultScope: "team"}).Save(); err != nil {
		t.Fatalf("DeployContext.Save() failed: %v", err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	if err := scopeCommand(&options{}); err != nil {
		t.Fatalf("scopeCommand() unexpected error: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("stdout close failed: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll() failed: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "team" {
		t.Fatalf("scopeCommand() output = %q, want %q", got, "team")
	}
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

func TestNewExecutor_UsesKubeconfigPath(t *testing.T) {
	originalFactory := runtimeClientFactory
	t.Cleanup(func() { runtimeClientFactory = originalFactory })

	var gotPath string
	runtimeClientFactory = func(kubeconfigPath string) (*k8s.RuntimeClient, error) {
		gotPath = kubeconfigPath
		return &k8s.RuntimeClient{}, nil
	}

	_, err := newExecutor(&options{kubeconfigPath: " /tmp/microk8s.conf "})
	if err != nil {
		t.Fatalf("newExecutor() unexpected error: %v", err)
	}
	if gotPath != " /tmp/microk8s.conf " {
		t.Fatalf("newExecutor() kubeconfig path = %q, want %q", gotPath, " /tmp/microk8s.conf ")
	}
}

func TestRun_Help(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("run(--help) failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("stdout close failed: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll() failed: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "Usage: deploy <command> [args]") {
		t.Fatalf("help output missing usage text: %q", got)
	}
	if !strings.Contains(got, "--kubeconfig=path") {
		t.Fatalf("help output missing kubeconfig flag: %q", got)
	}
}

func TestRun_OutsideGitRepoReturnsError(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "apps", "svc"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.Chdir(filepath.Join(outside, "apps", "svc")); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", filepath.Join(outside, "apps", "svc"), err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
	var (
		recovered any
		runErr    error
	)
	func() {
		defer func() {
			recovered = recover()
		}()
		runErr = run([]string{"cur"})
	}()

	if recovered != nil {
		t.Fatalf("run() panicked: %v", recovered)
	}
	if runErr == nil {
		t.Fatal("run() succeeded unexpectedly")
	}
	if !strings.Contains(runErr.Error(), "没有激活中的环境") {
		t.Fatalf("run() error = %v, want missing active env message", runErr)
	}
}
