package imagepush

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type stubRunner struct {
	targets []string
	runFunc func(ctx context.Context, pushTarget string) (*PushOutput, error)
}

func (r *stubRunner) Run(ctx context.Context, pushTarget string) (*PushOutput, error) {
	r.targets = append(r.targets, pushTarget)
	if r.runFunc == nil {
		return nil, nil
	}
	return r.runFunc(ctx, pushTarget)
}

type commandCall struct {
	dir  string
	name string
	args []string
}

type stubCommandExecutor struct {
	calls   []commandCall
	runFunc func(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

func (e *stubCommandExecutor) CombinedOutput(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	e.calls = append(e.calls, commandCall{dir: dir, name: name, args: append([]string(nil), args...)})
	if e.runFunc == nil {
		return nil, nil
	}
	return e.runFunc(ctx, dir, name, args...)
}

func TestResult_ImageRef(t *testing.T) {
	tests := []struct {
		name    string
		result  Result
		want    string
		wantErr bool
	}{
		{
			name:   "digest image ref",
			result: Result{URL: "registry.example.com/team/service", Dest: "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
			want:   "registry.example.com/team/service@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		{
			name:    "tag destination rejected",
			result:  Result{URL: "registry.example.com/team/service", Dest: "git-abc123"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.result.ImageRef()
			if tt.wantErr {
				if err == nil {
					t.Fatal("ImageRef() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("ImageRef() failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ImageRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDerivePushTarget(t *testing.T) {
	tests := []struct {
		name           string
		artifactTarget string
		want           string
		wantErr        bool
	}{
		{
			name:           "full label target",
			artifactTarget: "//experimental/grpc_hello_world/service:service_image",
			want:           "//experimental/grpc_hello_world/service:service_image_push",
		},
		{
			name:           "short package label",
			artifactTarget: "//pkg:gateway.image",
			want:           "//pkg:gateway.image_push",
		},
		{
			name:    "missing label rejected",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DerivePushTarget(tt.artifactTarget)
			if tt.wantErr {
				if err == nil {
					t.Fatal("DerivePushTarget() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("DerivePushTarget() failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("DerivePushTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolver_CachesByPushTarget(t *testing.T) {
	runner := &stubRunner{
		runFunc: func(ctx context.Context, pushTarget string) (*PushOutput, error) {
			switch pushTarget {
			case "//service:service_image_push":
				return &PushOutput{
					Repository: "registry.example.com/team/service",
					Digest:     "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				}, nil
			case "//gateway:service_image_push":
				return &PushOutput{
					Repository: "registry.example.com/team/gateway",
					Digest:     "sha256:2222222222222222222222222222222222222222222222222222222222222222",
				}, nil
			default:
				return nil, errors.New("unexpected push target")
			}
		},
	}

	resolver := NewResolver(runner)

	first, err := resolver.Resolve(context.Background(), "//service:service_image")
	if err != nil {
		t.Fatalf("Resolve() first call failed: %v", err)
	}
	second, err := resolver.Resolve(context.Background(), "//service:service_image")
	if err != nil {
		t.Fatalf("Resolve() second call failed: %v", err)
	}
	other, err := resolver.Resolve(context.Background(), "//gateway:service_image")
	if err != nil {
		t.Fatalf("Resolve() third call failed: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Resolve() cached result mismatch: first=%v second=%v", first, second)
	}
	if other.URL != "registry.example.com/team/gateway" {
		t.Fatalf("Resolve() other URL = %q, want gateway repository", other.URL)
	}
	if !reflect.DeepEqual(runner.targets, []string{"//service:service_image_push", "//gateway:service_image_push"}) {
		t.Fatalf("runner targets = %v, want cache keyed by full derived push target", runner.targets)
	}
}

func TestResolver_ParsesRepositoryAndDigestContract(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		manifest   []string
		files      map[string]string
		wantResult *Result
	}{
		{
			name: "repository field wins",
			script: strings.Join([]string{
				`readonly IMAGE_DIR="$(rlocation "_main/example/service_image")"`,
				`readonly FIXED_ARGS=("--repository" "registry.example.com/team/service")`,
				`readonly REPOSITORY_FILE="$(rlocation "_main/example/repository.txt")"`,
			}, "\n"),
			manifest: []string{
				"_main/example/service_image {WORKSPACE}/fixtures/service_image",
				"_main/example/repository.txt {WORKSPACE}/fixtures/repository.txt",
			},
			files: map[string]string{
				"fixtures/service_image/index.json": `{"manifests":[{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`,
				"fixtures/repository.txt":           "registry.example.com/team/ignored-from-file\n",
			},
			wantResult: &Result{
				URL:  "registry.example.com/team/service",
				Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
		{
			name: "repository file fallback",
			script: strings.Join([]string{
				`readonly IMAGE_DIR="$(rlocation "_main/example/gateway_image")"`,
				`readonly FIXED_ARGS=()`,
				`readonly REPOSITORY_FILE="$(rlocation "_main/example/repository.txt")"`,
			}, "\n"),
			manifest: []string{
				"_main/example/gateway_image {WORKSPACE}/fixtures/gateway_image",
				"_main/example/repository.txt {WORKSPACE}/fixtures/repository.txt",
			},
			files: map[string]string{
				"fixtures/gateway_image/index.json": `{"manifests":[{"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`,
				"fixtures/repository.txt":           "registry.example.com/team/gateway\n",
			},
			wantResult: &Result{
				URL:  "registry.example.com/team/gateway",
				Dest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			scriptRelPath := filepath.ToSlash("bazel-out/k8-fastbuild/bin/example/push_service_image_push.sh")
			scriptPath := filepath.Join(workspaceRoot, filepath.FromSlash(scriptRelPath))
			writeTestFile(t, scriptPath, tt.script)

			manifestLines := make([]string, 0, len(tt.manifest))
			for _, line := range tt.manifest {
				manifestLines = append(manifestLines, strings.ReplaceAll(line, "{WORKSPACE}", filepath.ToSlash(workspaceRoot)))
			}
			writeTestFile(t, scriptPath+".runfiles_manifest", strings.Join(manifestLines, "\n")+"\n")

			for relPath, content := range tt.files {
				writeTestFile(t, filepath.Join(workspaceRoot, relPath), content)
			}

			runner, exec := newTestBazelRunner(workspaceRoot, scriptRelPath, nil)
			resolver := NewResolver(runner)

			got, err := resolver.Resolve(context.Background(), "//experimental/grpc_hello_world/service:service_image")
			if err != nil {
				t.Fatalf("Resolve() failed: %v", err)
			}
			if !reflect.DeepEqual(got, tt.wantResult) {
				t.Fatalf("Resolve() = %v, want %v", got, tt.wantResult)
			}

			imageRef, err := got.ImageRef()
			if err != nil {
				t.Fatalf("ImageRef() failed: %v", err)
			}
			if imageRef != tt.wantResult.URL+"@"+tt.wantResult.Dest {
				t.Fatalf("ImageRef() = %q, want %q", imageRef, tt.wantResult.URL+"@"+tt.wantResult.Dest)
			}
			if gotSubcommands(exec.calls) != "run,cquery" {
				t.Fatalf("bazel subcommands = %s, want run,cquery", gotSubcommands(exec.calls))
			}
		})
	}
}

func TestResolver_RejectsMissingRepositoryOrDigest(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		manifest    []string
		files       map[string]string
		runErr      error
		errContains string
	}{
		{
			name:        "missing repository",
			script:      strings.Join([]string{`readonly IMAGE_DIR="$(rlocation "_main/example/service_image")"`, `readonly FIXED_ARGS=()`, `readonly REPOSITORY_FILE="$(rlocation "{{repository_file}}")"`}, "\n"),
			manifest:    []string{"_main/example/service_image {WORKSPACE}/fixtures/service_image"},
			files:       map[string]string{"fixtures/service_image/index.json": `{"manifests":[{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`},
			errContains: "repository",
		},
		{
			name:        "missing digest",
			script:      strings.Join([]string{`readonly IMAGE_DIR="$(rlocation "_main/example/service_image")"`, `readonly FIXED_ARGS=("--repository" "registry.example.com/team/service")`, `readonly REPOSITORY_FILE="$(rlocation "{{repository_file}}")"`}, "\n"),
			manifest:    []string{"_main/example/service_image {WORKSPACE}/fixtures/service_image"},
			files:       map[string]string{"fixtures/service_image/index.json": `{"manifests":[{"digest":""}]}`},
			errContains: "digest",
		},
		{
			name:        "malformed digest",
			script:      strings.Join([]string{`readonly IMAGE_DIR="$(rlocation "_main/example/service_image")"`, `readonly FIXED_ARGS=("--repository" "registry.example.com/team/service")`, `readonly REPOSITORY_FILE="$(rlocation "{{repository_file}}")"`}, "\n"),
			manifest:    []string{"_main/example/service_image {WORKSPACE}/fixtures/service_image"},
			files:       map[string]string{"fixtures/service_image/index.json": `{"manifests":[{"digest":"not-a-digest"}]}`},
			errContains: "digest",
		},
		{
			name:        "runner failure",
			runErr:      errors.New("bazel run failed"),
			errContains: "bazel run failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			scriptRelPath := filepath.ToSlash("bazel-out/k8-fastbuild/bin/example/push_service_image_push.sh")
			scriptPath := filepath.Join(workspaceRoot, filepath.FromSlash(scriptRelPath))
			if tt.script != "" {
				writeTestFile(t, scriptPath, tt.script)

				manifestLines := make([]string, 0, len(tt.manifest))
				for _, line := range tt.manifest {
					manifestLines = append(manifestLines, strings.ReplaceAll(line, "{WORKSPACE}", filepath.ToSlash(workspaceRoot)))
				}
				writeTestFile(t, scriptPath+".runfiles_manifest", strings.Join(manifestLines, "\n")+"\n")

				for relPath, content := range tt.files {
					writeTestFile(t, filepath.Join(workspaceRoot, relPath), content)
				}
			}

			runner, _ := newTestBazelRunner(workspaceRoot, scriptRelPath, tt.runErr)
			resolver := NewResolver(runner)

			_, err := resolver.Resolve(context.Background(), "//experimental/grpc_hello_world/service:service_image")
			if err == nil {
				t.Fatal("Resolve() succeeded unexpectedly")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("Resolve() err = %v, want substring %q", err, tt.errContains)
			}
		})
	}
}

func newTestBazelRunner(workspaceRoot string, scriptRelPath string, runErr error) (*bazelRunner, *stubCommandExecutor) {
	exec := &stubCommandExecutor{
		runFunc: func(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
			if dir != workspaceRoot {
				return nil, fmt.Errorf("unexpected working directory: %s", dir)
			}
			if name != bazelBinary {
				return nil, fmt.Errorf("unexpected command: %s", name)
			}
			if len(args) < 4 {
				return nil, fmt.Errorf("unexpected args: %v", args)
			}
			if args[1] != bazelNoProgress || args[2] != bazelOutputFilter {
				return nil, fmt.Errorf("unexpected bazel flag order: %v", args)
			}
			switch args[0] {
			case "run":
				if runErr != nil {
					return nil, runErr
				}
				return nil, nil
			case "cquery":
				return []byte(scriptRelPath + "\n"), nil
			default:
				return nil, fmt.Errorf("unexpected bazel subcommand: %s", args[0])
			}
		},
	}

	return &bazelRunner{
		workspaceRoot: workspaceRoot,
		exec:          exec,
	}, exec
}

func gotSubcommands(calls []commandCall) string {
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		if len(call.args) >= 1 {
			parts = append(parts, call.args[0])
		}
	}
	return strings.Join(parts, ",")
}

func writeTestFile(t *testing.T, filePath string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", filePath, err)
	}
}
