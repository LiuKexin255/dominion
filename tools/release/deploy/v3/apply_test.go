package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dominion/tools/release/deploy/pkg/imagepush"
	clientpkg "dominion/tools/release/deploy/v2/client"
)

const v3ServiceAPath = "tools/release/deploy/v2/compiler/testdata/service-a.yaml"

var v3ApplyServiceFixtures = map[string]string{
	v3ServiceAPath: strings.Join([]string{
		"version: \"3.0\"",
		"name: service-a",
		"app: alpha",
		"desc: service a",
		"artifacts:",
		"  - name: service-a",
		"    target: :service_a_image",
		"    tls: true",
		"    ports:",
		"      - name: grpc",
		"        port: 50051",
	}, "\n") + "\n",
}

func TestV3ApplyCommand_Success(t *testing.T) {
	_, deployPath := newV3ApplyWorkspace(t, strings.Join([]string{
		"version: \"3.0\"",
		"name: team.dev",
		"desc: alpha env",
		"type: prod",
		"services:",
		"  - artifact:",
		"      path: //tools/release/deploy/v2/compiler/testdata/service-a.yaml",
		"      name: service-a",
	}, "\n")+"\n")

	server, requestCount := newV3ApplyTestServer(t, []v3ApplyResponseStep{
		{method: http.MethodGet, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`},
		{method: http.MethodPost, path: "/v1/deploy/scopes/team/environments", status: http.StatusOK, body: `{"name":"deploy/scopes/team/environments/dev","status":{"state":"ENVIRONMENT_STATE_RECONCILING"}}`},
		{method: http.MethodGet, path: "/v1/deploy/scopes/team/environments/dev", status: http.StatusOK, body: `{"name":"deploy/scopes/team/environments/dev","status":{"state":"ENVIRONMENT_STATE_READY"}}`},
	})
	defer server.Close()

	oldRunner := newV3ImageRunner
	newV3ImageRunner = func() (imagepush.V3Runner, error) {
		return &fakeV3Runner{
			metadata: &imagepush.Metadata{
				App:         "alpha",
				Service:     "service-a",
				ImageTarget: "//tools/release/deploy/v2/compiler/testdata:service_a_image_oci",
				PushTarget:  "//tools/release/deploy/v2/compiler/testdata:service_a_image_push",
				Repository:  "registry.liukexin.com/alpha/service-a",
				Tag:         "latest",
			},
			digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}, nil
	}
	t.Cleanup(func() { newV3ImageRunner = oldRunner })

	var out bytes.Buffer
	oldStdout := stdout
	stdout = &out
	t.Cleanup(func() { stdout = oldStdout })

	err := applyCommand(context.Background(), &options{
		target:    deployPath,
		endpoint:  server.URL,
		timeout:   50 * time.Millisecond,
		scope:     "team",
		apiClient: clientpkg.NewClient(server.URL),
	})
	if err != nil {
		t.Fatalf("applyCommand() unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "环境 team.dev 已应用，状态: 就绪") {
		t.Fatalf("applyCommand() output = %q, want ready output", out.String())
	}
	if requestCount.Load() != 3 {
		t.Fatalf("server request count = %d, want 3", requestCount.Load())
	}
}

func TestV3ApplyCommand_RejectsV2Config(t *testing.T) {
	_, deployPath := newV3ApplyWorkspace(t, strings.Join([]string{
		"name: team.dev",
		"desc: alpha env",
		"type: prod",
		"services:",
		"  - artifact:",
		"      path: //tools/release/deploy/v2/compiler/testdata/service-a.yaml",
		"      name: service-a",
	}, "\n")+"\n")

	err := applyCommand(context.Background(), &options{target: deployPath, timeout: 50 * time.Millisecond, scope: "team", apiClient: clientpkg.NewClient("http://example.invalid")})
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("applyCommand() error = %v, want version error", err)
	}
}

func TestV3ApplyCommand_RejectsMixedVersion(t *testing.T) {
	root, deployPath := newV3ApplyWorkspace(t, strings.Join([]string{
		"version: \"3.0\"",
		"name: team.dev",
		"desc: alpha env",
		"type: prod",
		"services:",
		"  - artifact:",
		"      path: //tools/release/deploy/v2/compiler/testdata/service-a.yaml",
		"      name: service-a",
	}, "\n")+"\n")
	writeFile(t, filepath.Join(root, v3ServiceAPath), strings.Join([]string{
		"name: service-a",
		"app: alpha",
		"desc: service a",
		"artifacts:",
		"  - name: service-a",
		"    target: :service_a_image",
		"    ports:",
		"      - name: grpc",
		"        port: 50051",
	}, "\n")+"\n")

	err := applyCommand(context.Background(), &options{target: deployPath, timeout: 50 * time.Millisecond, scope: "team", apiClient: clientpkg.NewClient("http://example.invalid")})
	if err == nil || !strings.Contains(err.Error(), "service config version") {
		t.Fatalf("applyCommand() error = %v, want service version error", err)
	}
}

func TestV3ApplyCommand_MetadataValidationFails(t *testing.T) {
	_, deployPath := newV3ApplyWorkspace(t, strings.Join([]string{
		"version: \"3.0\"",
		"name: team.dev",
		"desc: alpha env",
		"type: prod",
		"services:",
		"  - artifact:",
		"      path: //tools/release/deploy/v2/compiler/testdata/service-a.yaml",
		"      name: service-a",
	}, "\n")+"\n")

	oldRunner := newV3ImageRunner
	newV3ImageRunner = func() (imagepush.V3Runner, error) {
		return &fakeV3Runner{
			metadata: &imagepush.Metadata{
				App:        "wrong",
				Service:    "service-a",
				PushTarget: "//tools/release/deploy/v2/compiler/testdata:service_a_image_push",
				Repository: "registry.liukexin.com/alpha/service-a",
				Tag:        "latest",
			},
			digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}, nil
	}
	t.Cleanup(func() { newV3ImageRunner = oldRunner })

	err := applyCommand(context.Background(), &options{target: deployPath, timeout: 50 * time.Millisecond, scope: "team", apiClient: clientpkg.NewClient("http://example.invalid")})
	if err == nil || !strings.Contains(err.Error(), "metadata app") {
		t.Fatalf("applyCommand() error = %v, want metadata app error", err)
	}
}

type fakeV3Runner struct {
	metadata  *imagepush.Metadata
	metaErr   error
	pushErr   error
	digest    string
	digestErr error
}

func (r *fakeV3Runner) BuildAndReadMetadata(ctx context.Context, target string) (*imagepush.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.metaErr != nil {
		return nil, r.metaErr
	}
	return r.metadata, nil
}

func (r *fakeV3Runner) RunPush(ctx context.Context, pushTarget string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.pushErr
}

func (r *fakeV3Runner) ReadDigest(ctx context.Context, imageTarget string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.digestErr != nil {
		return "", r.digestErr
	}
	return r.digest, nil
}

type v3ApplyResponseStep struct {
	method string
	path   string
	status int
	body   string
}

func newV3ApplyTestServer(t *testing.T, steps []v3ApplyResponseStep) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	requestCount := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(requestCount.Add(1)) - 1
		if len(steps) == 0 {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if idx >= len(steps) {
			idx = len(steps) - 1
		}

		step := steps[idx]
		if r.Method != step.method {
			t.Fatalf("request %d method = %s, want %s", idx, r.Method, step.method)
		}
		if r.URL.Path != step.path {
			t.Fatalf("request %d path = %s, want %s", idx, r.URL.Path, step.path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		if step.body != "" {
			if _, err := w.Write([]byte(step.body)); err != nil {
				t.Fatalf("Write() failed: %v", err)
			}
		}
	}))

	return server, requestCount
}

func newV3ApplyWorkspace(t *testing.T, deployYAML string) (string, string) {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "MODULE.bazel"), "")

	for path, content := range v3ApplyServiceFixtures {
		writeFile(t, filepath.Join(root, path), content)
	}
	deployPath := filepath.Join(root, "deploy.yaml")
	writeFile(t, deployPath, deployYAML)
	withV3ApplyWorkingDir(t, root)

	return root, deployPath
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", path, err)
	}
}

func withV3ApplyWorkingDir(t *testing.T, dir string) {
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
